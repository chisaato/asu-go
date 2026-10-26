package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDiskCacheRevalidationAndStale(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("If-None-Match") == `"test"` && r.Header.Get("If-Modified-Since") == "Mon, 01 Jan 2024 00:00:00 GMT" {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"test"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	core, logs := observer.New(zap.WarnLevel)
	c := &Cache{Dir: t.TempDir(), TTL: time.Hour, Client: NewClient("test"), Logger: zap.New(core)}
	ctx := context.Background()
	get := func() {
		t.Helper()
		body, err := c.get(ctx, upstream.URL, "releases/24.10.1/targets/x/y/profiles.json")
		if err != nil || string(body) != `{"ok":true}` {
			t.Fatalf("body=%s err=%v", body, err)
		}
	}
	get()
	get()
	if calls.Load() != 1 {
		t.Fatal("TTL 未命中")
	}
	// 新 Cache 模拟重启；必须从磁盘读取验证器并接受 304。
	c = &Cache{Dir: c.Dir, Client: c.Client, Logger: c.Logger}
	get()
	if calls.Load() != 2 {
		t.Fatal("未执行条件请求")
	}
	files, err := filepath.Glob(filepath.Join(c.Dir, "metadata", "*", "releases", "24.10.1", "targets", "x", "y", "profiles.json.meta.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("边车: %v %v", files, err)
	}
	encoded, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var meta cacheMeta
	if err := json.Unmarshal(encoded, &meta); err != nil || meta.ETag != `"test"` || meta.FetchedAt.IsZero() || meta.LastModified == "" {
		t.Fatalf("边车: %s %v", encoded, err)
	}
	fail.Store(true)
	get()
	if logs.Len() != 1 {
		t.Fatal("未告警")
	}
	if _, err := c.get(ctx, upstream.URL, "missing.json"); err == nil {
		t.Fatal("无缓存失败被忽略")
	}
	for _, path := range []string{"../outside", "/absolute", "a/../b", "a//b", `a\b`, "a/%2e%2e/b", "a?q=x", "a/#x"} {
		if _, err := c.get(ctx, upstream.URL, path); err == nil {
			t.Fatalf("未拒绝 %q", path)
		}
	}
}

func TestCacheSingleflight(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer upstream.Close()
	c := &Cache{Dir: t.TempDir(), TTL: time.Hour, Client: NewClient("test")}
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			if _, err := c.get(context.Background(), upstream.URL, "profiles.json"); err != nil {
				t.Error(err)
			}
		})
	}
	<-started
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("请求数 %d", calls.Load())
	}
}

func TestCacheInvalidJSONAndUncached304(t *testing.T) {
	var state atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch state.Load() {
		case 0:
			_, _ = fmt.Fprint(w, `{"ok":true}`)
		case 1:
			_, _ = fmt.Fprint(w, `broken`)
		case 2:
			w.WriteHeader(304)
		}
	}))
	defer upstream.Close()
	c := &Cache{Dir: t.TempDir(), Client: NewClient("test")}
	ctx := context.Background()
	if _, err := c.get(ctx, upstream.URL, "index.json"); err != nil {
		t.Fatal(err)
	}
	state.Store(1)
	if body, err := c.get(ctx, upstream.URL, "index.json"); err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("无效 JSON 覆盖缓存: %s %v", body, err)
	}
	state.Store(2)
	if _, err := c.get(ctx, upstream.URL, "uncached.json"); err == nil {
		t.Fatal("接受无缓存 304")
	}
	// 不同 upstream 不共享同一路径缓存。
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"other":true}`) }))
	defer other.Close()
	if body, err := c.get(ctx, other.URL, "index.json"); err != nil || string(body) != `{"other":true}` {
		t.Fatalf("跨 upstream 缓存污染: %s %v", body, err)
	}
}
