package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/admin"
	"github.com/chisaato/asu-go/internal/buildreq"
	"github.com/chisaato/asu-go/internal/runner"
)

func diagnosticsHarness(t *testing.T) *buildHarness {
	h := newBuildHarness(t)
	h.q.Options.MaxPendingJobs = 10
	admin.RegisterJobs(h.router.Group("/admin/api"), h.q)
	return h
}

func (h *buildHarness) adminPoll(id, status string) map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w, body := h.call("GET", "/admin/api/jobs/"+id, "")
		if w.Code != 200 {
			h.t.Fatal(w.Code, body)
		}
		if body["status"] == status {
			return body
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("管理任务轮询超时", id, status)
	return nil
}

func TestCancelQueuedRunningFinished(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "running"}[running], func(t *testing.T) {
			h := diagnosticsHarness(t)
			h.rt.plan.Delay = time.Minute
			_, body := h.call("POST", "/api/v1/build", buildBody)
			id := body["request_hash"].(string)
			if running {
				h.start()
				h.adminPoll(id, "running")
				// 等待句柄持久化，验证取消确实清理了已创建环境。
				deadline := time.Now().Add(5 * time.Second)
				for {
					var handle *string
					if err := h.db.QueryRow(`SELECT handle FROM jobs WHERE request_hash=?`, id).Scan(&handle); err != nil {
						t.Fatal(err)
					}
					if handle != nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("没有持久化句柄")
					}
					time.Sleep(time.Millisecond)
				}
			}
			w, body := h.call("POST", "/admin/api/jobs/"+id+"/cancel", "")
			if w.Code != 202 || body["detail"] != "cancelled" {
				t.Fatal(w.Code, body)
			}
			body = h.poll(id, 500)
			if body["detail"] != "cancelled" {
				t.Fatal(body)
			}
			w, _ = h.call("POST", "/admin/api/jobs/"+id+"/cancel", "")
			if w.Code != 409 {
				t.Fatal(w.Code)
			}
			states, err := h.rt.List(context.Background())
			if err != nil || len(states) != 0 {
				t.Fatal(states, err)
			}
		})
	}
	h := diagnosticsHarness(t)
	h.start()
	_, body := h.call("POST", "/api/v1/build", buildBody)
	id := body["request_hash"].(string)
	h.poll(id, 200)
	w, _ := h.call("POST", "/admin/api/jobs/"+id+"/cancel", "")
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	w, _ = h.call("POST", "/admin/api/jobs/missing/cancel", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestReplayIsolationOptionsAndAdminJobs(t *testing.T) {
	h := diagnosticsHarness(t)
	_, original := h.call("POST", "/api/v1/build", buildBody)
	id := original["request_hash"].(string)
	_, snapshot := h.call("GET", "/admin/api/jobs/"+id, "")
	before, _ := json.Marshal(snapshot)
	var statsBefore int
	if err := h.db.QueryRow(`SELECT sum(value) FROM stat_counters`).Scan(&statsBefore); err != nil {
		t.Fatal(err)
	}
	replays := []string{}
	for i := 0; i < 2; i++ {
		w, body := h.call("POST", "/admin/api/jobs/"+id+"/replay", `{"options":{"image_tag":"debug","pull_policy":"always"}}`)
		if w.Code != 202 || body["replay_of"] != id {
			t.Fatal(w.Code, body)
		}
		rp := body["id"].(string)
		if !regexp.MustCompile(`^rp_[0-7][0-9A-HJKMNP-TV-Z]{25}$`).MatchString(rp) {
			t.Fatal(rp)
		}
		replays = append(replays, rp)
		w, _ = h.call("GET", "/api/v1/build/"+rp, "")
		if w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
	if replays[0] == replays[1] {
		t.Fatal("重复 ID")
	}
	_, snapshot = h.call("GET", "/admin/api/jobs/"+id, "")
	after, _ := json.Marshal(snapshot)
	if string(before) != string(after) {
		t.Fatal("重放改动了原记录")
	}
	w, list := h.call("GET", "/admin/api/jobs?status=queued&page=1&page_size=2", "")
	if w.Code != 200 || list["total"] != float64(3) || len(list["items"].([]any)) != 2 {
		t.Fatal(w.Code, list)
	}
	w, _ = h.call("GET", "/admin/api/jobs?status=bogus", "")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	// 原记录不执行；重放失败，Fake 检查完整脚本、镜像与保留标签。
	if _, err := h.db.Exec(`UPDATE jobs SET status='failed' WHERE request_hash=?`, id); err != nil {
		t.Fatal(err)
	}
	h.rt.plan.ExitCode = 2
	jobs := make(chan runner.Job, 2)
	h.q.Runner.BuildScript = func(j runner.Job) (string, error) {
		jobs <- j
		return runner.Script(j)
	}
	h.start()
	for _, rp := range replays {
		detail := h.adminPoll(rp, "failed")
		if detail["replay_of"] != id || detail["request"] == nil || detail["stages"] == nil || detail["error"] == nil {
			t.Fatal(detail)
		}
		j := <-jobs
		if !j.Verbose || !j.KeepContainer || j.PullPolicy != "always" || !strings.HasSuffix(j.Image, ":debug") {
			t.Fatal(j)
		}
		script, err := runner.Script(j)
		if err != nil || !strings.Contains(script, "V=s") {
			t.Fatal(script, err)
		}
		if _, err := os.Stat(filepath.Join(h.dir, "store", rp)); !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	states, err := h.rt.List(context.Background())
	if err != nil || len(states) != 2 {
		t.Fatal(states, err)
	}
	for _, s := range states {
		if s.Labels["asu.keep_container"] != "true" {
			t.Fatal(s)
		}
	}
	var statsAfter int
	if err := h.db.QueryRow(`SELECT sum(value) FROM stat_counters`).Scan(&statsAfter); err != nil || statsAfter != statsBefore {
		t.Fatal(statsBefore, statsAfter, err)
	}
}

func TestDirectReplayLimitsAndRetention(t *testing.T) {
	h := diagnosticsHarness(t)
	body := strings.TrimSuffix(buildBody, "}") + `,"defaults":"echo hi"}`
	w, _ := h.call("POST", "/admin/api/replay", body)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	body = strings.TrimSuffix(body, "}") + `,"options":{"skip_limits":true,"verbose":false,"keep_container":false}}`
	w, replay := h.call("POST", "/admin/api/replay", body)
	if w.Code != 202 {
		t.Fatal(w.Code, replay)
	}
	var r buildreq.Request
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	hash, _ := r.Normalize().Hash()
	if replay["replay_of"] != hash {
		t.Fatal(replay, hash)
	}
	id := replay["id"].(string)
	_, detail := h.call("GET", "/admin/api/jobs/"+id, "")
	opts := detail["options"].(map[string]any)
	if opts["verbose"] != false || opts["keep_container"] != false {
		t.Fatal(opts)
	}
	h.rt.plan.ExitCode = 2
	h.start()
	h.adminPoll(id, "failed")
	states, err := h.rt.List(context.Background())
	if err != nil || len(states) != 0 {
		t.Fatal(states, err)
	}
	// 普通失败记录过期后公开不可见，详情与日志仍可诊断。
	_, original := h.call("POST", "/api/v1/build", buildBody)
	originalID := original["request_hash"].(string)
	h.poll(originalID, 500)
	if _, err := h.db.Exec(`UPDATE jobs SET expires_at=? WHERE request_hash=?`, time.Now().UTC().Add(-time.Hour), originalID); err != nil {
		t.Fatal(err)
	}
	w, _ = h.call("GET", "/api/v1/build/"+originalID, "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w, _ = h.call("GET", "/admin/api/jobs/"+originalID, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	log := httptest.NewRecorder()
	h.router.ServeHTTP(log, httptest.NewRequest("GET", "/admin/api/jobs/"+originalID+"/log", nil))
	if log.Code != 200 {
		t.Fatal(log.Code)
	}
	if _, err := h.db.Exec(`UPDATE jobs SET purge_at=? WHERE request_hash=?`, time.Now().UTC().Add(-time.Hour), originalID); err != nil {
		t.Fatal(err)
	}
	w, _ = h.call("GET", "/admin/api/jobs/"+originalID, "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	log = httptest.NewRecorder()
	h.router.ServeHTTP(log, httptest.NewRequest("GET", "/admin/api/jobs/"+originalID+"/log", nil))
	if log.Code != 404 {
		t.Fatal(log.Code)
	}
}
