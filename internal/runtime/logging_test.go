package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestEngineLifecycleLogging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.53")
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/start") || r.Method == http.MethodDelete:
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/wait"):
			_, _ = io.WriteString(w, `{"StatusCode":0}`)
		case strings.HasSuffix(r.URL.Path, "/archive") && r.Method == http.MethodPut:
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/archive"):
			w.Header().Set("X-Docker-Container-Path-Stat", "e30=")
			_, _ = io.WriteString(w, "archive-content")
		default:
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	core, logs := observer.New(zapcore.DebugLevel)
	rt, err := Open("docker", Options{Socket: server.URL, Logger: zap.New(core)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close() }()
	ctx := context.Background()
	h := Handle("opaque-handle")
	if err := rt.PutArchive(ctx, h, "/builder", strings.NewReader("input-content")); err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(ctx, h); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Wait(ctx, h); err != nil {
		t.Fatal(err)
	}
	r, err := rt.GetArchive(ctx, h, "/builder/output")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Remove(ctx, h); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"start", "wait", "remove", "put_archive", "get_archive"} {
		entries := logs.FilterField(zap.String("operation", op)).FilterLevelExact(zapcore.InfoLevel).All()
		if len(entries) != 1 || entries[0].ContextMap()["container"] != string(h) {
			t.Fatalf("%s: %v", op, entries)
		}
		if op == "get_archive" && entries[0].ContextMap()["bytes"] != int64(len("archive-content")) {
			t.Fatal(entries)
		}
		if op == "put_archive" && entries[0].ContextMap()["bytes"] != int64(len("input-content")) {
			t.Fatal(entries)
		}
	}
	if logs.FilterMessage("引擎 API 请求").Len() < 5 || logs.FilterMessage("引擎 API 响应").Len() < 5 {
		t.Fatal(logs.All())
	}
}
