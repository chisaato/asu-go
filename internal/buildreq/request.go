// Package buildreq 提供不依赖 HTTP、数据库或元数据存储的构建请求处理。
package buildreq

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Request 与原版 BuildRequest 字段一致。可空标量使用指针，区分 null 与空值。
// 程序内构造请求应使用 NewRequest；JSON 解码自动应用相同默认值。
type Request struct {
	Distro           string            `json:"distro"`
	Version          string            `json:"version"`
	VersionCode      string            `json:"version_code"`
	Target           string            `json:"target"`
	Profile          string            `json:"profile"`
	Packages         []string          `json:"packages"`
	PackagesVersions map[string]string `json:"packages_versions"`
	DiffPackages     bool              `json:"diff_packages"`
	Defaults         *string           `json:"defaults"`
	RootfsSizeMB     *int              `json:"rootfs_size_mb"`
	Filesystem       *string           `json:"filesystem"`
	Repositories     map[string]string `json:"repositories"`
	RepositoriesMode string            `json:"repositories_mode"`
	RepositoryKeys   []string          `json:"repository_keys"`
	Client           *string           `json:"client"`
	distroSet        bool
	modeSet          bool
}

func NewRequest() Request {
	return Request{
		Distro: "openwrt", RepositoriesMode: "replace",
		Packages: []string{}, PackagesVersions: map[string]string{},
		Repositories: map[string]string{}, RepositoryKeys: []string{},
	}
}

// DistroSpecified 区分 JSON 显式值与缺省值，供路径作用域应用默认发行版。
func (r Request) DistroSpecified() bool { return r.distroSet }

// UnmarshalJSON 保留原版的必填字段、非空类型约束与省略字段默认值。
// 类型和模型错误属于 422；服务端策略校验属于 400。
func (r *Request) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return invalid("request", err.Error())
	}
	for _, key := range []string{"version", "target", "profile"} {
		if _, ok := fields[key]; !ok {
			return invalid(key, "Field required")
		}
	}
	for _, key := range []string{"distro", "version", "version_code", "target", "profile", "packages", "packages_versions", "diff_packages", "repositories", "repositories_mode", "repository_keys"} {
		if raw, ok := fields[key]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return invalid(key, "Input cannot be null")
		}
	}
	// Go 的 JSON 解码器允许 null 转为元素零值，这与原版非空元素不符。
	for _, key := range []string{"packages", "repository_keys", "packages_versions", "repositories"} {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var values []json.RawMessage
		if key == "packages" || key == "repository_keys" {
			if err := json.Unmarshal(raw, &values); err != nil {
				return invalid(key, err.Error())
			}
		} else {
			var entries map[string]json.RawMessage
			if err := json.Unmarshal(raw, &entries); err != nil {
				return invalid(key, err.Error())
			}
			for _, value := range entries {
				values = append(values, value)
			}
		}
		for _, value := range values {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return invalid(key, "Input cannot contain null")
			}
		}
	}
	type plain Request
	decoded := plain(NewRequest())
	if err := json.Unmarshal(data, &decoded); err != nil {
		return invalid("request", err.Error())
	}
	*r = Request(decoded)
	_, r.distroSet = fields["distro"]
	_, r.modeSet = fields["repositories_mode"]
	return nil
}

// Normalize 返回独立副本，不改动调用者的切片或映射。包名仅去除一个 + 前缀。
// nil 集合保留为 null，不与显式空集合合并；省略字段默认值由解码器填充。
func (r Request) Normalize() Request {
	if r.Distro == "" && !r.distroSet {
		r.Distro = "openwrt"
	}
	if r.RepositoriesMode == "" && !r.modeSet {
		r.RepositoriesMode = "replace"
	}
	r.Profile = strings.ReplaceAll(r.Profile, ",", "_")
	r.Packages = slices.Clone(r.Packages)
	for i, pkg := range r.Packages {
		r.Packages[i] = strings.TrimPrefix(pkg, "+")
	}
	slices.Sort(r.Packages)
	r.Packages = slices.Compact(r.Packages)
	r.PackagesVersions = maps.Clone(r.PackagesVersions)
	r.Repositories = maps.Clone(r.Repositories)
	r.RepositoryKeys = slices.Clone(r.RepositoryKeys)
	return r
}

// Canonical 返回用于 jobs.request 的完整规范 JSON，含 client。
// 键递归排序、保留 null、禁用 HTML 转义；UTF-8 字符不转为 ASCII 转义。
func (r Request) Canonical() ([]byte, error) {
	r = r.Normalize()
	return canonical(r.fields(true))
}

// Hash 仅包含 API 规范规定的构建字段，client 不影响产物与缓存身份。
func (r Request) Hash() (string, error) {
	r = r.Normalize()
	data, err := canonical(r.fields(false))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (r Request) fields(includeClient bool) map[string]any {
	fields := map[string]any{
		"distro": r.Distro, "version": r.Version, "version_code": r.VersionCode,
		"target": r.Target, "profile": r.Profile, "packages": r.Packages,
		"packages_versions": r.PackagesVersions, "diff_packages": r.DiffPackages,
		"defaults": r.Defaults, "rootfs_size_mb": r.RootfsSizeMB,
		"filesystem": r.Filesystem, "repositories": r.Repositories,
		"repositories_mode": r.RepositoriesMode, "repository_keys": r.RepositoryKeys,
	}
	if includeClient {
		fields["client"] = r.Client
	}
	return fields
}

func canonical(fields map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fields); err != nil {
		return nil, fmt.Errorf("canonical build request: %w", err)
	}
	// encoding/json 固定转义这两个合法 Unicode 字符；规范形式使用原始 UTF-8。
	data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	// 不能直接替换转义文本，否则会改写用户输入的字面量反斜杠序列。
	var out bytes.Buffer
	for i := 0; i < len(data); i++ {
		if data[i] == '\\' && i+1 < len(data) {
			if i+6 <= len(data) && (string(data[i:i+6]) == `\u2028` || string(data[i:i+6]) == `\u2029`) {
				if data[i+5] == '8' {
					out.WriteRune('\u2028')
				} else {
					out.WriteRune('\u2029')
				}
				i += 5
				continue
			}
			out.WriteByte(data[i])
			i++
		}
		out.WriteByte(data[i])
	}
	return out.Bytes(), nil
}
