package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalSafety(t *testing.T) {
	ctx := context.Background()
	s := &Local{Dir: t.TempDir()}
	src := t.TempDir()
	write(t, filepath.Join(src, "sub", "image.bin"), "firmware")
	write(t, filepath.Join(src, ".secret"), "secret")
	if err := s.Publish(ctx, testHash, src); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret"), "outside")
	if err := os.Symlink(outside, filepath.Join(s.Dir, testHash, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".secret", filepath.Join(s.Dir, testHash, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.Dir, strings.Repeat("b", 64))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range [][2]string{
		{testHash, "../secret"}, {testHash, "/etc/passwd"}, {testHash, "sub/../../secret"}, {testHash, "sub\\image.bin"},
		{testHash, ".secret"}, {testHash, "alias"}, {testHash, "sub/.hidden"}, {testHash, "escape/secret"}, {testHash, "sub"},
		{"../other", "secret"}, {strings.Repeat("A", 64), "secret"}, {strings.Repeat("b", 64), "secret"}, {"rp_invalid", "secret"},
	} {
		if f, err := s.Open(ctx, tc[0], tc[1]); err == nil {
			_ = f.Close()
			t.Errorf("允许危险路径 %v", tc)
		}
	}
	f, err := s.Open(ctx, testHash, "sub/image.bin")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	_ = f.Close()
	if string(data) != "firmware" {
		t.Fatal(string(data))
	}
	if info, err := s.Stat(ctx, testHash, "sub/image.bin"); err != nil || info.Size() != 8 {
		t.Fatal(info, err)
	}
	if size, err := s.Usage(ctx); err != nil || size != 8 {
		t.Fatal(size, err)
	}
	for range 2 {
		if err := s.Delete(ctx, testHash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "secret")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHash("rp_01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
}

func TestPublishReplaceRecoveryAndConcurrency(t *testing.T) {
	ctx := context.Background()
	s := &Local{Dir: t.TempDir()}
	src := t.TempDir()
	write(t, filepath.Join(src, "file"), "old")
	if err := s.Publish(ctx, testHash, src); err != nil {
		t.Fatal(err)
	}
	// 源位于旧目录内：备份切换后源路径消失，强制触发 rename 失败及恢复。
	nested := filepath.Join(s.Dir, testHash, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, testHash, nested); err == nil {
		t.Fatal("应当发布失败")
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, testHash, "file"))
	if err != nil || string(data) != "old" {
		t.Fatal(string(data), err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			f, err := s.Open(ctx, testHash, "file")
			if err != nil {
				t.Error("发布过程中读取失败", err)
				return
			}
			data, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil || len(data) == 0 {
				t.Error("读到部分产物", string(data), err)
				return
			}
		}
	}()
	for i := range 12 {
		src := t.TempDir()
		write(t, filepath.Join(src, "file"), fmt.Sprint(i))
		write(t, filepath.Join(src, "other"), fmt.Sprint(i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Publish(ctx, testHash, src); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-readerDone
	a, err := os.ReadFile(filepath.Join(s.Dir, testHash, "file"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, testHash, "other"))
	if err != nil || string(a) != string(b) {
		t.Fatal(string(a), string(b), err)
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
}
