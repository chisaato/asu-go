package stream

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRingAndSubscription(t *testing.T) {
	h, err := NewHub(t.TempDir(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	if err = h.Begin("job"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"one\n", "two\n", "three\n"} {
		if err = h.Append("job", s); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, ch, cancel, ok := h.Subscribe("job")
	defer cancel()
	if !ok || !reflect.DeepEqual(snapshot, []string{"two\n", "three\n"}) {
		t.Fatal(snapshot)
	}
	for _, s := range []string{"four\n", "five\n", "six\n"} {
		if err = h.Append("job", s); err != nil {
			t.Fatal(err)
		}
	}
	if got := <-ch; got != "five\n" {
		t.Fatal("慢订阅者应丢弃最旧消息", got)
	}
	if err = h.Finish("job"); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got != "six\n" {
		t.Fatal(got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("未关闭")
	}
	h.Forget("job")
	source := NewMemoryJobSource()
	service := Service{Hub: h, Jobs: source}
	log, err := service.Open(context.Background(), "job", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	var buf bytes.Buffer
	if err = log.Copy(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "one\ntwo\nthree\nfour\nfive\nsix\n" {
		t.Fatal(buf.String())
	}
}
func TestLogPathSafetyAndCancel(t *testing.T) {
	dir := t.TempDir()
	h, err := NewHub(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	for _, id := range []string{"../escape", "/tmp/a", "a/b", "a.b", ""} {
		if !errors.Is(h.Begin(id), ErrInvalidID) {
			t.Fatal(id)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err = os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, "logs", "link.log")); err != nil {
		t.Fatal(err)
	}
	if err = h.Begin("link"); err == nil {
		t.Fatal("接受了日志符号链接")
	}
	if err = h.Begin("live"); err != nil {
		t.Fatal(err)
	}
	source := NewMemoryJobSource()
	source.Set("live", false)
	log, err := (Service{Hub: h, Jobs: source}).Open(context.Background(), "live", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = log.Copy(ctx, &bytes.Buffer{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
