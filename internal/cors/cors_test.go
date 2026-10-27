package cors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/config"
)

func TestMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		cfg         config.CORS
		path        string
		origin      string
		preflight   bool
		allow       string
		credentials string
		vary        string
	}{
		{name: "public wildcard", cfg: config.Default().Server.CORS, path: "/api/v1/stats", origin: "https://selector.example", allow: "*"},
		{name: "json wildcard", cfg: config.Default().Server.CORS, path: "/json/v1/latest.json", origin: "https://selector.example", allow: "*"},
		{name: "store range preflight", cfg: config.Default().Server.CORS, path: "/store/hash/image.bin", origin: "https://selector.example", preflight: true, allow: "*"},
		{name: "public exact", cfg: config.CORS{PublicOrigins: []string{"https://selector.example"}}, path: "/api/v1/stats", origin: "https://selector.example", allow: "https://selector.example", vary: "Origin"},
		{name: "public rejected", cfg: config.CORS{PublicOrigins: []string{"https://selector.example"}}, path: "/api/v1/stats", origin: "https://other.example", vary: "Origin"},
		{name: "public disabled", cfg: config.CORS{}, path: "/api/v1/stats", origin: "https://selector.example"},
		{name: "public preflight", cfg: config.Default().Server.CORS, path: "/api/v1/build", origin: "https://selector.example", preflight: true, allow: "*"},
		{name: "no origin", cfg: config.Default().Server.CORS, path: "/api/v1/stats"},
		{name: "admin exact", cfg: config.CORS{AdminOrigins: []string{"https://admin.example"}}, path: "/admin/api/jobs", origin: "https://admin.example", allow: "https://admin.example", credentials: "true", vary: "Origin"},
		{name: "admin preflight", cfg: config.CORS{AdminOrigins: []string{"https://admin.example"}}, path: "/admin/api/jobs", origin: "https://admin.example", preflight: true, allow: "https://admin.example", credentials: "true", vary: "Origin"},
		{name: "admin rejected", cfg: config.CORS{AdminOrigins: []string{"https://admin.example"}}, path: "/admin/api/jobs", origin: "https://admin.example.evil", vary: "Origin"},
		{name: "admin default", cfg: config.Default().Server.CORS, path: "/admin/api/jobs", origin: "https://admin.example"},
		{name: "admin static", cfg: config.CORS{AdminOrigins: []string{"https://admin.example"}}, path: "/admin/index.html", origin: "https://admin.example"},
		{name: "unrelated path", cfg: config.Default().Server.CORS, path: "/healthz", origin: "https://selector.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(New(tc.cfg))
			called := false
			router.Use(func(c *gin.Context) {
				called = true
				// 模拟业务处理或管理鉴权，预检不得执行到此处。
				c.AbortWithStatus(http.StatusUnauthorized)
			})
			method := http.MethodGet
			if tc.preflight {
				method = http.MethodOptions
			}
			req := httptest.NewRequest(method, tc.path, nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.preflight {
				req.Header.Set("Access-Control-Request-Method", "POST")
				req.Header.Set("Access-Control-Request-Headers", "Content-Type, Range, If-Modified-Since, If-None-Match")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			wantStatus := http.StatusUnauthorized
			if tc.preflight {
				wantStatus = http.StatusNoContent
			}
			if response.Code != wantStatus || called == tc.preflight {
				t.Fatalf("status=%d called=%v", response.Code, called)
			}
			for key, want := range map[string]string{
				"Access-Control-Allow-Origin":      tc.allow,
				"Access-Control-Allow-Credentials": tc.credentials,
				"Vary":                             tc.vary,
			} {
				if got := response.Header().Get(key); got != want {
					t.Errorf("%s=%q, want %q", key, got, want)
				}
			}
			if tc.allow == "" {
				for key := range response.Header() {
					if strings.HasPrefix(key, "Access-Control-") {
						t.Errorf("意外的 CORS 头: %s", key)
					}
				}
				return
			}
			if got := response.Header().Get("Access-Control-Max-Age"); got != "86400" {
				t.Errorf("max age=%q", got)
			}
			wantMethods := "GET, HEAD, POST, OPTIONS"
			wantHeaders := "Content-Type, Range, If-Modified-Since, If-None-Match"
			wantExpose := "X-Imagebuilder-Status, X-Queue-Position, Content-Length, Content-Range, ETag, Last-Modified"
			if tc.credentials != "" {
				wantMethods = "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS"
				wantHeaders = "Content-Type, Authorization, X-Requested-With"
				wantExpose = ""
			}
			for key, want := range map[string]string{
				"Access-Control-Allow-Methods":  wantMethods,
				"Access-Control-Allow-Headers":  wantHeaders,
				"Access-Control-Expose-Headers": wantExpose,
			} {
				if got := response.Header().Get(key); got != want {
					t.Errorf("%s=%q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestOrdinaryOptions(t *testing.T) {
	for _, origin := range []string{"", "https://selector.example"} {
		router := gin.New()
		router.Use(New(config.Default().Server.CORS))
		router.OPTIONS("/api/v1/build", func(c *gin.Context) { c.Status(http.StatusAccepted) })
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/build", nil)
		req.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusAccepted {
			t.Fatalf("非预检 OPTIONS 被拦截: %d", response.Code)
		}
	}
}
