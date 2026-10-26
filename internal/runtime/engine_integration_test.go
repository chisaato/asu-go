//go:build integration

package runtime

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func TestEngineLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	socket := os.Getenv("ASU_TEST_RUNTIME_SOCKET")
	if socket == "" {
		socket = "unix:///var/run/docker.sock"
	}
	name := os.Getenv("ASU_TEST_RUNTIME_DRIVER")
	if name == "" {
		name = "docker"
	}
	r, err := Open(name, Options{Socket: socket, MemoryLimit: 64 * 1024 * 1024, CPUs: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	e := r.(*engine)
	info, err := r.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("引擎: %+v", info)
	// 测试不会删除既有 asu-build 网络，只回收本次创建的网络。
	_, networkErr := e.client.NetworkInspect(ctx, buildNetwork, client.NetworkInspectOptions{})
	if err = r.Prepare(ctx, ImageSpec{Reference: "docker.io/library/alpine:latest"}); err != nil {
		t.Fatal(err)
	}
	if networkErr != nil {
		defer func() {
			_, _ = e.client.NetworkRemove(context.Background(), buildNetwork, client.NetworkRemoveOptions{})
		}()
	}
	if _, err = e.client.NetworkInspect(ctx, buildNetwork, client.NetworkInspectOptions{}); err != nil {
		t.Fatal(err)
	}
	h, err := r.Create(ctx, RunSpec{Image: "docker.io/library/alpine:latest", RequestHash: "integration", Command: []string{"sh", "-c", "echo first; sleep 2; echo second >&2; mkdir /tmp/result; cp /tmp/input /tmp/result/output; exit 7"}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Remove(context.Background(), h)
	encoded, _ := json.Marshal(h)
	var restored Handle
	if err = json.Unmarshal(encoded, &restored); err != nil || restored != h {
		t.Fatal("句柄无法恢复", err)
	}
	i, err := e.client.ContainerInspect(ctx, string(h), client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := i.Container
	_, attached := c.NetworkSettings.Networks[buildNetwork]
	if c.Config.Labels["managed-by"] != "asu-go" || c.Config.Labels["asu.request_hash"] != "integration" || c.HostConfig.AutoRemove || c.HostConfig.Privileged || len(c.HostConfig.Binds) != 0 || !attached {
		t.Fatalf("不安全配置: %+v", c.HostConfig)
	}
	if len(c.HostConfig.CapDrop) == 0 || len(c.HostConfig.SecurityOpt) == 0 || c.HostConfig.Memory != 64*1024*1024 || c.HostConfig.NanoCPUs != 500000000 {
		t.Fatalf("限制未生效: %+v", c.HostConfig)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	data := []byte("artifact\n")
	if err = tw.WriteHeader(&tar.Header{Name: "input", Mode: 0644, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err = tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = r.PutArchive(ctx, h, "/tmp", &buf); err != nil {
		t.Fatal(err)
	}
	if err = r.Start(ctx, h); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	logs, err := r.Logs(ctx, h, LogsOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	s := bufio.NewScanner(logs)
	if !s.Scan() || s.Text() != "first" {
		t.Fatalf("首行: %q %v", s.Text(), s.Err())
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("首行被缓冲到后续输出")
	}
	if !s.Scan() || s.Text() != "second" {
		t.Fatalf("后续行: %q %v", s.Text(), s.Err())
	}
	if s.Scan() || s.Err() != nil {
		t.Fatal("日志 EOF", s.Text(), s.Err())
	}
	status, err := r.Wait(ctx, h)
	if err != nil || status.Code != 7 {
		t.Fatalf("退出: %+v %v", status, err)
	}
	archive, err := r.GetArchive(ctx, h, "/tmp/result")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	tr := tar.NewReader(archive)
	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(header.Name, "/output") {
			got, err := io.ReadAll(tr)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("产物: %q %v", got, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("未回收产物")
	}
	replay, err := r.Logs(ctx, h, LogsOptions{Since: "1970-01-01T00:00:01Z", Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := io.ReadAll(replay)
	_ = replay.Close()
	if err != nil || !strings.Contains(string(replayed), "first") || !strings.Contains(string(replayed), "T") {
		t.Fatalf("日志重放: %q %v", replayed, err)
	}
	if err = r.Remove(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err = r.Remove(ctx, h); err != nil {
		t.Fatal(err)
	}
	h, err = r.Create(ctx, RunSpec{Image: "docker.io/library/alpine:latest", Command: []string{"sleep", "60"}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Remove(context.Background(), h)
	if err = r.Start(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err = r.Kill(ctx, h); err != nil {
		t.Fatal(err)
	}
	status, err = r.Wait(ctx, h)
	if err != nil || status.Code == 0 {
		t.Fatalf("Kill: %+v %v", status, err)
	}
	if err = r.Kill(ctx, h); err != nil {
		t.Fatal(err)
	}
}
