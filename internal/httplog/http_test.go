package httplog

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/logcontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestHTTPLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		path   string
		status int
		level  zapcore.Level
	}{
		{"/jobs/abc", 200, zapcore.InfoLevel},
		{"/jobs/abc", 400, zapcore.WarnLevel},
		{"/jobs/abc", 500, zapcore.ErrorLevel},
		{"/healthz", 200, zapcore.DebugLevel},
		{"/immortalwrt/healthz", 200, zapcore.DebugLevel},
		{"/immortalwrt/healthz", 404, zapcore.WarnLevel},
		{"/admin/_nuxt/app.js", 200, zapcore.DebugLevel},
	} {
		t.Run(tc.path+http.StatusText(tc.status), func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			r := gin.New()
			r.Use(HTTP(zap.New(core)))
			r.GET("/jobs/:id", func(c *gin.Context) {
				if logcontext.RequestID(c.Request.Context()) != "trace-123" {
					t.Error("context 缺少 request_id")
				}
				c.Status(tc.status)
			})
			r.GET("/healthz", func(c *gin.Context) { c.Status(tc.status) })
			r.GET("/:distro/healthz", func(c *gin.Context) { c.Status(tc.status) })
			r.GET("/admin/_nuxt/*asset", func(c *gin.Context) { c.Status(tc.status) })
			req := httptest.NewRequest(http.MethodGet, tc.path+"?token=secret-query&password=secret-password", nil)
			req.Header.Set("X-Request-Id", "trace-123")
			req.Header.Set("Authorization", "Bearer secret-auth")
			req.Header.Set("Cookie", "secret-cookie")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Header().Get("X-Request-Id") != "trace-123" {
				t.Fatal(w.Header())
			}
			entries := logs.All()
			if len(entries) != 1 || entries[0].Level != tc.level {
				t.Fatal(entries)
			}
			fields := entries[0].ContextMap()
			if fields["path"] != tc.path || fields["request_id"] != "trace-123" || fields["status"] != int64(tc.status) {
				t.Fatal(fields)
			}
			if strings.Contains(tc.path, "/jobs/") && fields["route"] != "/jobs/:id" {
				t.Fatal(fields)
			}
			if strings.HasPrefix(tc.path, "/immortalwrt/") && fields["route"] != "/:distro/healthz" {
				t.Fatal(fields)
			}
			for _, f := range entries[0].Context {
				if strings.Contains(f.String, "secret-") {
					t.Fatalf("敏感值泄漏: %v", f)
				}
			}
		})
	}
}

func TestHandlerErrorContext(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	r := gin.New()
	r.Use(HTTP(zap.New(core)))
	r.GET("/failure", func(c *gin.Context) {
		_ = c.Error(errors.New("数据库不可用"))
		c.Set("request_hash", "test-hash")
		c.Status(500)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/failure", nil))
	fields := logs.All()[0].ContextMap()
	if fields["error"] != "数据库不可用" || fields["request_hash"] != "test-hash" {
		t.Fatal(fields)
	}
}

func TestGeneratedIDAndPanic(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	})
	r.Use(HTTP(zap.New(core)))
	r.GET("/panic", func(*gin.Context) { panic("private-panic-data") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if len(w.Header().Get("X-Request-Id")) != 32 || w.Code != 500 {
		t.Fatal(w)
	}
	if logs.Len() != 1 || logs.All()[0].Level != zapcore.ErrorLevel || logs.All()[0].ContextMap()["status"] != int64(500) {
		t.Fatal(logs.All())
	}
}
