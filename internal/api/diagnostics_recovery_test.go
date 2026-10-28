package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/runtime"
)

func TestReplaySuccessfulArtifactsPrivate(t *testing.T) {
	h := diagnosticsHarness(t)
	w, response := h.call("POST", "/admin/api/replay", buildBody)
	if w.Code != 202 {
		t.Fatal(w.Code, response)
	}
	id := response["id"].(string)
	h.start()
	h.adminPoll(id, "done")
	if _, err := os.Stat(filepath.Join(h.dir, "diagnostics", id, "firmware.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "store", id)); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	w, _ = h.call("HEAD", "/api/v1/build/"+id, "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	var counters int
	if err := h.db.QueryRow(`SELECT count(*) FROM stat_counters`).Scan(&counters); err != nil || counters != 0 {
		t.Fatal(counters, err)
	}
}

func TestCancelTakeoverKeptReplay(t *testing.T) {
	h := diagnosticsHarness(t)
	_, response := h.call("POST", "/admin/api/replay", buildBody)
	id := response["id"].(string)
	h.rt.Next = runtime.FakePlan{Delay: time.Minute, Output: "##asu:stage=building_image\n"}
	ctx := context.Background()
	handle, err := h.rt.Fake.Create(ctx, runtime.RunSpec{RequestHash: id, KeepContainer: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Start(ctx, handle); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(handle)
	if _, err := h.db.Exec(`UPDATE jobs SET status='running',handle=?,started_at=? WHERE request_hash=?`, string(raw), time.Now().UTC(), id); err != nil {
		t.Fatal(err)
	}
	// 取消发生在 Takeover 注册上下文之前，持久标记仍必须生效。
	w, _ := h.call("POST", "/admin/api/jobs/"+id+"/cancel", "")
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	h.start()
	detail := h.adminPoll(id, "failed")
	if detail["detail"] != "cancelled" {
		t.Fatal(detail)
	}
	state, err := h.rt.Inspect(ctx, handle)
	if err != nil || state.Exists {
		t.Fatal(state, err)
	}
}

func TestReplayRejectsInvalidOptionsAndModel(t *testing.T) {
	h := diagnosticsHarness(t)
	for _, body := range []string{
		`{"version":"24.10.0","target":"x86/64","profile":"generic","options":{"pull_policy":"invalid"}}`,
		`{"version":"24.10.0","target":"x86/64","profile":"generic","options":{"image_tag":"bad/tag"}}`,
		`{"version":"24.10.0","target":"x86/64","profile":"generic","options":{"verbose":"true"}}`,
		`{"version":"24.10.0","target":"invalid","profile":"generic","options":{"skip_limits":true}}`,
		`{"version":"24.10.0","target":"x86/64","profile":"generic","repositories":{"bad":"file:///etc"},"options":{"skip_limits":true}}`,
		`{} {}`,
	} {
		w, response := h.call("POST", "/admin/api/replay", body)
		if w.Code != 400 && w.Code != 422 {
			t.Fatal(w.Code, response)
		}
	}
}
