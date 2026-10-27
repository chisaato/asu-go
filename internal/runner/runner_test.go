package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stream"
)

type observedRuntime struct {
	runtime.Runtime
	calls            []string
	spec             runtime.RunSpec
	input            []byte
	inject           []byte
	putPath, getPath string
}

func (r *observedRuntime) Create(ctx context.Context, s runtime.RunSpec) (runtime.Handle, error) {
	r.calls = append(r.calls, "create")
	r.spec = s
	return r.Runtime.Create(ctx, s)
}
func (r *observedRuntime) PutArchive(ctx context.Context, h runtime.Handle, p string, reader io.Reader) error {
	r.calls = append(r.calls, "put")
	r.putPath = p
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	r.input = data
	if err = r.Runtime.PutArchive(ctx, h, p, bytes.NewReader(data)); err != nil {
		return err
	}
	if r.inject != nil {
		return r.Runtime.PutArchive(ctx, h, p, bytes.NewReader(r.inject))
	}
	return nil
}
func (r *observedRuntime) Start(ctx context.Context, h runtime.Handle) error {
	r.calls = append(r.calls, "start")
	return r.Runtime.Start(ctx, h)
}
func (r *observedRuntime) Logs(ctx context.Context, h runtime.Handle, o runtime.LogsOptions) (io.ReadCloser, error) {
	r.calls = append(r.calls, "logs")
	if !o.Follow {
		return nil, errors.New("必须 follow")
	}
	return r.Runtime.Logs(ctx, h, o)
}
func (r *observedRuntime) Wait(ctx context.Context, h runtime.Handle) (runtime.ExitStatus, error) {
	r.calls = append(r.calls, "wait")
	return r.Runtime.Wait(ctx, h)
}
func (r *observedRuntime) GetArchive(ctx context.Context, h runtime.Handle, p string) (io.ReadCloser, error) {
	r.calls = append(r.calls, "get")
	r.getPath = p
	return r.Runtime.GetArchive(ctx, h, p)
}
func (r *observedRuntime) Kill(ctx context.Context, h runtime.Handle) error {
	r.calls = append(r.calls, "kill")
	return r.Runtime.Kill(ctx, h)
}
func (r *observedRuntime) Remove(ctx context.Context, h runtime.Handle) error {
	r.calls = append(r.calls, "remove")
	return r.Runtime.Remove(ctx, h)
}

