package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gzzchh/asu-go/internal/sources"
)

type PackageChange struct {
	Source    string `json:"source,omitempty"`
	Target    string `json:"target,omitempty"`
	Revision  int    `json:"revision"`
	Mandatory bool   `json:"mandatory,omitempty"`
}

// 与原版 asu/config.py 的 package_changes_list 和 release() 保持一致。
var packageChanges = []PackageChange{
	{Source: "firewall", Target: "firewall4", Revision: 18611},
	{Source: "kmod-nft-nat6", Revision: 20282, Mandatory: true},
	{Source: "libustream-wolfssl", Target: "libustream-mbedtls", Revision: 21994},
	{Source: "px5g-wolfssl", Target: "px5g-mbedtls", Revision: 21994},
	{Source: "wpad-basic-wolfssl", Target: "wpad-basic-mbedtls", Revision: 21994},
	{Source: "luci-app-diag-core", Revision: 25984, Mandatory: true},
	{Source: "auc", Target: "owut", Revision: 26792},
	{Source: "luci-app-opkg", Target: "luci-app-package-manager", Revision: 27897},
	{Source: "opkg", Target: "apk-mbedtls", Revision: 28056},
}

type BranchInfo struct {
	Path           string          `json:"path"`
	Enabled        bool            `json:"enabled"`
	Snapshot       bool            `json:"snapshot"`
	PathPackages   string          `json:"path_packages"`
	BranchOffRev   int             `json:"branch_off_rev,omitempty"`
	PackageChanges []PackageChange `json:"package_changes"`
	Name           string          `json:"name"`
	Versions       []string        `json:"versions"`
	Targets        map[string]any  `json:"targets"`
}

type ServerInfo struct {
	Version               string   `json:"version"`
	Contact               string   `json:"contact"`
	AllowDefaults         bool     `json:"allow_defaults"`
	RepositoryAllowList   []string `json:"repository_allow_list"`
	MaxCustomRootfsSizeMB int      `json:"max_custom_rootfs_size_mb"`
	MaxDefaultsLength     int      `json:"max_defaults_length"`
}

type Overview struct {
	Latest      []string               `json:"latest"`
	Branches    map[string]*BranchInfo `json:"branches"`
	UpstreamURL string                 `json:"upstream_url"`
	Server      ServerInfo             `json:"server"`
}

type Service struct {
	Store   *sources.Store
	Client  *Client
	Version string
	Cache   *Cache
}

func branchInfo(name string) *BranchInfo {
	revision := map[string]int{"25.12": 32295, "24.10": 27990, "23.05": 23069, "22.03": 19160, "21.02": 15812}[name]
	changes := []PackageChange{}
	for _, c := range packageChanges {
		if name == "SNAPSHOT" || c.Revision <= revision {
			changes = append(changes, c)
		}
	}
	path := "releases/{version}"
	if name == "SNAPSHOT" {
		path = "snapshots"
	}
	return &BranchInfo{Path: path, Snapshot: name == "SNAPSHOT", PathPackages: "DEPRECATED", BranchOffRev: revision, PackageChanges: changes, Name: name, Versions: []string{}, Targets: map[string]any{}}
}

// 公共兼容协议没有 distribution 参数，按原版固定提供 OpenWrt 视图。
// 发行版全量目录仍通过 /admin/api/sources 访问。
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	catalog, err := s.Store.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	sort.SliceStable(catalog, func(i, j int) bool {
		return strings.ReplaceAll(catalog[i].Version, ".0-rc", "-rc") > strings.ReplaceAll(catalog[j].Version, ".0-rc", "-rc")
	})
	result := Overview{Latest: []string{}, Branches: map[string]*BranchInfo{}, UpstreamURL: "https://downloads.openwrt.org", Server: ServerInfo{Version: s.Version, Contact: "mail@aparcar.org", RepositoryAllowList: []string{}, MaxCustomRootfsSizeMB: 1024, MaxDefaultsLength: 20480}}
	channels := map[string]string{}
	upstreamSet := false
	for _, source := range catalog {
		if source.Distribution != "openwrt" {
			continue
		}
		if !upstreamSet || source.Version == "SNAPSHOT" {
			result.UpstreamURL = source.UpstreamURL
			upstreamSet = true
		}
		b := result.Branches[source.Branch]
		if b == nil {
			b = branchInfo(source.Branch)
			result.Branches[source.Branch] = b
		}
		b.Enabled = b.Enabled || source.Enabled
		if !source.Enabled {
			continue
		}
		if len(b.Versions) == 0 {
			b.Targets = source.Targets
		}
		b.Versions = append(b.Versions, source.Version)
		if channels[source.Channel] == "" {
			channels[source.Channel] = source.Version
		}
	}
	for _, channel := range []string{"upcoming", "stable", "oldstable"} {
		if v := channels[channel]; v != "" {
			result.Latest = append(result.Latest, v)
		}
	}
	return result, nil
}

func (s *Service) Branches(ctx context.Context) ([]*BranchInfo, error) {
	overview, err := s.Overview(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(overview.Branches))
	for name := range overview.Branches {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	result := make([]*BranchInfo, 0, len(names))
	for _, name := range names {
		result = append(result, overview.Branches[name])
	}
	return result, nil
}

type RevisionError struct {
	Status int
	Detail string
}

func (e *RevisionError) Error() string { return e.Detail }

func (s *Service) Revision(ctx context.Context, version, target, subtarget string) (string, error) {
	catalog, err := s.Store.List(ctx)
	if err != nil {
		return "", err
	}
	var selected *sources.Source
	for i := range catalog {
		source := &catalog[i]
		if source.Distribution != "openwrt" || source.Branch != Branch(version) {
			continue
		}
		selected = source
		if source.Version == version {
			break
		}
	}
	if selected == nil {
		return "", &RevisionError{400, "Unsupported version: " + version}
	}
	path := VersionPath(version) + "/targets/" + target + "/" + subtarget + "/profiles.json"
	var res Response
	if s.Cache != nil {
		res.Body, err = s.Cache.get(ctx, selected.UpstreamURL, path)
		res.Status = 200
		var status *RevisionError
		if errors.As(err, &status) {
			res.Status, err = status.Status, nil
		}
	} else {
		// 保留仅使用同步客户端的嵌入调用方；serve 总是配置磁盘缓存。
		res, err = s.Client.Get(ctx, strings.TrimRight(selected.UpstreamURL, "/")+"/"+path)
	}
	if err != nil {
		return "", err
	}
	if res.Status != 200 {
		return "", &RevisionError{res.Status, fmt.Sprintf("Failed to fetch revision for %s/%s/%s", version, target, subtarget)}
	}
	var profiles struct {
		VersionCode *string `json:"version_code"`
	}
	if err := json.Unmarshal(res.Body, &profiles); err != nil {
		return "", err
	}
	if profiles.VersionCode == nil {
		return "", fmt.Errorf("profiles.json 缺少 version_code")
	}
	return *profiles.VersionCode, nil
}
