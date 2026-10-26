//go:build integration

package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stream"
	"golang.org/x/sync/errgroup"
)

// 模拟 ImageBuilder 的 make，脚本作为容器主进程的子进程运行，不使用 exec API。
const fakeMake = `#!/bin/sh
set -eu
case "$1" in
info)
  if [ "${ASU_TEST_EXPECT_INPUTS:-}" = 1 ]; then
    test -f asu-files/etc/uci-defaults/99-asu-defaults
    test -f keys/custom-0.pem
    grep -F 'src/gz custom https://example.com/repo' repositories.conf
  fi
  echo 'Current Revision: "r-test"'
  sleep 1
  ;;
manifest)
  echo 'base - 1.0'
  ;;
image)
  echo "args: $*"
  shift
  for arg in "$@"; do
    case "$arg" in BIN_DIR=*) bin=${arg#BIN_DIR=};; esac
  done
  echo 'image started'
  sleep "${ASU_TEST_SLEEP:-1}"
  mkdir -p "$bin"
  echo artifact > "$bin/firmware.bin"
  echo 'image finished'
  ;;
esac
`

func TestPodmanRunner(t *testing.T) {
	socket := os.Getenv("ASU_TEST_RUNTIME_SOCKET")
	if socket == "" {
		socket = "unix:///var/run/docker.sock"
	}
	driver := os.Getenv("ASU_TEST_RUNTIME_DRIVER")
	if driver == "" {
		driver = "podman"
	}
	rt, err := runtime.Open(driver, runtime.Options{Socket: socket})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close() }()
	for _, name := range []string{"success", "cancel", "timeout", "manifest"} {
		t.Run(name, func(t *testing.T) {
			h, err := stream.NewHub(t.TempDir(), 100)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = h.Close() }()
			observed := &observedRuntime{Runtime: rt, inject: tarData(t, "make", fakeMake, 0)}
			r := Runner{Runtime: observed, Hub: h, JobTimeout: 20 * time.Second}
			j := Job{ID: "runner-integration-" + name, Image: "docker.io/library/alpine:latest", Profile: "test", VersionCode: "r-test", PackageVersions: map[string]string{"base": "1.0"}, ArtifactDir: t.TempDir(), Env: []string{"PATH=/builder:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}}
			if name == "success" {
				j.Defaults = "#!/bin/sh\necho test\n"
				j.Repositories = map[string]string{"custom": "https://example.com/repo"}
				j.RepositoryKeys = []string{"-----BEGIN PUBLIC KEY-----\ntest"}
				j.RootFSSizeMB = 123
				j.Verbose = true
				j.Env = append(j.Env, "ASU_TEST_EXPECT_INPUTS=1")
			}
			if name == "timeout" {
				r.JobTimeout = 1500 * time.Millisecond
				j.Env = append(j.Env, "ASU_TEST_SLEEP=30")
			}
			if name == "cancel" {
				j.Env = append(j.Env, "ASU_TEST_SLEEP=30")
			}
			if name == "manifest" {
				j.PackageVersions["base"] = "2.0"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			stages := make(chan Stage, 10)
			j.OnStage = func(_ context.Context, s Stage) error {
				stages <- s
				if name == "cancel" && s.Name == "building_image" {
					cancel()
				}
				return nil
			}
			group, _ := errgroup.WithContext(ctx)
			var result Result
			var runErr error
			started := time.Now()
			group.Go(func() error { result, runErr = r.Run(ctx, j); return nil })
			select {
			case first := <-stages:
				if first.Name != "init" || time.Since(started) > 10*time.Second {
					t.Error("阶段未实时到达", first)
				}
				if tail := strings.Join(h.Tail(j.ID), ""); !strings.Contains(tail, "##asu:stage=init") || strings.Contains(tail, "image finished") {
					t.Error("日志不是实时输出", tail)
				}
			case <-ctx.Done():
				t.Error("未收到实时阶段", ctx.Err())
			}
			if err = group.Wait(); err != nil {
				t.Fatal(err)
			}
			if name == "success" {
				if runErr != nil {
					t.Fatal(runErr)
				}
				var names []string
				for _, s := range result.Stages {
					names = append(names, s.Name)
				}
				if !reflect.DeepEqual(names, []string{"init", "container_setup", "validate_revision", "validate_manifest", "building_image"}) {
					t.Fatal(names)
				}
				tail := strings.Join(h.Tail(j.ID), "")
				for _, want := range []string{"FILES=/builder/asu-files", "ROOTFS_PARTSIZE=123", "V=s"} {
					if !strings.Contains(tail, want) {
						t.Fatal("缺少构建参数", want, tail)
					}
				}
				data, e := os.ReadFile(filepath.Join(j.ArtifactDir, "firmware.bin"))
				if e != nil || string(data) != "artifact\n" {
					t.Fatal(string(data), e)
				}
			} else {
				var e *Error
				if !errors.As(runErr, &e) {
					t.Fatal(runErr)
				}
				want := name
				if name == "cancel" {
					want = "cancelled"
				}
				if name == "manifest" {
					want = "failed"
				}
				if e.Kind != want {
					t.Fatal(e)
				}
				if !strings.Contains(strings.Join(observed.calls, ","), "kill,remove") {
					t.Fatal(observed.calls)
				}
			}
		})
	}
}
