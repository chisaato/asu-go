package metadata

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gzzchh/asu-go/internal/sources"
)

// ProfileProvider 是构建校验可依赖的最小元数据接口，不依赖 HTTP/Gin。
// target 使用 target/subtarget；Packages 返回去 ABI 后的包名到版本映射。
type ProfileProvider interface {
	Profile(context.Context, string, string, string, string) (map[string]any, error)
	Targets(context.Context, string, string) (map[string]any, error)
	Packages(context.Context, string, string, string) (map[string]string, error)
}

var _ ProfileProvider = (*Service)(nil)

type ProfileNotFoundError struct{ Profile string }

func (e *ProfileNotFoundError) Error() string { return "Profile not found: " + e.Profile }

func (s *Service) source(ctx context.Context, distro, version string) (*sources.Source, error) {
	if !safePath(distro) || strings.Contains(distro, "/") || !safePath(version) || strings.Contains(version, "/") {
		return nil, &RevisionError{400, "Invalid metadata path"}
	}
	all, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	var fallback *sources.Source
	for i := range all {
		item := &all[i]
		if item.Distribution != distro || !item.Enabled {
			continue
		}
		if item.Version == version {
			return item, nil
		}
		if item.Branch == Branch(version) {
			fallback = item
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, &RevisionError{400, "Unsupported version: " + version}
}

func (s *Service) Targets(ctx context.Context, distro, version string) (map[string]any, error) {
	source, err := s.source(ctx, distro, version)
	if err != nil {
		return nil, err
	}
	if source.Version != version {
		return nil, &RevisionError{400, "Unsupported version: " + version}
	}
	return source.Targets, nil
}

func (s *Service) resource(ctx context.Context, source *sources.Source, path string) ([]byte, error) {
	if s.Cache == nil {
		return nil, fmt.Errorf("metadata Cache 未配置")
	}
	return s.Cache.get(ctx, source.UpstreamURL, path)
}

func (s *Service) profiles(ctx context.Context, source *sources.Source, path string) (map[string]any, error) {
	body, err := s.resource(ctx, source, path+"/profiles.json")
	if err != nil {
		return nil, fmt.Errorf("读取 profiles.json: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("profiles.json 不是对象")
	}
	return result, nil
}

func (s *Service) Profile(ctx context.Context, distro, version, target, profile string) (map[string]any, error) {
	if !safePath(target) || len(strings.Split(target, "/")) != 2 || !safePath(profile) || strings.Contains(profile, "/") {
		return nil, &RevisionError{400, "Invalid metadata path"}
	}
	source, err := s.source(ctx, distro, version)
	if err != nil {
		return nil, err
	}
	data, err := s.profiles(ctx, source, VersionPath(version)+"/targets/"+target)
	if err != nil {
		return nil, fmt.Errorf("读取 profile: %v", err)
	}
	profiles, _ := data["profiles"].(map[string]any)
	detail, ok := profiles[profile].(map[string]any)
	if !ok {
		return nil, &ProfileNotFoundError{Profile: profile}
	}
	delete(data, "profiles")
	epoch := int64(0)
	if value, ok := data["source_date_epoch"]; ok {
		if number, ok := value.(float64); ok {
			epoch = int64(number)
		} else {
			epoch, err = strconv.ParseInt(fmt.Sprint(value), 10, 64)
			if err != nil {
				return nil, err
			}
		}
	}
	for k, v := range detail {
		data[k] = v
	}
	data["id"] = profile
	data["build_at"] = time.Unix(epoch, 0).UTC().Format("2006-01-02T15:04:05.000000Z")
	return data, nil
}

// PostKmodSplit 与 asu/util.py 的字符串比较规则逐项对齐（包括 23.x 的历史规则）。
func PostKmodSplit(path string) bool {
	if strings.HasPrefix(path, "snapshots") {
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return false
	}
	v := strings.Split(parts[1], ".")
	if len(v) < 2 {
		return false
	}
	major, _ := strconv.Atoi(v[0])
	minor := v[len(v)-1]
	return major >= 24 || major == 23 && (minor == "05-SNAPSHOT" || minor >= "6")
}

func parsePackages(body []byte) (map[string]any, error) {
	packages := map[string]string{}
	architecture := ""
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, chunk := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		headers, err := mail.ReadMessage(bufio.NewReader(strings.NewReader(chunk + "\n\n")))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if headers == nil {
			return nil, fmt.Errorf("Packages 记录无效")
		}
		name, version := headers.Header.Get("Package"), headers.Header.Get("Version")
		if name == "" || version == "" {
			return nil, fmt.Errorf("Packages 缺少 Package/Version")
		}
		if architecture == "" && headers.Header.Get("Architecture") != "all" {
			architecture = headers.Header.Get("Architecture")
		}
		if abi := headers.Header.Get("ABIVersion"); abi != "" {
			name = strings.TrimSuffix(name, abi)
		}
		packages[name] = version
	}
	return map[string]any{"architecture": architecture, "packages": packages}, nil
}

func (s *Service) packageIndex(ctx context.Context, source *sources.Source, path string) (map[string]any, error) {
	result := map[string]any{}
	body, err := s.resource(ctx, source, path+"/index.json")
	var status *RevisionError
	if err != nil && !errors.As(err, &status) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("index.json 不是对象")
		}
		if version, ok := result["version"].(float64); ok && version >= 2 {
			delete(result, "version")
			return result, nil
		}
	}
	body, err = s.resource(ctx, source, path+"/Packages")
	if err != nil {
		if errors.As(err, &status) {
			return result, nil
		}
		return nil, err
	}
	return parsePackages(body)
}

