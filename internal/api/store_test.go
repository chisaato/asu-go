package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/store"
)

func TestStoreDownloads(t *testing.T) {
	hash := strings.Repeat("a", 64)
	dir := t.TempDir()
	job := filepath.Join(dir, hash)
	if err := os.MkdirAll(filepath.Join(job, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterStore(router, &store.Local{Dir: dir})
	for _, name := range []string{"image.bin", "profiles.json", "test.txt", "image.manifest", "sha256sums", "sub/image.bin"} {
		if err := os.WriteFile(filepath.Join(job, name), []byte("firmware"), 0644); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/store/"+hash+"/"+name, nil))
		if w.Code != 200 || w.Body.String() != "firmware" || w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal(w)
		}
	}
	for _, tc := range []struct {
		method, header, value string
		status                int
		body                  string
	}{
		{"HEAD", "", "", 200, ""}, {"GET", "Range", "bytes=1-3", 206, "irm"},
		{"GET", "If-Modified-Since", "Wed, 31 Dec 2098 00:00:00 GMT", 304, ""},
	} {
		req := httptest.NewRequest(tc.method, "/store/"+hash+"/image.bin", nil)
		if tc.header != "" {
			req.Header.Set(tc.header, tc.value)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatal(tc, w)
		}
	}
	for _, name := range []string{"missing", "../image.bin", "%2e%2e/image.bin", "%2Fetc/passwd", "sub/%2e%2e/image.bin", ".secret", "sub/.hidden", "sub%5cimage.bin", "sub/", ""} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/store/"+hash+"/"+name, nil))
		if w.Code != 404 || w.Body.String() != `{"detail":"Not found"}` {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
	}
}
