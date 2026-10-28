package queue

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/buildreq"
	"github.com/chisaato/asu-go/internal/db"
	"github.com/chisaato/asu-go/internal/sources"
)

type changesMetadata struct{}

func (changesMetadata) Targets(context.Context, string, string) (map[string]any, error) {
	return map[string]any{"mediatek/filogic": "aarch64"}, nil
}
func (changesMetadata) Packages(context.Context, string, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (changesMetadata) Profile(context.Context, string, string, string, string) (map[string]any, error) {
	return map[string]any{"id": "tplink_archer-c6-v2", "default_packages": []string{"owut", "fitblk"}}, nil
}

func TestJobPackageChanges(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err = database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	catalog := sources.NewStore(database)
	source := sources.NewSource()
	source.Distribution, source.Version, source.Branch = "openwrt", "24.10.0", "24.10"
	source.Image, source.UpstreamURL, source.TagTemplate = "docker.io/library/alpine", "https://example.com", "latest"
	if _, err = catalog.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	q := NewSQLite(database.DB, catalog, changesMetadata{}, nil, Options{
		DataDir: dir, MaxPendingJobs: 20, BuildTTL: time.Hour, BuildUnversionedTTL: time.Hour, Retention: time.Hour,
	})
	for _, diff := range []bool{false, true} {
		for _, versions := range []bool{false, true} {
			r := buildreq.NewRequest()
			r.Version, r.Target, r.Profile = "24.10.0", "mediatek/filogic", "alias"
			r.DiffPackages = diff
			r.Packages = []string{"auc", "luci-i18n-opkg-zh-cn"}
			want := []string{"luci-i18n-package-manager-zh-cn", "owut", "ipq-wifi-tplink_archer-c6-v2", "fitblk"}
			if versions {
				r.Packages = []string{"ignored"}
				r.PackagesVersions = map[string]string{"auc": "1"}
				want = want[1:]
			}
			r = r.Normalize()
			id, err := r.Hash()
			if err != nil {
				t.Fatal(err)
			}
			resp, err := q.Submit(ctx, r, "")
			if err != nil || resp.Status != 202 || resp.Body["request_hash"] != id {
				t.Fatalf("submit: %+v, %v", resp, err)
			}
			job, err := q.job(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			// 差异包仍由 Runner 计算，不能把未修正的包或提前产生的负包传给它。
			if job.DiffPackages != diff || !slices.Equal(job.Packages, want) || job.Profile != "tplink_archer-c6-v2" {
				t.Fatalf("diff=%v versions=%v: job=%+v, want packages %v", diff, versions, job, want)
			}
			if !maps.Equal(job.PackageVersions, r.PackagesVersions) {
				t.Fatal("版本约束被改写")
			}
			var options string
			if err = q.DB.QueryRowContext(ctx, "SELECT options FROM jobs WHERE request_hash=?", id).Scan(&options); err != nil {
				t.Fatal(err)
			}
			var stats struct {
				Packages []string `json:"stat_packages"`
			}
			if err = json.Unmarshal([]byte(options), &stats); err != nil {
				t.Fatal(err)
			}
			statWant := []string{"luci-i18n-package-manager-zh-cn", "ipq-wifi-tplink_archer-c6-v2"}
			if versions {
				statWant = statWant[1:]
			}
			if !slices.Equal(stats.Packages, statWant) {
				t.Fatalf("统计应使用修正后非默认包: got %v, want %v", stats.Packages, statWant)
			}
			var raw string
			if err = q.DB.QueryRowContext(ctx, "SELECT request FROM jobs WHERE request_hash=?", id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var stored buildreq.Request
			if err = json.Unmarshal([]byte(raw), &stored); err != nil {
				t.Fatal(err)
			}
			storedID, err := stored.Hash()
			if err != nil || storedID != id || !slices.Equal(stored.Packages, r.Packages) || stored.Profile != "alias" {
				t.Fatalf("原请求/哈希被规则改写: %s, %v", raw, err)
			}
		}
	}
}
