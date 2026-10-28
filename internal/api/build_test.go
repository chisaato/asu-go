package api

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/admin"
	"github.com/chisaato/asu-go/internal/buildreq"
	"github.com/chisaato/asu-go/internal/db"
	"github.com/chisaato/asu-go/internal/httplog"
	"github.com/chisaato/asu-go/internal/metadata"
	"github.com/chisaato/asu-go/internal/queue"
	"github.com/chisaato/asu-go/internal/runner"
	"github.com/chisaato/asu-go/internal/runtime"
	"github.com/chisaato/asu-go/internal/sources"
	"github.com/chisaato/asu-go/internal/store"
	"github.com/chisaato/asu-go/internal/stream"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type buildMetadata struct{}

func (buildMetadata) Profile(_ context.Context, _, _, _, p string) (map[string]any, error) {
	if p == "missing" {
		return nil, &metadata.ProfileNotFoundError{Profile: p}
	}
	return map[string]any{"id": p, "default_packages": []any{"base"}, "device_packages": []any{"driver"}}, nil
}
func (buildMetadata) Targets(context.Context, string, string) (map[string]any, error) {
	return map[string]any{"x86/64": "x86_64"}, nil
}
func (buildMetadata) Packages(context.Context, string, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}

type artifactRuntime struct {
	*runtime.Fake
	mu   sync.Mutex
	plan runtime.FakePlan
}

func (f *artifactRuntime) Create(ctx context.Context, s runtime.RunSpec) (runtime.Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plan := f.plan
	var data bytes.Buffer
	tw := tar.NewWriter(&data)
	files := map[string]string{"profiles.json": `{"source_date_epoch":1700000000,"version_number":"24.10.0","profiles":{"generic":{"images":[{"name":"firmware.bin","type":"sysupgrade"}],"supported_devices":["test"]}}}`, "manifest": "base - 1\nluci - 2\n", "build_cmd_packages": "base luci\n", "firmware.bin": "firmware"}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: s.RequestHash + "/" + name, Mode: 0644, Size: int64(len(body))}); err != nil {
			return "", err
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	plan.Archives = map[string][]byte{"/builder/" + s.RequestHash: data.Bytes()}
	f.Next = plan
	return f.Fake.Create(ctx, s)
}

type buildHarness struct {
	t      *testing.T
	q      *queue.SQLite
	db     *db.DB
	router *gin.Engine
	rt     *artifactRuntime
	dir    string
	cancel context.CancelFunc
	done   chan error
}

