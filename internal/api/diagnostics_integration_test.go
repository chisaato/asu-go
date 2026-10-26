//go:build integration

package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gzzchh/asu-go/internal/runner"
	"github.com/gzzchh/asu-go/internal/runtime"
)

// 限定扫描范围，不回收共享 Podman socket 上其他测试或用户的环境。
type replayTestRuntime struct {
	runtime.Runtime
	id string
}

func (r replayTestRuntime) List(ctx context.Context) ([]runtime.State, error) {
	states, err := r.Runtime.List(ctx)
	var selected []runtime.State
	for _, state := range states {
		if state.Labels["asu.request_hash"] == r.id {
			selected = append(selected, state)
		}
	}
	return selected, err
}

func TestPodmanReplayRetainedAndScavenged(t *testing.T) {
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
	t.Cleanup(func() { _ = rt.Close() })
	h := diagnosticsHarness(t)
	h.q.Runner.Runtime = rt
	h.q.Runner.BuildScript = func(runner.Job) (string, error) {
		return "echo '##asu:stage=building_image'\necho diagnostic-failure\nexit 2\n", nil
	}
	w, response := h.call("POST", "/admin/api/replay", buildBody)
	if w.Code != 202 {
		t.Fatal(w.Code, response)
	}
	id := response["id"].(string)
	h.start()
	h.adminPoll(id, "failed")
	var raw string
	if err := h.db.QueryRow(`SELECT handle FROM jobs WHERE request_hash=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var handle runtime.Handle
	if err := json.Unmarshal([]byte(raw), &handle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Remove(context.Background(), handle) })
	state, err := rt.Inspect(context.Background(), handle)
	if err != nil || !state.Exists || state.Running || state.Labels["asu.keep_container"] != "true" {
		t.Fatal(state, err)
	}
	s := &runner.Scavenger{Runtime: replayTestRuntime{Runtime: rt, id: id}, Jobs: h.q, KeepContainerTTL: time.Hour, MaxKept: 100}
	if err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = rt.Inspect(context.Background(), handle)
	if err != nil || !state.Exists {
		t.Fatal(state, err)
	}
	s.KeepContainerTTL = time.Nanosecond
	if err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = rt.Inspect(context.Background(), handle)
	if err != nil || state.Exists {
		t.Fatal(state, err)
	}
}
