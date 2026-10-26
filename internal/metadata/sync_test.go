package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/sources"
	"go.uber.org/zap"
)

func TestDistributionAndSourceUpstreams(t *testing.T) {
	ctx := context.Background()
	upstream := func(marker, version string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.versions.json" {
				_, _ = fmt.Fprintf(w, `{"upcoming_version":"","stable_version":%q,"oldstable_version":"","versions_list":[%q,"24.10.1","24.10.6","24.10.8"]}`, version, version)
				return
			}
			_, _ = fmt.Fprintf(w, `{"marker":%q}`, marker)
		}))
	}
	openwrt := upstream("openwrt", "24.10.2")
	defer openwrt.Close()
	immortal := upstream("immortalwrt", "24.10.3")
	defer immortal.Close()
	mirror := upstream("mirror", "24.10.4")
	defer mirror.Close()
	database, err := db.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := sources.NewStore(database)
	if err := store.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	filtered := list[:0]
	for _, source := range list {
		if source.Branch == "25.12" {
			if err := store.Delete(ctx, source.ID); err != nil {
				t.Fatal(err)
			}
			continue
		}
		filtered = append(filtered, source)
	}
	list = filtered
	for i := range list {
		if list[i].Distribution == "openwrt" {
			list[i].UpstreamURL = openwrt.URL
		} else {
			list[i].UpstreamURL = immortal.URL
		}
	}
	// 一个发行版的单个旧版本可使用独立 mirror，不能被另一 URL 的同步覆盖。
	extra := list[0]
	extra.ID = 0
	extra.Distribution, extra.Version, extra.Branch, extra.UpstreamURL = "openwrt", "24.10.1", "24.10", mirror.URL
	list = append(list, extra)
	if err := store.Import(ctx, list); err != nil {
		t.Fatal(err)
	}
	// Import 的本地 overrides 仅影响 URL，不能阻止正常同步 targets。
	syncer := Syncer{Store: store, Client: NewClient("test"), Logger: zap.NewNop()}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	list, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range list {
		want := "openwrt"
		if source.Distribution == "immortalwrt" {
			want = "immortalwrt"
		}
		if source.Distribution == "openwrt" && source.Version == "24.10.1" {
			want = "mirror"
		}
		if source.Targets["marker"] != want {
			t.Fatalf("source 使用了错误的上游: %+v，期望 %s", source, want)
		}
		if source.Version == "SNAPSHOT" && !source.Rolling {
			t.Fatal("主线快照应 rolling")
		}
	}
}

func TestClientTimeoutAndUncached304(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/304" {
			w.WriteHeader(304)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	client := NewClient("test")
	if _, err := client.Get(context.Background(), server.URL+"/304"); err == nil {
		t.Fatal("无缓存的 304 应失败")
	}
	client.HTTP.Timeout = 10 * time.Millisecond
	if _, err := client.Get(context.Background(), server.URL+"/timeout"); err == nil {
		t.Fatal("未执行超时")
	}
}

func TestRunRefreshesAndCancels(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".versions.json") {
			calls.Add(1)
			_, _ = fmt.Fprint(w, `{"upcoming_version":"","stable_version":"24.10.2","oldstable_version":"","versions_list":["24.10.2"]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	database, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := sources.NewStore(database)
	source := sources.NewSource()
	source.Distribution, source.Version, source.Branch, source.UpstreamURL, source.Image, source.Origin = "openwrt", "24.10.1", "24.10", server.URL, "test/image", "upstream"
	if err := store.Import(context.Background(), []sources.Source{source}); err != nil {
		t.Fatal(err)
	}
	syncer := Syncer{Store: store, Client: NewClient("test"), Logger: zap.NewNop()}
	if err := syncer.Run(ctx, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if calls.Load() < 2 {
		t.Fatalf("没有启动即时刷新和周期刷新: %d", calls.Load())
	}
}
