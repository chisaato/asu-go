package runtime

import (
	"context"
	"io"
	"testing"
	"time"
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
	status, err := f.Wait(ctx, h)
	if err != nil || status.Code != 137 {
		t.Fatal(status, err)
	}
	if err = f.Remove(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err = f.Remove(ctx, h); err != nil {
		t.Fatal(err)
	}
}
