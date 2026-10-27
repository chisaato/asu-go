package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/buildreq"
	"github.com/gzzchh/asu-go/internal/sources"
)

func owutFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/owut/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

type owutBuildMetadata struct{ buildMetadata }

func (owutBuildMetadata) Targets(context.Context, string, string) (map[string]any, error) {
	return map[string]any{"mediatek/filogic": "aarch64_cortex-a53"}, nil
}

func TestOWUTCapturedBuildSubmitAndFailure(t *testing.T) {
	h := newBuildHarness(t)
	ctx := context.Background()
	catalog := sources.NewStore(h.db)
	source := sources.NewSource()
	source.Distribution, source.Version, source.Branch = "immortalwrt", "25.12.2", "25.12"
	source.Image, source.UpstreamURL = "fake/imagebuilder", "https://downloads.immortalwrt.org"
	source.Targets = map[string]any{"mediatek/filogic": "aarch64_cortex-a53"}
	if _, err := catalog.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	// 抓包没有 profiles.json；仅 profile 查询使用已有 fake，不伪造真实 profile 夹具。
	h.q.Metadata = owutBuildMetadata{}
	h.router = gin.New()
	h.router.Use(DistroScope(catalog, "openwrt"))
	RegisterBuild(h.router.Group("/:distro"), h.q)

	body := owutFixture(t, "build-request-immortalwrt-25.12.2.json")
	w, queued := h.call("POST", "/immortalwrt/api/v1/build", string(body))
	if w.Code != 202 || w.Header().Get("X-Imagebuilder-Status") != "queued" || w.Header().Get("X-Queue-Position") != "0" {
		t.Fatalf("提交: %d %s %v", w.Code, w.Body, w.Header())
	}
	var captured map[string]any
	if err := json.Unmarshal(owutFixture(t, "build-queued.json"), &captured); err != nil {
		t.Fatal(err)
	}
	assertOWUTBuildResponse(t, queued, captured)
	id := captured["request_hash"].(string)
	var stored string
	if err := h.db.QueryRowContext(ctx, "SELECT request FROM jobs WHERE request_hash=?", id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var request buildreq.Request
	if err := json.Unmarshal([]byte(stored), &request); err != nil || request.Distro != "immortalwrt" {
		t.Fatalf("持久请求发行版: %s, %v", stored, err)
	}

	var failed map[string]any
	if err := json.Unmarshal(owutFixture(t, "build-failed.json"), &failed); err != nil {
		t.Fatal(err)
	}
	// 重现抓包中的非零退出与原始输出，验证协议格式，而不是要求真容器再次发生权限错误。
	h.rt.plan.Output, h.rt.plan.ExitCode = failed["stderr"].(string), 1
	h.start()
	path := "/immortalwrt/api/v1/build/" + id
	deadline := time.Now().Add(20 * time.Second)
	for {
		w, result := h.call("GET", path, "")
		if w.Code == 500 {
			assertOWUTBuildResponse(t, result, failed)
			if w.Header().Get("X-Imagebuilder-Status") != "failed" || w.Header().Get("X-Queue-Position") != "0" {
				t.Fatal(w.Header())
			}
			break
		}
		if w.Code != 202 || time.Now().After(deadline) {
			t.Fatalf("轮询: %d %s", w.Code, w.Body)
		}
		time.Sleep(5 * time.Millisecond)
	}
	w, _ = h.call("HEAD", path, "")
	if w.Code != 500 || w.Body.Len() != 0 || w.Header().Get("X-Imagebuilder-Status") != "failed" {
		t.Fatalf("HEAD: %d %s %v", w.Code, w.Body, w.Header())
	}
}

func assertOWUTBuildResponse(t *testing.T, got, captured map[string]any) {
	t.Helper()
	// 对照原版 asu/routers/api.py:return_job_v1：公共字段必须齐全。
	// enqueued_at 是本次排队时间，不能照抄抓包时间；其余抓包字段逐项相等。
	for key, want := range captured {
		if key == "enqueued_at" {
			value, ok := got[key].(string)
			if !ok {
				t.Fatalf("缺少 enqueued_at: %v", got)
			}
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if got[key] != want {
			t.Errorf("%s=%v，抓包为 %v", key, got[key], want)
		}
	}
}
