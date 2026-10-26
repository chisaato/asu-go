package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func TestLoggingDriver(t *testing.T) {
	for _, name := range []string{"none", ""} {
		if checkLoggingDriver(name) == nil {
			t.Fatalf("接受不可读驱动 %q", name)
		}
	}
	for _, name := range []string{"json-file", "k8s-file", "journald", "local"} {
		if err := checkLoggingDriver(name); err != nil {
			t.Fatal(err)
		}
	}
}
func TestFake(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	f.Next = FakePlan{Output: "hello\n", ExitCode: 7, Delay: time.Hour}
	h, err := f.Create(ctx, RunSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Start(ctx, h); err != nil {
		t.Fatal(err)
	}
	state, err := f.Inspect(ctx, h)
	if err != nil || !state.Exists || !state.Running || state.CreatedAt.IsZero() {
		t.Fatal(state, err)
	}
	states, err := f.List(ctx)
	if err != nil || len(states) != 1 || states[0].Handle != h {
		t.Fatal(states, err)
	}
	r, err := f.Logs(ctx, h, LogsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(data) != "hello\n" {
		t.Fatal(string(data), err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = f.Wait(cancelled, h); err == nil {
		t.Fatal("应响应取消")
	}
	if err = f.Kill(ctx, h); err != nil {
		t.Fatal(err)
	}
	state, err = f.Inspect(ctx, h)
	if err != nil || state.Running || state.FinishedAt.IsZero() {
		t.Fatal(state, err)
	}
	status, err := f.Wait(ctx, h)
	if err != nil || status.Code != 137 {
		t.Fatal(status, err)
	}
	if err = f.Remove(ctx, h); err != nil {
		t.Fatal(err)
	}
	state, err = f.Inspect(ctx, h)
	if err != nil || state.Exists {
		t.Fatal(state, err)
	}
	if err = f.Remove(ctx, h); err != nil {
		t.Fatal(err)
	}
}

func TestEngineInspectAndList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			if r.URL.Query().Get("all") != "1" || !strings.Contains(r.URL.Query().Get("filters"), "managed-by=asu-go") {
				t.Errorf("缺少服务标签过滤: %s", r.URL)
			}
			_, _ = fmt.Fprint(w, `[{"Id":"ours","Labels":{"managed-by":"asu-go"}},{"Id":"foreign","Labels":{"managed-by":"other"}}]`)
		case strings.HasSuffix(r.URL.Path, "/containers/ours/json"):
			_, _ = fmt.Fprint(w, `{"Id":"ours","Created":"2026-10-27T00:00:00Z","Config":{"Labels":{"managed-by":"asu-go","asu.keep_container":"true"}},"State":{"Running":false,"FinishedAt":"2026-10-27T00:01:00Z"}}`)
		case strings.HasSuffix(r.URL.Path, "/containers/missing/json"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"message":"no such container"}`)
		default:
			t.Errorf("意外请求 %s", r.URL)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	c, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.44"))
	if err != nil {
		t.Fatal(err)
	}
	e := &engine{client: c}
	defer func() { _ = e.Close() }()
	states, err := e.List(context.Background())
	if err != nil || len(states) != 1 {
		t.Fatal(states, err)
	}
	s := states[0]
	if !s.Exists || s.Running || s.CreatedAt.IsZero() || s.FinishedAt.IsZero() || s.Labels["asu.keep_container"] != "true" {
		t.Fatal(s)
	}
	s, err = e.Inspect(context.Background(), "missing")
	if err != nil || s.Exists {
		t.Fatal(s, err)
	}
}