func tarData(t *testing.T, name, data string, kind byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	h := &tar.Header{Name: name, Mode: 0777, Typeflag: kind, Size: int64(len(data))}
	if kind == tar.TypeSymlink {
		h.Size = 0
		h.Linkname = "/etc/passwd"
		data = ""
	}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func setupRunner(t *testing.T, p runtime.FakePlan) (*Runner, *observedRuntime, Job) {
	t.Helper()
	f := runtime.NewFake()
	f.Next = p
	observed := &observedRuntime{Runtime: f}
	h, err := stream.NewHub(t.TempDir(), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return &Runner{Runtime: observed, Hub: h, JobTimeout: time.Second}, observed, Job{ID: "job", Image: "fake", Profile: "generic", ArtifactDir: t.TempDir()}
}
func TestRunLifecycle(t *testing.T) {
	names := []string{"init", "container_setup", "validate_revision", "validate_manifest", "building_image"}
	var output strings.Builder
	for _, name := range names {
		output.WriteString("##asu:stage=" + name + "\n")
	}
	r, rt, j := setupRunner(t, runtime.FakePlan{Output: output.String(), Archives: map[string][]byte{"/builder/job": tarData(t, "job/firmware.bin", "image", tar.TypeReg)}})
	j.Defaults = "#!/bin/sh\necho safe\n"
	j.Verbose = true
	j.KeepContainer = true
	var stages []string
	j.OnStage = func(_ context.Context, s Stage) error { stages = append(stages, s.Name); return nil }
	var handle runtime.Handle
	j.OnHandle = func(_ context.Context, h runtime.Handle) error { handle = h; return nil }
	result, err := r.Run(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stages, names) || len(result.Stages) != 5 || handle != result.Handle {
		t.Fatal(result, stages)
	}
	if !reflect.DeepEqual(rt.calls, []string{"create", "put", "start", "logs", "wait", "get", "remove"}) {
		t.Fatal(rt.calls)
	}
	data, err := os.ReadFile(filepath.Join(j.ArtifactDir, "firmware.bin"))
	if err != nil || string(data) != "image" {
		t.Fatal(string(data), err)
	}
	fi, err := os.Stat(filepath.Join(j.ArtifactDir, "firmware.bin"))
	if err != nil || fi.Mode().Perm() != 0644 {
		t.Fatal(fi, err)
	}
	if !rt.spec.KeepContainer || !strings.Contains(rt.spec.Command[2], " V=s") || !strings.Contains(rt.spec.Command[2], "FILES=/builder/asu-files") {
		t.Fatal(rt.spec)
	}
	tr := tar.NewReader(bytes.NewReader(rt.input))
	found := false
	for {
		hdr, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if hdr.Name == "asu-files/etc/uci-defaults/99-asu-defaults" {
			found = true
		}
	}
	if !found {
		t.Fatal("未注入 defaults")
	}
	if got := strings.Join(r.Hub.Tail(j.ID), ""); got != output.String() {
		t.Fatal(got)
	}
}
func TestRunFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		delay   time.Duration
		exit    int64
		keep    bool
		timeout time.Duration
		cancel  bool
		kind    string
	}{
		{name: "exit", exit: 7, kind: "failed"}, {name: "kept", exit: 3, keep: true, kind: "failed"},
		{name: "timeout", delay: time.Second, timeout: 10 * time.Millisecond, kind: "timeout"},
		{name: "cancel", delay: time.Second, cancel: true, kind: "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, rt, j := setupRunner(t, runtime.FakePlan{Output: "error: rootfs is too big\n", ExitCode: tc.exit, Delay: tc.delay})
			j.KeepContainer = tc.keep
			if tc.timeout > 0 {
				r.JobTimeout = tc.timeout
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				j.OnHandle = func(context.Context, runtime.Handle) error { cancel(); return nil }
			}
			_, err := r.Run(ctx, j)
			var runErr *Error
			if !errors.As(err, &runErr) || runErr.Kind != tc.kind {
				t.Fatalf("%+v", err)
			}
			if !tc.cancel && !strings.Contains(err.Error(), "Selected packages exceed device storage") {
				t.Fatal(err)
			}
			calls := strings.Join(rt.calls, ",")
			if !strings.Contains(calls, "kill") || (strings.Contains(calls, "remove") == tc.keep) {
				t.Fatal(calls)
			}
			if tc.exit != 0 && runErr.ExitCode != tc.exit {
				t.Fatal(runErr)
			}
			if tc.kind == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
}
func TestArtifactTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "job/../../escape", "/absolute", "job/link", "job/dir\\escape"} {
		t.Run(name, func(t *testing.T) {
			kind := byte(tar.TypeReg)
			if name == "job/link" {
				kind = tar.TypeSymlink
			}
			r, rt, j := setupRunner(t, runtime.FakePlan{Archives: map[string][]byte{"/builder/job": tarData(t, name, "bad", kind)}})
			if _, err := r.Run(context.Background(), j); err == nil {
				t.Fatal("未拒绝不安全 tar")
			}
			if rt.calls[len(rt.calls)-1] != "remove" {
				t.Fatal(rt.calls)
			}
		})
	}
}
func TestScriptQuotingAndInputs(t *testing.T) {
	j := Job{ID: "job", Profile: "a'; echo injected; #", Packages: []string{"a", "b'$(touch /tmp/injected)"}, VersionCode: "r'1", RootFSSizeMB: 123, Filesystem: "squashfs", Verbose: true, PackageVersions: map[string]string{"foo": "1.0"}, Defaults: "#!/bin/sh", Repositories: map[string]string{"custom": "https://example.com/repo"}, RepositoryKeys: []string{"-----BEGIN PUBLIC KEY-----\nkey"}}
	script, err := Script(j)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	for _, want := range []string{"make info", "make manifest", "make image", "STRIP_ABI=1", "ROOTFS_PARTSIZE=123", "V=s", "Impossible package selection"} {
		if !strings.Contains(script, want) {
			t.Fatal(want)
		}
	}
	data, err := inputs(j)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(data))
	names := map[string]bool{}
	for {
		hdr, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		names[hdr.Name] = true
	}
	for _, name := range []string{".asu-repositories-apk", ".asu-repositories-opkg", "keys/custom-0.pem"} {
		if !names[name] {
			t.Fatal(name)
		}
	}
	j.RepositoryKeys = []string{"bad"}
	if _, err = inputs(j); err == nil {
		t.Fatal("接受无效 key")
	}
}
