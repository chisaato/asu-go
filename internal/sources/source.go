// Package sources 管理数据库中的发行流目录。
package sources

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Source struct {
	ID             int64          `json:"id" yaml:"id"`
	Distribution   string         `json:"distribution" yaml:"distribution"`
	Branch         string         `json:"branch" yaml:"branch"`
	Version        string         `json:"version" yaml:"version"`
	Channel        string         `json:"channel" yaml:"channel"`
	UpstreamURL    string         `json:"upstream_url" yaml:"upstream_url"`
	Image          string         `json:"image" yaml:"image"`
	TagTemplate    string         `json:"tag_template" yaml:"tag_template"`
	PackageManager string         `json:"package_manager" yaml:"package_manager"`
	Origin         string         `json:"origin" yaml:"origin"`
	Rolling        bool           `json:"rolling" yaml:"rolling"`
	Enabled        bool           `json:"enabled" yaml:"enabled"`
	Overrides      []string       `json:"overrides" yaml:"overrides"`
	Targets        map[string]any `json:"targets" yaml:"targets"`
	SyncedAt       *time.Time     `json:"synced_at" yaml:"synced_at"`
}

func NewSource() Source {
	return Source{Origin: "local", Enabled: true, PackageManager: "opkg", TagTemplate: "{target}-{version_tag}", Overrides: []string{}, Targets: map[string]any{}}
}

var releaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(-rc\d+)?$`)
var tagPattern = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)

// ResolveImage 对齐原版 asu/util.py:get_container_version_tag。
// ImmortalWrt 标签于 2026-10-27 查证 Docker Hub API：
// https://hub.docker.com/v2/repositories/immortalwrt/imagebuilder/tags?name=mediatek-filogic&page_size=10
// 正式版 openwrt-25.12.2、分支 openwrt-24.10、主线 master 均存在。
// tag_template 可直接指定固定标签，或使用 {target}/{version}/{version_tag} 覆盖规则。
func ResolveImage(source Source, target string) (string, error) {
	versionTag := "openwrt-" + strings.TrimSuffix(source.Version, "-SNAPSHOT")
	if source.Version == "SNAPSHOT" {
		versionTag = "master"
	} else if releaseVersion.MatchString(source.Version) && source.Distribution != "immortalwrt" {
		versionTag = "v" + source.Version
	}
	tag := strings.NewReplacer("{target}", strings.ReplaceAll(target, "/", "-"), "{version}", source.Version, "{version_tag}", versionTag).Replace(source.TagTemplate)
	if !tagPattern.MatchString(tag) || source.Image == "" {
		return "", fmt.Errorf("无效镜像模板或标签 %q", tag)
	}
	return source.Image + ":" + tag, nil
}

func (s Source) Validate() error {
	for name, value := range map[string]string{"distribution": s.Distribution, "branch": s.Branch, "version": s.Version, "image": s.Image, "tag_template": s.TagTemplate} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s 不得为空", name)
		}
	}
	u, err := url.Parse(s.UpstreamURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("upstream_url 必须是 HTTP(S) URL")
	}
	if s.Origin != "local" && s.Origin != "upstream" {
		return errors.New("origin 仅支持 local/upstream")
	}
	if s.PackageManager != "opkg" && s.PackageManager != "apk" {
		return errors.New("package_manager 仅支持 opkg/apk")
	}
	if _, err := ResolveImage(s, "mediatek/filogic"); err != nil {
		return err
	}
	for _, field := range s.Overrides {
		if !writable[field] {
			return fmt.Errorf("未知 overrides 字段 %q", field)
		}
	}
	return nil
}

func DecodeCatalog(r io.Reader) ([]Source, error) {
	d := yaml.NewDecoder(r)
	d.KnownFields(true)
	var catalog []Source
	if err := d.Decode(&catalog); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("目录只允许一个 YAML 文档")
	}
	return catalog, nil
}

func Export(w io.Writer, catalog []Source) error {
	e := yaml.NewEncoder(w)
	defer func() { _ = e.Close() }()
	return e.Encode(catalog)
}
