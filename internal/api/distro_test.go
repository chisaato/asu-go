package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/api"
	"github.com/chisaato/asu-go/internal/config"
	"github.com/chisaato/asu-go/internal/cors"
	"github.com/chisaato/asu-go/internal/metadata"
	"github.com/chisaato/asu-go/internal/sources"
	"github.com/chisaato/asu-go/internal/store"
	"github.com/chisaato/asu-go/internal/webui"
)

func TestDistroMetadataMirrors(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		distro := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
		switch {
		case strings.HasSuffix(r.URL.Path, "profiles.json"):
			_, _ = fmt.Fprintf(w, `{"version_code":%q,"profiles":{"generic":{"marker":%q}}}`, distro, distro)
		case strings.HasSuffix(r.URL.Path, "index.json"):
			_, _ = fmt.Fprintf(w, `{"version":2,"architecture":%q,"packages":{}}`, distro)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	catalog, _ := fixture(t, upstream.URL+"/openwrt")
	all := mustList(t, catalog)
	openwrt := all[0]
	openwrt.Version, openwrt.Branch = "25.12.6", "25.12"
	immortal := openwrt
	immortal.Distribution, immortal.Version, immortal.Branch = "immortalwrt", "25.12.2", "25.12"
	immortal.UpstreamURL = upstream.URL + "/immortalwrt"
	immortal.Targets = map[string]any{"immortal/target": "immortal"}
	if err := catalog.Import(ctx, []sources.Source{openwrt, immortal}); err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateCatalog(ctx, catalog, "openwrt"); err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateCatalog(ctx, catalog, "missing"); err == nil || !strings.Contains(err.Error(), "server.default_distro") {
		t.Fatalf("默认发行版未校验: %v", err)
	}
	meta := &metadata.Service{Store: catalog, Cache: &metadata.Cache{Dir: t.TempDir(), Client: metadata.NewClient("test")}}
	backend := &store.Local{Dir: t.TempDir()}
	hash := strings.Repeat("a", 64)
	if err := os.MkdirAll(filepath.Join(backend.Dir, hash), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backend.Dir, hash, "image.bin"), []byte("firmware"), 0600); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(api.DistroScope(catalog, "openwrt"), cors.New(config.Default().Server.CORS))
	webui.Register(router)
	router.GET("/admin/api/check", func(c *gin.Context) { c.String(200, "admin") })
	for _, routes := range []gin.IRouter{router, router.Group("/:distro")} {
		api.RegisterMetadata(routes, meta)
		api.RegisterStore(routes, backend)
	}
	get := func(method, path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Origin", "https://selector.example")
		if method == "OPTIONS" {
			req.Header.Set("Access-Control-Request-Method", "POST")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w
	}
	for _, tc := range []struct{ prefix, distro, version, excluded string }{
		{"", "openwrt", "25.12.6", "25.12.2"},
		{"/openwrt", "openwrt", "25.12.6", "25.12.2"},
		{"/immortalwrt", "immortalwrt", "25.12.2", "25.12.6"},
	} {
		t.Run(tc.distro+tc.prefix, func(t *testing.T) {
			latest := get("GET", tc.prefix+"/json/v1/latest.json", 200)
			equalJSON(t, latest.Body.Bytes(), `{"latest":["`+tc.version+`"]}`)
			t.Logf("GET %s/json/v1/latest.json => %s", tc.prefix, latest.Body.String())
			if latest.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Fatal("前缀 CORS 未生效")
			}
			for _, name := range []string{"overview", "branches"} {
				w := get("GET", tc.prefix+"/json/v1/"+name+".json", 200)
				if !strings.Contains(w.Body.String(), tc.version) || strings.Contains(w.Body.String(), tc.excluded) {
					t.Fatalf("发行版视图泄漏: %s", w.Body)
				}
			}
			for _, name := range []string{"latest", "overview"} {
				w := get("GET", tc.prefix+"/api/v1/"+name, 301)
				if w.Header().Get("Location") != tc.prefix+"/json/v1/"+name+".json" {
					t.Fatal("重定向丢失前缀")
				}
			}
			w := get("GET", tc.prefix+"/api/v1/revision/"+tc.version+"/x86/64", 200)
			equalJSON(t, w.Body.Bytes(), `{"revision":"`+tc.distro+`"}`)
			path := tc.prefix + "/json/v1/releases/" + tc.version + "/targets/x86/64/"
			w = get("GET", path+"generic.json", 200)
			if !strings.Contains(w.Body.String(), `"marker":"`+tc.distro+`"`) {
				t.Fatal(w.Body)
			}
			w = get("GET", path+"index.json", 200)
			if !strings.Contains(w.Body.String(), `"architecture":"`+tc.distro+`"`) {
				t.Fatal(w.Body)
			}
			get("OPTIONS", tc.prefix+"/api/v1/build", 204)
			w = get("GET", tc.prefix+"/store/"+hash+"/image.bin", 200)
			if w.Body.String() != "firmware" {
				t.Fatal(w.Body)
			}
			get("HEAD", tc.prefix+"/store/"+hash+"/image.bin", 200)
		})
	}
	for _, prefix := range []string{"unknown", "admin", "api", "json", "store", "healthz", "stats"} {
		get("GET", "/"+prefix+"/json/v1/latest.json", 404)
	}
	get("OPTIONS", "/unknown/api/v1/build", 404)
	get("GET", "/admin", 200)
	get("GET", "/admin/api/check", 200)
	get("GET", "/admin/api/v1/latest", 404)
	get("GET", "/immortalwrt/admin/api/check", 404)
	// 禁用在运行时立即生效；默认发行版失效给出清晰错误而非混用其他发行版。
	all = mustList(t, catalog)
	for _, item := range all {
		if _, err := catalog.Patch(ctx, item.ID, map[string]json.RawMessage{"enabled": json.RawMessage("false")}); err != nil {
			t.Fatal(err)
		}
	}
	get("GET", "/immortalwrt/json/v1/latest.json", 404)
	w := get("GET", "/json/v1/latest.json", 503)
	if !strings.Contains(w.Body.String(), "server.default_distro") {
		t.Fatal(w.Body)
	}
}

func TestCatalogRejectsLegacyReservedDistribution(t *testing.T) {
	catalog, database := fixture(t, "https://example.com")
	if _, err := database.ExecContext(context.Background(), "UPDATE sources SET distribution='admin'"); err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateCatalog(context.Background(), catalog, "admin"); err == nil || !strings.Contains(err.Error(), "保留路由") {
		t.Fatalf("旧目录未拒绝保留词: %v", err)
	}
}
