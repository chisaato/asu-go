//go:build integration

package runner

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/runtime"
	"github.com/chisaato/asu-go/internal/stream"
	"golang.org/x/sync/errgroup"
)

// 真实 ImageBuilder 只跑 setup 与 make info，不下载包、不生成固件。
func TestPodmanImageBuilderSetup(t *testing.T) {
	socket := os.Getenv("ASU_TEST_RUNTIME_SOCKET")
	if socket == "" {
		socket = "unix:///var/run/docker.sock"
	}
	rt, err := runtime.Open("podman", runtime.Options{Socket: socket, PullPolicy: "never"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close() }()
	for _, image := range []string{
		"ghcr.io/openwrt/imagebuilder:mediatek-filogic-v25.12.6",
		"docker.io/immortalwrt/imagebuilder:mediatek-filogic-openwrt-25.12.2",
	} {
		t.Run(image, func(t *testing.T) {
			hub, err := stream.NewHub(t.TempDir(), 100)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = hub.Close() }()
			r := &Runner{Runtime: rt, Hub: hub, JobTimeout: 2 * time.Minute}
			r.BuildScript = func(j Job) (string, error) {
				script, err := Script(j)
				if err != nil {
					return "", err
				}
				setup, _, ok := strings.Cut(script, "stage validate_revision\n")
				if !ok {
					t.Fatal("缺少 make info 后阶段标记")
				}
				// 验证注入目录和文件均归构建用户可写，而非仅可读。
				setup += "test -w asu-files/etc/uci-defaults\ntest -w asu-files/etc/uci-defaults/99-asu-defaults\ntest -w keys\ntest -w keys/custom-0.pem\nprintf test > asu-files/etc/uci-defaults/write-test\n"
				setup += "mkdir -p " + quote(path.Join(j.WorkingDir, j.ID)) + "\ncp .asu-info " + quote(path.Join(j.WorkingDir, j.ID, "info.txt")) + "\nprintf '%s\\n' '##asu:stage=validate_revision'\n"
				return setup, nil
			}
			j := Job{ID: "imagebuilder-setup", Image: image, ArtifactDir: t.TempDir(), PullPolicy: "never", Defaults: "#!/bin/sh\necho test\n", Repositories: map[string]string{"custom": "https://example.invalid/repo"}, RepositoriesMode: "replace", RepositoryKeys: []string{"-----BEGIN PUBLIC KEY-----\ntest"}}
			result, err := r.Run(context.Background(), j)
			if err != nil {
				t.Fatal(err)
			}
			if result.ExitCode != 0 || len(result.Stages) != 3 || result.Stages[2].Name != "validate_revision" {
				t.Fatal(result)
			}
			info, err := os.ReadFile(filepath.Join(j.ArtifactDir, "info.txt"))
			if err != nil || !strings.Contains(string(info), "Current Revision:") {
				t.Fatal("未取回真实 make info 输出", string(info), err)
			}
			t.Log("setup、FIFO、仓库写入、defaults/keys 写权限、make info 与 get_archive 均通过")
		})
	}
}

func TestPodmanTakeoverAndScavenge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
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
	dataDir := t.TempDir()
	hub, err := stream.NewHub(dataDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	first := &Runner{Runtime: rt, Hub: hub}
	j := Job{ID: "takeover-integration", Image: "docker.io/library/alpine:latest", ArtifactDir: t.TempDir()}
	if err := first.Runtime.Prepare(ctx, runtime.ImageSpec{Reference: j.Image}); err != nil {
		t.Fatal(err)
	}
	h, err := first.Runtime.Create(ctx, runtime.RunSpec{Image: j.Image, WorkingDir: "/tmp/asu-takeover", RequestHash: j.ID, Command: []string{"sh", "-ec", "echo first; sleep 2; echo second; sleep 1; mkdir -p takeover-integration; echo artifact > takeover-integration/firmware.bin; echo last"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Remove(context.Background(), h) }()
	if err := first.Runtime.Start(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := first.Hub.Begin(j.ID); err != nil {
		t.Fatal(err)
	}
	reader, err := first.Runtime.Logs(ctx, h, runtime.LogsOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() || scanner.Text() != "first" {
		t.Fatal("缺少重启前首行", scanner.Text(), scanner.Err())
	}
	if err := first.Hub.Append(j.ID, scanner.Text()+"\n"); err != nil {
		t.Fatal(err)
	}
	// 模拟崩溃：仅关闭宿主日志连接，不触发 Run 的正常取消清理。
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Hub.Close(); err != nil {
		t.Fatal(err)
	}
	state, err := rt.Inspect(ctx, h)
	if err != nil || !state.Running {
		t.Fatal("接管前容器未运行", state, err)
	}
	hub, err = stream.NewHub(dataDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hub.Close() }()
	restarted := &Runner{Runtime: rt, Hub: hub}
	result, err := restarted.Resume(ctx, j, h)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || strings.Join(hub.Tail(j.ID), "") != "first\nsecond\nlast\n" {
		t.Fatal(result, hub.Tail(j.ID))
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "logs", j.ID+".log"))
	if err != nil || string(data) != "first\nsecond\nlast\n" {
		t.Fatal(string(data), err)
	}
	data, err = os.ReadFile(filepath.Join(j.ArtifactDir, "firmware.bin"))
	if err != nil || string(data) != "artifact\n" {
		t.Fatal(string(data), err)
	}
	state, err = rt.Inspect(ctx, h)
	if err != nil || state.Exists {
		t.Fatal(state, err)
	}
	// 将所有已有服务环境视为活跃，只回收本测试新产生的孤儿，避免影响使用者。
	states, err := rt.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &memoryJobs{}
	for _, state := range states {
		jobs.tasks = append(jobs.tasks, Task{ID: state.Labels["asu.request_hash"], Handle: state.Handle, Status: "running"})
	}
	orphan, err := rt.Create(ctx, runtime.RunSpec{Image: j.Image, RequestHash: "scavenger-integration", Command: []string{"sleep", "30"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Remove(context.Background(), orphan) }()
	if err := rt.Start(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	if err := rt.Kill(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	s := &Scavenger{Runtime: rt, Jobs: jobs, MaxKept: 3}
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = rt.Inspect(ctx, orphan)
	if err != nil || state.Exists {
		t.Fatal("孤儿没有被清理", state, err)
	}
}

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

func fakeMakeArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// Alpine 的 WorkingDir=/ 且根目录为 0555；测试夹具模拟可写 ImageBuilder 目录。
	for _, hdr := range []*tar.Header{
		{Name: ".", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "make", Mode: 0755, Size: int64(len(fakeMake))},
	} {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "make" {
			if _, err := tw.Write([]byte(fakeMake)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

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
			observed := &observedRuntime{Runtime: rt, inject: fakeMakeArchive(t)}
			r := Runner{Runtime: observed, Hub: h, JobTimeout: 20 * time.Second}
			image := "docker.io/library/alpine:latest"
			if err := rt.Prepare(context.Background(), runtime.ImageSpec{Reference: image}); err != nil {
				t.Fatal(err)
			}
			metadata, err := rt.InspectImage(context.Background(), image)
			if err != nil {
				t.Fatal(err)
			}
			dir := workingDir(metadata.WorkingDir)
			j := Job{ID: "runner-integration-" + name, Image: image, Profile: "test", VersionCode: "r-test", PackageVersions: map[string]string{"base": "1.0"}, ArtifactDir: t.TempDir(), Env: []string{"PATH=" + dir + ":/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}}
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
				for _, want := range []string{"FILES=" + path.Join(dir, "asu-files"), "ROOTFS_PARTSIZE=123", "V=s"} {
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