func packageMap(index map[string]any) map[string]string {
	result := map[string]string{}
	switch p := index["packages"].(type) {
	case map[string]string:
		for k, v := range p {
			result[k] = v
		}
	case map[string]any:
		for k, v := range p {
			if text, ok := v.(string); ok {
				result[k] = text
			}
		}
	}
	return result
}

func (s *Service) targetIndex(ctx context.Context, source *sources.Source, path string) (map[string]any, error) {
	index, err := s.packageIndex(ctx, source, path+"/packages")
	if err != nil {
		return nil, err
	}
	if PostKmodSplit(path) {
		profiles, err := s.profiles(ctx, source, path)
		if err != nil {
			// 原版 parse_kernel_version 在非 200 时跳过合并。
			var status *RevisionError
			if errors.As(err, &status) {
				return index, nil
			}
			return nil, err
		}
		kernel, _ := profiles["linux_kernel"].(map[string]any)
		if len(kernel) > 0 {
			if _, exists := index["packages"]; !exists {
				return nil, fmt.Errorf("基础索引缺少 packages，无法合并 kmod")
			}
			parts := make([]string, 0, 3)
			for _, field := range []string{"version", "release", "vermagic"} {
				value, ok := kernel[field]
				if !ok {
					return nil, fmt.Errorf("linux_kernel 缺少 %s", field)
				}
				parts = append(parts, fmt.Sprint(value))
			}
			kmods, err := s.packageIndex(ctx, source, path+"/kmods/"+strings.Join(parts, "-"))
			if err != nil {
				return nil, err
			}
			merged := packageMap(index)
			for k, v := range packageMap(kmods) {
				merged[k] = v
			}
			index["packages"] = merged
		}
	}
	return index, nil
}

// Packages 同时合并 target 包与 feeds.conf 中架构 feed 的包，供构建请求校验。
func (s *Service) Packages(ctx context.Context, distro, version, target string) (map[string]string, error) {
	if !safePath(target) || len(strings.Split(target, "/")) != 2 {
		return nil, &RevisionError{400, "Invalid metadata path"}
	}
	source, err := s.source(ctx, distro, version)
	if err != nil {
		return nil, err
	}
	index, err := s.targetIndex(ctx, source, VersionPath(version)+"/targets/"+target)
	if err != nil {
		return nil, err
	}
	result := packageMap(index)
	arch, _ := index["architecture"].(string)
	if arch != "" {
		feeds, err := s.archIndex(ctx, source, VersionPath(version)+"/packages/"+arch)
		if err != nil {
			return nil, err
		}
		for k, v := range feeds {
			result[k] = v
		}
	}
	return result, nil
}

func (s *Service) archIndex(ctx context.Context, source *sources.Source, path string) (map[string]string, error) {
	result := map[string]string{}
	body, err := s.resource(ctx, source, path+"/feeds.conf")
	if err != nil {
		var status *RevisionError
		if errors.As(err, &status) {
			return result, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		index, err := s.packageIndex(ctx, source, path+"/"+fields[1])
		if err != nil {
			return nil, err
		}
		for k, v := range packageMap(index) {
			result[k] = v
		}
	}
	return result, nil
}

// JSONPath 解析 /json/v1/ 后的任意多段规范路径，路由层无需模拟 FastAPI 的贪婪参数。
func (s *Service) JSONPath(ctx context.Context, path string) (any, error) {
	if !safePath(path) {
		return nil, &RevisionError{400, "Invalid metadata path"}
	}
	parts := strings.Split(path, "/")
	version := ""
	switch {
	case len(parts) >= 2 && parts[0] == "snapshots":
		version = "SNAPSHOT"
	case len(parts) >= 3 && parts[0] == "releases":
		version = parts[1]
	default:
		return nil, &RevisionError{404, "Not Found"}
	}
	source, err := s.source(ctx, "openwrt", version)
	if err != nil {
		return nil, err
	}
	last := parts[len(parts)-1]
	base := strings.Join(parts[:len(parts)-1], "/")
	if last == "index.json" {
		return s.targetIndex(ctx, source, base)
	}
	if strings.HasSuffix(last, "-index.json") {
		return s.archIndex(ctx, source, base+"/"+strings.TrimSuffix(last, "-index.json"))
	}
	prefix := VersionPath(version) + "/targets/"
	if strings.HasPrefix(path, prefix) && strings.HasSuffix(last, ".json") {
		target := strings.TrimPrefix(base, prefix)
		return s.Profile(ctx, "openwrt", version, target, strings.TrimSuffix(last, ".json"))
	}
	return nil, &RevisionError{404, "Not Found"}
}
