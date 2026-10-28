package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/api"
	"github.com/chisaato/asu-go/internal/metadata"
)

func TestProfileAndIndexCompatibility(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/24.10.1/targets/x86/64/profiles.json":
			_, _ = fmt.Fprint(w, `{"version_code":"r123","source_date_epoch":1700000000,"arch_packages":"x86_64","linux_kernel":{"version":"6.6","release":"1","vermagic":"abc"},"profiles":{"generic":{"device_packages":["base"],"title":"Generic"}}}`)
		case "/releases/24.10.1/targets/x86/64/packages/index.json":
			_, _ = fmt.Fprint(w, `{"version":2,"architecture":"x86_64","packages":{"base":"1"}}`)
		case "/releases/24.10.1/targets/x86/64/kmods/6.6-1-abc/index.json":
			_, _ = fmt.Fprint(w, `{"version":2,"packages":{"kmod-tun":"2"}}`)
		case "/releases/24.10.1/packages/x86_64/feeds.conf":
			_, _ = fmt.Fprint(w, "src/gz base https://not-used.invalid/base\nsrc/gz luci https://not-used.invalid/luci\n")
		case "/releases/24.10.1/packages/x86_64/base/index.json":
			_, _ = fmt.Fprint(w, `{"version":2,"packages":{"foo":"1","duplicate":"old"}}`)
		case "/releases/24.10.1/packages/x86_64/luci/index.json":
			_, _ = fmt.Fprint(w, `{"version":2,"packages":{"luci":"2","duplicate":"new"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	store, _ := fixture(t, upstream.URL)
	service := &metadata.Service{Store: store, Client: metadata.NewClient("test"), Cache: &metadata.Cache{Dir: t.TempDir(), TTL: time.Hour, Client: metadata.NewClient("test")}}
	router := gin.New()
	api.RegisterMetadata(router, service)
	for _, tc := range []struct {
		path, want string
		status     int
	}{
		{"releases/24.10.1/targets/x86/64/generic.json", `{"version_code":"r123","source_date_epoch":1700000000,"arch_packages":"x86_64","linux_kernel":{"version":"6.6","release":"1","vermagic":"abc"},"device_packages":["base"],"title":"Generic","id":"generic","build_at":"2023-11-14T22:13:20.000000Z"}`, 200},
		{"releases/24.10.1/targets/x86/64/missing.json", `{"detail":"Profile not found: missing"}`, 404},
		{"releases/24.10.1/targets/x86/64/index.json", `{"architecture":"x86_64","packages":{"base":"1","kmod-tun":"2"}}`, 200},
		{"releases/24.10.1/packages/x86_64-index.json", `{"foo":"1","luci":"2","duplicate":"new"}`, 200},
		{"releases/24.10.1/packages/absent-index.json", `{}`, 200},
		{"releases/24.10.1/targets/absent/absent/index.json", `{}`, 200},
		{"releases/24.10.1/targets/x86/64/../../escape.json", `{"detail":"Invalid metadata path","status":400}`, 400},
		{"releases/24.10.1/packages/%2e%2e/x-index.json", `{"detail":"Invalid metadata path","status":400}`, 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/json/v1/"+tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			equalJSON(t, w.Body.Bytes(), tc.want)
		})
	}
	// provider 合并 target 包与 feeds，接口无需 HTTP 上下文。
	packages, err := service.Packages(context.Background(), "openwrt", "24.10.1", "x86/64")
	if err != nil || packages["kmod-tun"] != "2" || packages["luci"] != "2" {
		t.Fatalf("provider packages=%v err=%v", packages, err)
	}
	if _, err := service.Targets(context.Background(), "openwrt", "24.10.1"); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataUncachedUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer upstream.Close()
	store, _ := fixture(t, upstream.URL)
	router := gin.New()
	api.RegisterMetadata(router, &metadata.Service{Store: store, Cache: &metadata.Cache{Dir: t.TempDir(), Client: metadata.NewClient("test")}})
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"releases/24.10.1/targets/x86/64/generic.json", 500},
		{"releases/24.10.1/targets/x86/64/index.json", 200},
		{"releases/24.10.1/packages/x86_64-index.json", 200},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/json/v1/"+tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if tc.status == 200 && strings.TrimSpace(w.Body.String()) != "{}" {
			t.Fatal(w.Body.String())
		}
	}
}
