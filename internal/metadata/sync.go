package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gzzchh/asu-go/internal/sources"
	"go.uber.org/zap"
)

type Versions struct {
	Upcoming  string   `json:"upcoming_version"`
	Stable    string   `json:"stable_version"`
	Oldstable string   `json:"oldstable_version"`
	List      []string `json:"versions_list"`
}

func Branch(version string) string {
	if version == "SNAPSHOT" {
		return version
	}
	if strings.HasSuffix(version, "-SNAPSHOT") {
		return strings.TrimSuffix(version, "-SNAPSHOT")
	}
	if i := strings.LastIndex(version, "."); i >= 0 {
		return version[:i]
	}
	return version
}

func VersionPath(version string) string {
	if version == "SNAPSHOT" {
		return "snapshots"
	}
	return "releases/" + version
}

// SortVersions 对齐原版 replace('.0-rc', '-rc') 后的字符串降序。
func SortVersions(v []string) {
	sort.SliceStable(v, func(i, j int) bool {
		return strings.ReplaceAll(v[i], ".0-rc", "-rc") > strings.ReplaceAll(v[j], ".0-rc", "-rc")
	})
}

// FilterAllowedVersions 精简版本列表：
// 1. 仅保留 24.x 和 25.x 两个大版本；
// 2. 彻底剔除 SNAPSHOT 与 -rc 测试版本；
// 3. 对每个分支（如 24.10、25.12）按版本降序排序，仅保留最新的 3 个正式版本。
func FilterAllowedVersions(list []string) []string {
	grouped := make(map[string][]string)
	for _, v := range list {
		if v == "" || strings.Contains(v, "SNAPSHOT") || strings.Contains(v, "-rc") {
			continue
		}
		if !strings.HasPrefix(v, "24.") && !strings.HasPrefix(v, "25.") {
			continue
		}
		b := Branch(v)
		grouped[b] = append(grouped[b], v)
	}

	var result []string
	for _, vers := range grouped {
		SortVersions(vers)
		if len(vers) > 3 {
			vers = vers[:3]
		}
		result = append(result, vers...)
	}
	SortVersions(result)
	return result
}

type Syncer struct {
	Store  *sources.Store
	Client *Client
	Logger *zap.Logger
}

func (s *Syncer) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Sync(ctx); err != nil && ctx.Err() == nil {
			s.Logger.Warn("元数据同步失败，保留旧数据", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Syncer) read(ctx context.Context, url string, value any) error {
	res, err := s.Client.Get(ctx, url)
	if err != nil {
		return err
	}
	if res.Status != 200 {
		return fmt.Errorf("%s: HTTP %d", url, res.Status)
	}
	if err := json.Unmarshal(res.Body, value); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	return nil
}

func (s *Syncer) Sync(ctx context.Context) error {
	// 同步前自动清理旧版本与快照
	_ = s.Store.PruneObsolete(ctx)

	catalog, err := s.Store.List(ctx)
	if err != nil {
		return err
	}
	defaults, err := sources.Defaults()
	if err != nil {
		return err
	}
	// 每个发行版的每个上游分别拉取；管理员可给单个 source 指定镜像站。
	groups := map[string][]sources.Source{}
	primary := map[string]sources.Source{}
	for _, source := range catalog {
		if source.Origin == "upstream" {
			branchKey := source.Distribution + "\n" + Branch(source.Version)
			before, exists := primary[branchKey]
			// 每个发行版/分支只从一个上游发现新版本：优先最新版本。
			if source.Enabled && (!exists || source.Version > before.Version) {
				primary[branchKey] = source
			}
			key := source.Distribution + "\n" + strings.TrimRight(source.UpstreamURL, "/")
			groups[key] = append(groups[key], source)
		}
	}
	failures := 0
	for _, group := range groups {
		base := strings.TrimRight(group[0].UpstreamURL, "/")
		var versions Versions
		if err := s.read(ctx, base+"/.versions.json", &versions); err != nil {
			s.Logger.Warn("版本同步失败，保留旧数据", zap.String("upstream_url", base), zap.Error(err))
			failures++
			continue
		}
		if versions.List == nil {
			s.Logger.Warn("版本列表缺失，保留旧数据", zap.String("upstream_url", base))
			failures++
			continue
		}
		branches := map[string]sources.Source{}
		for _, source := range group {
			if source.Enabled {
				branches[Branch(source.Version)] = source
			}
		}
		rawList := append([]string{versions.Upcoming}, versions.List...)
		candidates := FilterAllowedVersions(rawList)
		for _, source := range group {
			if source.Enabled && source.Version != "" && !strings.Contains(source.Version, "SNAPSHOT") {
				candidates = append(candidates, source.Version)
			}
		}
		seen := map[string]bool{}
		for _, version := range candidates {
			if version == "" || seen[version] {
				continue
			}
			seen[version] = true
			branch := Branch(version)
			template, ok := branches[branch]
			if !ok {
				continue
			}
			// 已有 source 必须使用其自己的 upstream_url，不被另一组覆盖。
			var existing *sources.Source
			for i := range catalog {
				if catalog[i].Distribution == template.Distribution && catalog[i].Version == version {
					existing = &catalog[i]
					break
				}
			}
			if existing != nil && (existing.Origin == "local" || strings.TrimRight(existing.UpstreamURL, "/") != base) {
				continue
			}
			// 单个旧版本的镜像站只刷新已有行，避免跨组抢写新版本。
			if existing == nil && base != strings.TrimRight(primary[template.Distribution+"\n"+branch].UpstreamURL, "/") {
				continue
			}
			incoming := template
			if existing != nil {
				incoming = *existing
			} else {
				for _, d := range defaults {
					if d.Distribution == template.Distribution && d.Branch == branch {
						incoming.Image, incoming.TagTemplate, incoming.PackageManager = d.Image, d.TagTemplate, d.PackageManager
						break
					}
				}
			}
			incoming.ID, incoming.Overrides = 0, []string{}
			incoming.Version, incoming.Branch, incoming.Channel = version, branch, ""
			incoming.Origin, incoming.Rolling = "upstream", strings.HasSuffix(version, "SNAPSHOT")
			switch version {
			case versions.Upcoming:
				incoming.Channel = "upcoming"
			case versions.Stable:
				incoming.Channel = "stable"
			case versions.Oldstable:
				incoming.Channel = "oldstable"
			}
			var targets map[string]any
			if err := s.read(ctx, base+"/"+VersionPath(version)+"/.targets.json", &targets); err != nil || targets == nil {
				s.Logger.Warn("target 同步失败，保留旧数据", zap.String("distribution", template.Distribution), zap.String("version", version), zap.Error(err))
				failures++
				continue
			}
			now := time.Now().UTC()
			incoming.Targets, incoming.SyncedAt = targets, &now
			if err := s.Store.SyncUpstream(ctx, incoming); err != nil {
				s.Logger.Warn("写入同步元数据失败", zap.String("version", version), zap.Error(err))
				failures++
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := s.Store.RecordSync(ctx, time.Now().UTC(), failures); err != nil {
		return err
	}
	if failures > 0 {
		return fmt.Errorf("%d 个元数据同步请求失败", failures)
	}
	return nil
}
