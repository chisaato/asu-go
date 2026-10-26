package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	register(r, fstest.MapFS{
		"index.html":   {Data: []byte("<html>Nuxt index</html>")},
		"_nuxt/app.js": {Data: []byte("console.log('asset')")},
	})
	r.GET("/admin/api/health", func(c *gin.Context) { c.String(http.StatusOK, "api") })
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/admin", 200, "<html>Nuxt index</html>"},
		{"/admin/", 200, "<html>Nuxt index</html>"},
		{"/admin/login", 200, "<html>Nuxt index</html>"},
		{"/admin/tasks/deep/path", 200, "<html>Nuxt index</html>"},
		{"/admin/_nuxt/app.js", 200, "console.log('asset')"},
		{"/admin/api/x", 404, "404 page not found"},
		{"/admin/api", 404, "404 page not found"},
		{"/admin/api/health", 200, "api"},
		{"/elsewhere", 404, "404 page not found"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status || w.Body.String() != tc.body {
				t.Fatalf("got %d %q, want %d %q", w.Code, w.Body.String(), tc.status, tc.body)
			}
		})
	}
}

func TestRegisterWithoutGenerate(t *testing.T) {
	r := gin.New()
	register(r, fstest.MapFS{"placeholder.html": {Data: []byte("请执行 make web")}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if w.Code != http.StatusOK || w.Body.String() != "请执行 make web" {
		t.Fatalf("placeholder: %d %q", w.Code, w.Body.String())
	}
}
