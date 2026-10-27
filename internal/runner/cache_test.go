package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gzzchh/asu-go/internal/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCacheKeyAndNames(t *testing.T) {
	for input, want := range map[string]string{"ghcr.io/openwrt/imagebuilder:target-v1@sha256:abc": "ghcr.io_openwrt_imagebuilder_target-v1", "../../X:tag": ".._..___tag", "..": "_", "": "_"} {
		if got := cacheKey(input); got != want {
			t.Fatalf("%q => %q, want %q", input, got, want)
		}
	}
	for _, name := range []string{"APKINDEX.a.tar.gz", "Packages", "../x.apk", "x/ok.apk", "x\\ok.ipk", "a..apk", "x.apk\x00"} {
		if packageName(name) {
			t.Fatal("接受不安全包名", name)
		}
	}
}

func TestScriptTiming(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "make"), []byte("#!/bin/sh\ncase $1 in manifest) echo 'base 1.0';; esac\n"), 0755); err != nil {
		t.Fatal(err)
	}
	s, err := Script(Job{ID: "job", Image: "test:image", Profile: "profile", WorkingDir: dir, Packages: []string{"base"}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", s)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(out), err)
	}
	matches := regexp.MustCompile(`(?m)^##asu:timing=([a-z_]+) ([0-9]+)$`).FindAllStringSubmatch(string(out), -1)
	var names []string
	for _, m := range matches {
		names = append(names, m[1])
	}
	if !reflect.DeepEqual(names, []string{"init", "container_setup", "validate_revision", "validate_manifest", "building_image"}) {
		t.Fatal(string(out), names)
	}
}

func TestCacheCollectFiltersAndExisting(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		kind       byte
		accepted   bool
	}{
		{"dl/a.apk", "valid", tar.TypeReg, true}, {"dl/a.ipk", "valid", tar.TypeReg, true},
		{"dl/APKINDEX.hash.tar.gz", "index", tar.TypeReg, false}, {"dl/Packages", "index", tar.TypeReg, false},
		{"dl/a.apk", "", tar.TypeReg, false}, {"dl/../evil.apk", "evil", tar.TypeReg, false},
		{"/evil.apk", "evil", tar.TypeReg, false}, {"dl/sub/a.apk", "evil", tar.TypeReg, false},
		{"dl/a.apk", "", tar.TypeSymlink, false},
	} {
		t.Run(tc.name+tc.data+string(tc.kind), func(t *testing.T) {
			c := &PackageCache{Enabled: true, Dir: t.TempDir()}
			f := runtime.NewFake()
			f.Next = runtime.FakePlan{Archives: map[string][]byte{"/builder/dl": tarData(t, tc.name, tc.data, tc.kind)}}
			h, err := f.Create(context.Background(), runtime.RunSpec{})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.collect(context.Background(), f, h, Job{Image: "test"}); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(c.Dir, "test"))
			if err != nil {
				t.Fatal(err)
			}
			if (len(entries) == 1) != tc.accepted {
				t.Fatal(entries)
			}
			if tc.accepted {
				dest := filepath.Join(c.Dir, "test", entries[0].Name())
				if err := os.WriteFile(dest, []byte("existing"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := c.collect(context.Background(), f, h, Job{Image: "test"}); err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(dest)
				if string(b) != "existing" {
					t.Fatal("覆盖已有包")
				}
			}
		})
	}
}