func newBuildHarness(t *testing.T) *buildHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	database, err := db.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog := sources.NewStore(database)
	source := sources.NewSource()
	source.Distribution = "openwrt"
	source.Version = "24.10.0"
	source.Branch = "24.10"
	source.Image = "docker.io/library/alpine"
	source.UpstreamURL = "https://example.com"
	source.TagTemplate = "latest"
	source.Targets = map[string]any{"x86/64": "x86_64"}
	if _, err = catalog.Create(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	hub, err := stream.NewHub(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	rt := &artifactRuntime{Fake: runtime.NewFake(), plan: runtime.FakePlan{Output: "##asu:stage=container_setup\n##asu:stage=building_image\n", Delay: 40 * time.Millisecond}}
	q := queue.NewSQLite(database.DB, catalog, buildMetadata{}, &runner.Runner{Runtime: rt, Hub: hub}, queue.Options{DataDir: dir, MaxConcurrentBuilds: 1, MaxPendingJobs: 2, BuildTTL: 7 * 24 * time.Hour, BuildUnversionedTTL: 24 * time.Hour, BuildDefaultsTTL: 30 * time.Minute, BuildFailureTTL: time.Hour, Retention: 7 * 24 * time.Hour})
	router := gin.New()
	RegisterBuild(router, q)
	RegisterStore(router, q.Publisher.(*store.Local))
	admin.RegisterLogs(router.Group("/admin/api"), stream.Service{Hub: hub, Jobs: q})
	h := &buildHarness{t: t, q: q, db: database, rt: rt, dir: dir, router: router}
	t.Cleanup(func() {
		if h.cancel != nil {
			h.cancel()
			if err := <-h.done; err != nil {
				t.Error(err)
			}
		}
		_ = hub.Close()
		_ = database.Close()
	})
	return h
}
func (h *buildHarness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.done = make(chan error, 1)
	go func() { h.done <- h.q.Run(ctx) }()
}

func TestBuildDistroScope(t *testing.T) {
	h := newBuildHarness(t)
	ctx := context.Background()
	catalog := sources.NewStore(h.db)
	all, err := catalog.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	immortal := all[0]
	immortal.Distribution = "immortalwrt"
	if _, err := catalog.Create(ctx, immortal); err != nil {
		t.Fatal(err)
	}
	// 根路径默认 ImmortalWrt，证明不是硬编码的 OpenWrt。
	h.router = gin.New()
	h.router.Use(DistroScope(catalog, "immortalwrt"))
	for _, routes := range []gin.IRouter{h.router, h.router.Group("/:distro")} {
		RegisterBuild(routes, h.q)
	}
	body := `"version":"24.10.0","target":"x86/64","profile":"generic"`
	hashes := map[string]string{}
	for _, tc := range []struct{ path, extra, distro string }{
		{"", "", "immortalwrt"},
		{"/immortalwrt", "", "immortalwrt"},
		{"/immortalwrt", `,"distro":"immortalwrt"`, "immortalwrt"},
		{"/openwrt", "", "openwrt"},
		{"", `,"distro":"openwrt"`, "openwrt"},
	} {
		w, response := h.call("POST", tc.path+"/api/v1/build", "{"+body+tc.extra+"}")
		if w.Code != 202 {
			t.Fatalf("%s %s: %d %s", tc.path, tc.extra, w.Code, w.Body)
		}
		hash, _ := response["request_hash"].(string)
		var raw string
		if err := h.db.QueryRowContext(ctx, "SELECT request FROM jobs WHERE request_hash=?", hash).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var request buildreq.Request
		if err := json.Unmarshal([]byte(raw), &request); err != nil || request.Distro != tc.distro {
			t.Fatalf("实际 distro=%s, 期望 %s: %v", request.Distro, tc.distro, err)
		}
		if previous := hashes[tc.distro]; previous != "" && previous != hash {
			t.Fatal("路径前缀改变了相同请求的 hash")
		}
		hashes[tc.distro] = hash
		for _, method := range []string{"GET", "HEAD"} {
			w, _ := h.call(method, tc.path+"/api/v1/build/"+hash, "")
			if w.Code != 202 {
				t.Fatalf("%s 轮询失败: %d", method, w.Code)
			}
		}
		w, _ = h.call("GET", tc.path+"/api/v1/stats", "")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	if hashes["openwrt"] == hashes["immortalwrt"] {
		t.Fatal("hash 未区分 distro")
	}
	w, _ := h.call("POST", "/immortalwrt/api/v1/build", "{"+body+`,"distro":"openwrt"}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Unsupported distro: openwrt") || !strings.Contains(w.Body.String(), "path prefix immortalwrt") {
		t.Fatalf("未拒绝前缀冲突: %d %s", w.Code, w.Body)
	}
}
func (h *buildHarness) call(method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	response := map[string]any{}
	if method != http.MethodHead {
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			h.t.Fatalf("invalid response %d %s: %v", w.Code, w.Body, err)
		}
	}
	return w, response
}

const buildBody = `{"version":"24.10.0","target":"x86/64","profile":"generic","packages":["luci","base"]}`

func (h *buildHarness) poll(id string, want int) map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w, body := h.call("GET", "/api/v1/build/"+id, "")
		if w.Code == want {
			return body
		}
		if w.Code != 202 {
			h.t.Fatalf("poll status=%d body=%v want=%d", w.Code, body, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("轮询超时")
	return nil
}
func TestBuildSubmitPoll(t *testing.T) {
	h := newBuildHarness(t)
	w, queued := h.call("POST", "/api/v1/build", buildBody)
	if w.Code != 202 || queued["detail"] != "queued" || queued["queue_position"] != float64(0) || w.Header().Get("X-Imagebuilder-Status") != "queued" || w.Header().Get("X-Queue-Position") != "0" {
		t.Fatal(w.Code, queued, w.Header())
	}
	id := queued["request_hash"].(string)
	w, duplicate := h.call("POST", "/api/v1/build", strings.Replace(buildBody, `["luci","base"]`, `["+base","luci","base"]`, 1))
	if w.Code != 202 || duplicate["request_hash"] != id {
		t.Fatal(w.Code, duplicate)
	}
	w, stats := h.call("GET", "/api/v1/stats", "")
	if w.Code != 200 || stats["queue_length"] != float64(1) {
		t.Fatal(stats)
	}
	h.start()
	result := h.poll(id, 200)
	for _, key := range []string{"status", "detail", "id", "bin_dir", "images", "manifest", "build_cmd_packages", "build_at", "version_number", "enqueued_at", "request_hash"} {
		if _, ok := result[key]; !ok {
			t.Errorf("完成响应缺少 %s: %v", key, result)
		}
	}
	if result["bin_dir"] != id || result["id"] != "generic" || result["build_at"] != "2023-11-14T22:13:20.000000Z" {
		t.Fatal(result)
	}
	image := result["images"].([]any)[0].(map[string]any)
	download := httptest.NewRecorder()
	h.router.ServeHTTP(download, httptest.NewRequest("GET", "/store/"+result["bin_dir"].(string)+"/"+image["name"].(string), nil))
	if download.Code != 200 || download.Body.String() != "firmware" {
		t.Fatal(download)
	}
	sum := sha256.Sum256([]byte("firmware"))
	if image["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatal(image)
	}
	if data, err := os.ReadFile(filepath.Join(h.dir, "store", id, "firmware.bin")); err != nil || string(data) != "firmware" {
		t.Fatal(string(data), err)
	}
	w, _ = h.call("HEAD", "/api/v1/build/"+id, "")
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("X-Imagebuilder-Status") != "done" {
		t.Fatal(w)
	}
	w, _ = h.call("POST", "/api/v1/build", buildBody)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for metric, want := range map[string]int{"requests": 3, "cache_hits": 2, "package": 1} {
		var got int
		if err := h.db.QueryRow(`SELECT COALESCE(sum(value),0) FROM stat_counters WHERE metric=?`, metric).Scan(&got); err != nil || got != want {
			t.Errorf("%s=%d want %d: %v", metric, got, want, err)
		}
	}
	var raw string
	var sourceID int
	var expires, purge, finished time.Time
	if err := h.db.QueryRow(`SELECT request,source_id,expires_at,purge_at,finished_at FROM jobs WHERE request_hash=?`, id).Scan(&raw, &sourceID, &expires, &purge, &finished); err != nil {
		t.Fatal(err)
	}
	if sourceID == 0 || expires.Sub(finished) != 24*time.Hour || purge.Sub(finished) != 7*24*time.Hour {
		t.Fatal(sourceID, expires, purge, finished)
	}
	var request buildreq.Request
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatal(err)
	}
	canonical, _ := request.Canonical()
	if raw != string(canonical) {
		t.Fatal("request 不是规范 JSON")
	}
}

func TestBuildQueuePositionsOverloadAndRestart(t *testing.T) {
	h := newBuildHarness(t)
	_, first := h.call("POST", "/api/v1/build", buildBody)
	secondBody := strings.Replace(buildBody, `"luci"`, `"extra"`, 1)
	w, second := h.call("POST", "/api/v1/build", secondBody)
	if w.Code != 202 || second["queue_position"] != float64(1) || w.Header().Get("X-Queue-Position") != "1" {
		t.Fatal(w.Code, second, w.Header())
	}
	w, overloaded := h.call("POST", "/api/v1/build", strings.Replace(buildBody, `"luci"`, `"other"`, 1))
	if w.Code != 529 || overloaded["title"] != "Server overloaded" || overloaded["detail"] != "server overload, queue contains too many build requests: 2" {
		t.Fatal(w.Code, overloaded)
	}
	// 丢弃内存队列实例：新实例仅从持久 jobs 恢复 queued。
	h.q = queue.NewSQLite(h.db.DB, h.q.Sources, h.q.Metadata, h.q.Runner, h.q.Options)
	h.router = gin.New()
	RegisterBuild(h.router, h.q)
	h.start()
	h.poll(first["request_hash"].(string), 200)
	h.poll(second["request_hash"].(string), 200)
	var n int
	if err := h.db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}

func TestBuildFailure(t *testing.T) {
	h := newBuildHarness(t)
	h.rt.plan.Output = "image is too big\n"
	h.rt.plan.ExitCode = 1
	_, queued := h.call("POST", "/api/v1/build", buildBody)
	id := queued["request_hash"].(string)
	h.start()
	result := h.poll(id, 500)
	if result["detail"] != "Selected packages exceed device storage" || !strings.Contains(result["stderr"].(string), "is too big") {
		t.Fatal(result)
	}
	w, _ := h.call("HEAD", "/api/v1/build/"+id, "")
	if w.Code != 500 || w.Header().Get("X-Imagebuilder-Status") != "failed" {
		t.Fatal(w)
	}
	var n int
	if err := h.db.QueryRow(`SELECT value FROM stat_counters WHERE metric='failures'`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var expires, finished time.Time
	if err := h.db.QueryRow(`SELECT expires_at,finished_at FROM jobs`).Scan(&expires, &finished); err != nil || expires.Sub(finished) != time.Hour {
		t.Fatal(expires, finished, err)
	}
	// 等上一轮 worker 退出后改为成功计划，验证再次提交会实际执行新构建。
	h.cancel()
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
	h.cancel = nil
	h.rt.plan.ExitCode = 0
	h.rt.plan.Output = "新的构建输出\n"
	w, retried := h.call("POST", "/api/v1/build", buildBody)
	if w.Code != 202 || retried["request_hash"] != id || w.Header().Get("X-Imagebuilder-Status") != "queued" {
		t.Fatal(w.Code, retried)
	}
	h.start()
	h.poll(id, 200)
	data, err := os.ReadFile(filepath.Join(h.dir, "logs", id+".log"))
	if err != nil || !strings.Contains(string(data), "新的构建输出") || strings.Contains(string(data), "is too big") {
		t.Fatal(string(data), err)
	}
	// 同 hash 重建不产生额外日志文件，常规失败/成功容器均由 Runner 清理。
	entries, err := os.ReadDir(filepath.Join(h.dir, "logs"))
	if err != nil || len(entries) != 1 || entries[0].Name() != id+".log" {
		t.Fatal(entries, err)
	}
	states, err := h.rt.List(context.Background())
	if err != nil || len(states) != 0 {
		t.Fatal(states, err)
	}
}

func TestFailedBuildRetryOverloadPreservesFailure(t *testing.T) {
	h := newBuildHarness(t)
	_, body := h.call("POST", "/api/v1/build", buildBody)
	id := body["request_hash"].(string)
	if _, err := h.q.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		w, _ := h.call("POST", "/api/v1/build", strings.Replace(buildBody, "luci", name, 1))
		if w.Code != 202 {
			t.Fatal(w.Code)
		}
	}
	w, _ := h.call("POST", "/api/v1/build", buildBody)
	if w.Code != 529 {
		t.Fatal(w.Code)
	}
	w, failure := h.call("GET", "/api/v1/build/"+id, "")
	if w.Code != 500 || failure["detail"] != "cancelled" {
		t.Fatal(w.Code, failure)
	}
}

func TestBuildInternalErrorIsLogged(t *testing.T) {
	h := newBuildHarness(t)
	core, logs := observer.New(zapcore.DebugLevel)
	h.router = gin.New()
	h.router.Use(httplog.HTTP(zap.New(core)))
	RegisterBuild(h.router, h.q)
	if _, err := h.db.Exec(`DROP TABLE jobs`); err != nil {
		t.Fatal(err)
	}
	w, body := h.call("GET", "/api/v1/build/test-hash", "")
	if w.Code != 500 || body["detail"] != "Internal Server Error" {
		t.Fatal(w.Code, body)
	}
	fields := logs.All()[0].ContextMap()
	if fields["request_hash"] != "test-hash" || !strings.Contains(fields["error"].(string), "no such table") {
		t.Fatal(fields)
	}
}

func TestConcurrentFailedBuildResubmission(t *testing.T) {
	for _, reason := range []string{"failed", "timeout", "cancelled", "interrupted"} {
		t.Run(reason, func(t *testing.T) {
			h := newBuildHarness(t)
			_, queued := h.call("POST", "/api/v1/build", buildBody)
			id := queued["request_hash"].(string)
			if _, err := h.db.Exec(`UPDATE jobs SET status='failed',stage='failed',detail=?,error='old output',finished_at=? WHERE request_hash=?`, reason, time.Now().UTC(), id); err != nil {
				t.Fatal(err)
			}
			core, logs := observer.New(zapcore.DebugLevel)
			h.router = gin.New()
			h.router.Use(httplog.HTTP(zap.New(core)))
			RegisterBuild(h.router, h.q)
			for _, method := range []string{"GET", "HEAD"} {
				w, _ := h.call(method, "/api/v1/build/"+id, "")
				if w.Code != 500 || w.Header().Get("X-Imagebuilder-Status") != "failed" {
					t.Fatal(w.Code)
				}
				fields := logs.All()[logs.Len()-1].ContextMap()
				if fields["request_hash"] != id || fields["error"] != reason || strings.Contains(fields["error"].(string), "old output") {
					t.Fatal(fields)
				}
			}
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Go(func() {
					w := httptest.NewRecorder()
					h.router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/build", strings.NewReader(buildBody)))
					if w.Code != 202 {
						t.Errorf("重新提交失败: %d %s", w.Code, w.Body)
					}
				})
			}
			wg.Wait()
			var jobs, hits, reset int
			if err := h.db.QueryRow(`SELECT count(*),sum(status='queued' AND stage='init' AND detail IS NULL AND error IS NULL AND finished_at IS NULL AND handle IS NULL) FROM jobs WHERE request_hash=?`, id).Scan(&jobs, &reset); err != nil || jobs != 1 || reset != 1 {
				t.Fatal(jobs, reset, err)
			}
			if err := h.db.QueryRow(`SELECT sum(value) FROM stat_counters WHERE metric='cache_hits'`).Scan(&hits); err != nil || hits != 11 {
				t.Fatal(hits, err)
			}
		})
	}
}

func TestBuildResponseCompatibility(t *testing.T) {
	h := newBuildHarness(t)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		detail                   any
	}{
		{"missing", "GET", "/api/v1/build/missing", "", 404, "could not find provided request hash"},
		{"distro", "POST", "/api/v1/build", strings.Replace(buildBody, `"version"`, `"distro":"unknown","version"`, 1), 400, "Unsupported distro: unknown"},
		{"branch", "POST", "/api/v1/build", strings.Replace(buildBody, "24.10.0", "99.99.0", 1), 400, "Unsupported branch: 99.99.0"},
		{"version", "POST", "/api/v1/build", strings.Replace(buildBody, "24.10.0", "24.10.99", 1), 400, "Unsupported version: 24.10.99"},
		{"target", "POST", "/api/v1/build", strings.Replace(buildBody, "x86/64", "x86/missing", 1), 400, "Unsupported target: x86/missing. The requested target was either dropped, is still being built or is not supported by the selected version. Please check the forums or try again later."},
		{"profile", "POST", "/api/v1/build", strings.Replace(buildBody, "generic", "missing", 1), 400, "Unsupported profile: missing. The requested profile was either dropped or never existed. Please check the forums for more information."},
		{"defaults", "POST", "/api/v1/build", strings.Replace(buildBody, `"version"`, `"defaults":"echo test","version"`, 1), 400, "Handling `defaults` not enabled on server"},
		{"model", "POST", "/api/v1/build", `{"version":"24.10.0"}`, 422, nil},
		{"json", "POST", "/api/v1/build", `{`, 422, nil},
		{"regex", "POST", "/api/v1/build", strings.Replace(buildBody, "generic", "../generic", 1), 422, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, body := h.call(tc.method, tc.path, tc.body)
			if w.Code != tc.status {
				t.Fatal(w.Code, body)
			}
			if tc.detail != nil {
				if body["detail"] != tc.detail || body["status"] != float64(tc.status) {
					t.Fatal(body)
				}
			} else if _, ok := body["detail"].([]any); !ok {
				t.Fatal(body)
			}
			if tc.status == 404 && body["title"] != "Not Found" {
				t.Fatal(body)
			}
		})
	}
}

// 确保测试 runtime 保持接口完整，下载不绕过 Runner 的 archive 提取。
var _ runtime.Runtime = (*artifactRuntime)(nil)

func TestBuildRunningAndLogs(t *testing.T) {
	h := newBuildHarness(t)
	h.rt.plan.Delay = 150 * time.Millisecond
	_, queued := h.call("POST", "/api/v1/build", buildBody)
	id := queued["request_hash"].(string)
	h.start()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w, body := h.call("GET", "/api/v1/build/"+id, "")
		if body["imagebuilder_status"] == "building_image" {
			if w.Code != 202 || body["detail"] != "started" || w.Header().Get("X-Imagebuilder-Status") != "building_image" || w.Header().Get("X-Queue-Position") != "0" {
				t.Fatal(w.Code, body, w.Header())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("未进入 building_image", body)
		}
		time.Sleep(time.Millisecond)
	}
	h.poll(id, 200)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, httptest.NewRequest("GET", "/admin/api/jobs/"+id+"/log", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "building_image") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestBuildTakeoverPublishesResult(t *testing.T) {
	h := newBuildHarness(t)
	_, queued := h.call("POST", "/api/v1/build", buildBody)
	id := queued["request_hash"].(string)
	ctx := context.Background()
	handle, err := h.rt.Create(ctx, runtime.RunSpec{Image: "test", RequestHash: id})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.rt.Start(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if _, err = h.db.Exec(`UPDATE jobs SET status='running',started_at=? WHERE request_hash=?`, time.Now().UTC(), id); err != nil {
		t.Fatal(err)
	}
	if err = h.q.SaveHandle(ctx, id, handle); err != nil {
		t.Fatal(err)
	}
	h.start()
	body := h.poll(id, 200)
	if body["id"] != "generic" || body["bin_dir"] != id {
		t.Fatal(body)
	}
	var status string
	var stages string
	if err = h.db.QueryRow(`SELECT status,stages FROM jobs WHERE request_hash=?`, id).Scan(&status, &stages); err != nil || status != "done" || !strings.Contains(stages, "building_image") {
		t.Fatal(status, stages, err)
	}
}

func TestBuildTTLsExpiryAndNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		ttl         time.Duration
	}{
		{"versioned", `"packages_versions":{"base":"1"},`, 7 * 24 * time.Hour},
		{"defaults", `"defaults":"echo test","packages_versions":{"base":"1"},`, 30 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newBuildHarness(t)
			h.q.Options.Limits.AllowDefaults = true
			body := strings.Replace(buildBody, `"version"`, tc.extra+`"version"`, 1)
			_, queued := h.call("POST", "/api/v1/build", body)
			id := queued["request_hash"].(string)
			var expires, enqueued time.Time
			if err := h.db.QueryRow(`SELECT expires_at,enqueued_at FROM jobs`).Scan(&expires, &enqueued); err != nil || expires.Sub(enqueued) != tc.ttl {
				t.Fatal(expires, enqueued, err)
			}
			h.start()
			h.poll(id, 200)
			// 已存在请求在策略检查前命中缓存。
			h.q.Options.Limits.AllowDefaults = false
			w, _ := h.call("POST", "/api/v1/build", body)
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			if _, err := h.db.Exec(`UPDATE jobs SET expires_at=? WHERE request_hash=?`, time.Now().UTC().Add(-time.Hour), id); err != nil {
				t.Fatal(err)
			}
			w, missing := h.call("GET", "/api/v1/build/"+id, "")
			if w.Code != 404 || missing["detail"] != "could not find provided request hash" {
				t.Fatal(w.Code, missing)
			}
		})
	}
	h := newBuildHarness(t)
	_, first := h.call("POST", "/api/v1/build", strings.Replace(buildBody, "generic", "vendor,model", 1))
	_, second := h.call("POST", "/api/v1/build", strings.Replace(buildBody, "generic", "vendor_model", 1))
	if first["request_hash"] != second["request_hash"] {
		t.Fatal(first, second)
	}
}

func TestConcurrentBuildDeduplication(t *testing.T) {
	h := newBuildHarness(t)
	var wg sync.WaitGroup
	errors := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/build", strings.NewReader(buildBody)))
			if w.Code != 202 {
				errors <- w.Body.String()
			}
		}()
	}
	wg.Wait()
	close(errors)
	for message := range errors {
		t.Error(message)
	}
	var jobs, hits int
	if err := h.db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT sum(value) FROM stat_counters WHERE metric='cache_hits'`).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || hits != 11 {
		t.Fatal(jobs, hits)
	}
}

func TestBuildWorkerConcurrency(t *testing.T) {
	h := newBuildHarness(t)
	h.q.Options.MaxConcurrentBuilds = 2
	h.q.Options.MaxPendingJobs = 3
	h.rt.plan.Delay = 250 * time.Millisecond
	ids := []string{}
	for _, p := range []string{"one", "two", "three"} {
		w, body := h.call("POST", "/api/v1/build", strings.Replace(buildBody, `"luci"`, `"`+p+`"`, 1))
		if w.Code != 202 {
			t.Fatal(w.Code, body)
		}
		ids = append(ids, body["request_hash"].(string))
	}
	h.start()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var running, pending int
		if err := h.db.QueryRow(`SELECT count(*) FROM jobs WHERE status='running'`).Scan(&running); err != nil {
			t.Fatal(err)
		}
		if running > 2 {
			t.Fatal("超过并发限制", running)
		}
		if err := h.db.QueryRow(`SELECT count(*) FROM jobs WHERE status='queued'`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if running == 2 && pending == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker pool 未按配置启动", running, pending)
		}
		time.Sleep(time.Millisecond)
	}
	for _, id := range ids {
		h.poll(id, 200)
	}
}

func TestExpiredBuildCanBeResubmitted(t *testing.T) {
	h := newBuildHarness(t)
	_, body := h.call("POST", "/api/v1/build", buildBody)
	id := body["request_hash"].(string)
	h.start()
	h.poll(id, 200)
	if _, err := h.db.Exec(`UPDATE jobs SET expires_at=? WHERE request_hash=?`, time.Now().UTC().Add(-time.Hour), id); err != nil {
		t.Fatal(err)
	}
	w, body := h.call("POST", "/api/v1/build", buildBody)
	if w.Code != 202 || body["detail"] != "queued" || body["request_hash"] != id {
		t.Fatal(w.Code, body)
	}
	h.poll(id, 200)
	var hits int
	if err := h.db.QueryRow(`SELECT COALESCE(sum(value),0) FROM stat_counters WHERE metric='cache_hits'`).Scan(&hits); err != nil || hits != 0 {
		t.Fatal(hits, err)
	}
}

func TestReplayIsPrivateAndDoesNotCount(t *testing.T) {
	h := newBuildHarness(t)
	_, queued := h.call("POST", "/api/v1/build", buildBody)
	if _, err := h.db.Exec(`UPDATE jobs SET request_hash='rp_test',status='running' WHERE request_hash=?`, queued["request_hash"]); err != nil {
		t.Fatal(err)
	}
	if err := h.q.Complete(context.Background(), "rp_test", runner.Result{}, &runner.Error{Kind: "failed", Tail: "diagnostic"}); err != nil {
		t.Fatal(err)
	}
	w, _ := h.call("GET", "/api/v1/build/rp_test", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	var n int
	if err := h.db.QueryRow(`SELECT COALESCE(sum(value),0) FROM stat_counters WHERE metric IN ('failures','package','cache_hits')`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
