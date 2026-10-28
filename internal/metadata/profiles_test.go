package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/db"
	"github.com/chisaato/asu-go/internal/sources"
)

func TestPackageIndexes(t *testing.T) {
	for _, version := range []string{"23.05.5", "23.05.6", "23.05-SNAPSHOT", "24.10.0-rc1", "SNAPSHOT"} {
		t.Run(version, func(t *testing.T) {
			var mu sync.Mutex
			requests := []string{}
			base := VersionPath(version) + "/targets/x/y"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.URL.Path)
				mu.Unlock()
				switch r.URL.Path {
				case "/" + base + "/packages/index.json":
					_, _ = fmt.Fprint(w, `{"version":2,"architecture":"test_arch","packages":{"base":"1"}}`)
				case "/" + base + "/profiles.json":
					_, _ = fmt.Fprint(w, `{"linux_kernel":{"version":"6.6.1","release":"1","vermagic":"abc"}}`)
				case "/" + base + "/kmods/6.6.1-1-abc/index.json":
					_, _ = fmt.Fprint(w, `{"version":2,"packages":{"kmod-test":"2"}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer upstream.Close()
			s := &Service{Cache: &Cache{Dir: t.TempDir(), TTL: time.Hour, Client: NewClient("test")}}
			source := &sources.Source{UpstreamURL: upstream.URL}
			index, err := s.targetIndex(context.Background(), source, base)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"base": "1"}
			if version != "23.05.5" {
				want["kmod-test"] = "2"
			}
			if !reflect.DeepEqual(packageMap(index), want) || index["version"] != nil || index["architecture"] != "test_arch" {
				t.Fatalf("索引不符: %#v", index)
			}
			mu.Lock()
			defer mu.Unlock()
			if version == "23.05.5" && len(requests) != 1 {
				t.Fatalf("pre-split 不应请求 kernel: %v", requests)
			}
			for _, path := range requests {
				if strings.HasSuffix(path, "/Packages") {
					t.Fatal("v2 不应回退")
				}
			}
		})
	}
}

func TestPostKmodSplitPythonRules(t *testing.T) {
	for path, want := range map[string]bool{
		"snapshots/targets/x/y":               true,
		"releases/24.10.0-rc1/targets/x/y":    true,
		"releases/23.05-SNAPSHOT/targets/x/y": true,
		"releases/23.05.6/targets/x/y":        true,
		"releases/23.05.9/targets/x/y":        true,
		"releases/23.05.5/targets/x/y":        false,
		"releases/23.05.10/targets/x/y":       false,
		"releases/22.03.7/targets/x/y":        false,
	} {
		if got := PostKmodSplit(path); got != want {
			t.Errorf("%s: %v != %v", path, got, want)
		}
	}
}

func TestPackagesFallback(t *testing.T) {
	for _, mode := range []string{"v1", "missing", "fallback-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "index.json") {
					if mode == "missing" {
						w.WriteHeader(404)
						return
					}
					_, _ = fmt.Fprint(w, `{"architecture":"old","packages":{"libfoo123":"old"}}`)
					return
				}
				if mode == "fallback-unavailable" {
					w.WriteHeader(404)
					return
				}
				_, _ = fmt.Fprint(w, "Package: common\r\nVersion: 1\r\nArchitecture: all\r\n\r\nPackage: libfoo123\r\nABIVersion: 123\r\nVersion: 2\r\nArchitecture: aarch64\r\nDescription: a\r\n continuation\r\n\r\n")
			}))
			defer upstream.Close()
			s := &Service{Cache: &Cache{Dir: t.TempDir(), Client: NewClient("test")}}
			index, err := s.packageIndex(context.Background(), &sources.Source{UpstreamURL: upstream.URL}, "packages")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "fallback-unavailable" {
				if packageMap(index)["libfoo123"] != "old" {
					t.Fatal(index)
				}
			} else if !reflect.DeepEqual(packageMap(index), map[string]string{"common": "1", "libfoo": "2"}) || index["architecture"] != "aarch64" {
				t.Fatal(index)
			}
		})
	}
}

// 只读真实上游冒烟，默认不依赖网络；由 ASU_METADATA_SMOKE=1 显式启用。
func TestOpenWrtSmoke(t *testing.T) {
	if os.Getenv("ASU_METADATA_SMOKE") != "1" {
		t.Skip("未启用真实上游冒烟")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := sources.NewStore(database)
	source := sources.NewSource()
	source.Distribution, source.Version, source.Branch = "openwrt", "24.10.1", "24.10"
	source.Image, source.UpstreamURL = "ghcr.io/openwrt/imagebuilder", "https://downloads.openwrt.org"
	if err := store.Import(ctx, []sources.Source{source}); err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: store, Cache: &Cache{Dir: t.TempDir(), TTL: time.Hour, Client: NewClient("asu-go/smoke")}}
	profile, err := s.Profile(ctx, "openwrt", "24.10.1", "x86/64", "generic")
	if err != nil {
		t.Fatal(err)
	}
	index, err := s.JSONPath(ctx, "openwrt", "releases/24.10.1/targets/x86/64/index.json")
	if err != nil {
		t.Fatal(err)
	}
	data := index.(map[string]any)
	packages := packageMap(data)
	if packages["kmod-tun"] == "" {
		t.Fatalf("kmod 未合并，包数 %d", len(packages))
	}
	encoded, _ := json.Marshal(profile["linux_kernel"])
	t.Logf("profile=%v kernel=%s packages=%d kmod-tun=%s", profile["id"], encoded, len(packages), packages["kmod-tun"])
}

func TestResolveBuildProfile(t *testing.T) {
	for _, tc := range []struct{ name, profiles, requested, want string }{
		{"alias", `{"board":{"supported_devices":["vendor,model"]}}`, "vendor_model", "board"},
		{"generic", `{"generic":{}}`, "unknown", "generic"},
		{"missing", `{"board":{}}`, "unknown", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"default_packages":["base"],"profiles":%s}`, tc.profiles)
			}))
			defer upstream.Close()
			ctx := context.Background()
			database, err := db.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close() }()
			if err = database.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			store := sources.NewStore(database)
			source := sources.NewSource()
			source.Distribution = "openwrt"
			source.Version = "24.10.0"
			source.Branch = "24.10"
			source.Image = "test"
			source.UpstreamURL = upstream.URL
			if _, err = store.Create(ctx, source); err != nil {
				t.Fatal(err)
			}
			s := &Service{Store: store, Cache: &Cache{Dir: t.TempDir(), TTL: time.Hour, Client: NewClient("test")}}
			profile, err := s.ResolveBuildProfile(ctx, "openwrt", "24.10.0", "x86/64", tc.requested)
			if tc.want == "" {
				if _, ok := err.(*ProfileNotFoundError); !ok {
					t.Fatal(err)
				}
				return
			}
			if err != nil || profile["id"] != tc.want || profile["profiles"] != nil {
				t.Fatal(profile, err)
			}
		})
	}
}