func TestCacheSeedOwnerFilteringAndLRU(t *testing.T) {
	c := &PackageCache{Enabled: true, Dir: t.TempDir(), MaxBytes: 3}
	dir := filepath.Join(c.Dir, "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for _, name := range []string{"a.apk", "b.ipk", "APKINDEX.x.tar.gz", "empty.apk"} {
		data := "pkg"
		if name == "empty.apk" {
			data = ""
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("a.apk", filepath.Join(dir, "link.apk")); err != nil {
		t.Fatal(err)
	}
	f := &observedRuntime{Runtime: runtime.NewFake()}
	h, err := f.Create(context.Background(), runtime.RunSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.seed(context.Background(), f, h, Job{Image: "test", WorkingDir: "/home/build", inputUID: 123, inputGID: 456}); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(f.input))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
		if hdr.Uid != 123 || hdr.Gid != 456 {
			t.Fatal(hdr)
		}
	}
	if !reflect.DeepEqual(names, []string{"dl/", "dl/a.apk", "dl/b.ipk"}) || f.putPath != "/home/build" {
		t.Fatal(names, f.putPath)
	}
	info, _ := os.Stat(filepath.Join(dir, "a.apk"))
	if !info.ModTime().After(old) {
		t.Fatal("未 touch")
	}
	if err := os.Chtimes(filepath.Join(dir, "a.apk"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.apk")); !os.IsNotExist(err) {
		t.Fatal("未淘汰最旧包")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.ipk")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "APKINDEX.x.tar.gz")); err != nil {
		t.Fatal("不应碰非包文件", err)
	}
}

func TestPackagePublishAtomicFailure(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "a.apk")
	if err := publishPackage(dir, dest, strings.NewReader("partial"), 100); err == nil {
		t.Fatal("截断未报错")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("泄漏半成品", entries)
	}
	if err := publishPackage(dir, dest, strings.NewReader("full"), 4); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "full" {
		t.Fatal(string(b))
	}
}

func TestCacheDisabledAndCustomRepoReasons(t *testing.T) {
	for _, reason := range []string{"disabled", "custom_repos"} {
		core, logs := observer.New(zap.DebugLevel)
		c := &PackageCache{Enabled: reason != "disabled", Logger: zap.New(core)}
		j := Job{ID: "job", Image: "image"}
		if reason == "custom_repos" {
			j.Repositories = map[string]string{"custom": "https://example.invalid"}
		}
		if c.usable(j) {
			t.Fatal("不应使用缓存")
		}
		entries := logs.All()
		if len(entries) != 1 || entries[0].ContextMap()["reason"] != reason || entries[0].ContextMap()["component"] != "runner" {
			t.Fatal(entries)
		}
	}
}

func TestCacheZeroCapacity(t *testing.T) {
	c := &PackageCache{Enabled: true, Dir: t.TempDir(), MaxBytes: 0}
	dir := filepath.Join(c.Dir, "image")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.ipk"), []byte("pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestRunCachePolicyAndFailure(t *testing.T) {
	for _, mode := range []string{"success", "disabled", "custom_repos", "failed", "collect_failure", "seed_failure"} {
		t.Run(mode, func(t *testing.T) {
			p := runtime.FakePlan{Output: "##asu:stage=init\n##asu:timing=init 2\n##asu:unknown=x\n##asu:stage=building_image\n", Archives: map[string][]byte{"/builder/job": tarData(t, "job/image.bin", "image", tar.TypeReg), "/builder/dl": tarData(t, "dl/a.apk", "pkg", tar.TypeReg)}}
			if mode == "failed" {
				p.ExitCode = 2
			}
			if mode == "collect_failure" {
				delete(p.Archives, "/builder/dl")
			}
			r, rt, j := setupRunner(t, p)
			r.Cache = &PackageCache{Enabled: mode != "disabled", Dir: t.TempDir(), MaxBytes: 20 << 30}
			if mode == "seed_failure" {
				r.Cache.Dir = filepath.Join(r.Cache.Dir, "not-a-dir")
				if err := os.WriteFile(r.Cache.Dir, []byte("bad"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "custom_repos" {
				j.Repositories = map[string]string{"x": "https://example.invalid"}
			}
			r.Verbose = true
			result, err := r.Run(context.Background(), j)
			if (err != nil) != (mode == "failed") {
				t.Fatal(err)
			}
			if len(result.Stages) != 2 {
				t.Fatal("timing 被解析为阶段", result.Stages)
			}
			if !strings.Contains(rt.spec.Command[2], " V=s\n") {
				t.Fatal("默认未详细输出")
			}
			getCount, putCount := 0, 0
			for _, call := range rt.calls {
				if call == "get" {
					getCount++
				}
				if call == "put" {
					putCount++
				}
			}
			wantPut, wantGet := 2, 2
			if mode == "disabled" || mode == "custom_repos" {
				wantPut, wantGet = 1, 1
			}
			if mode == "failed" {
				wantGet = 0
			}
			if mode == "seed_failure" {
				wantPut = 1
			}
			if putCount != wantPut || getCount != wantGet {
				t.Fatal(rt.calls)
			}
		})
	}
}
