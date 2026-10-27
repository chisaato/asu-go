package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/queue"
	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/store"
	"github.com/gzzchh/asu-go/internal/stream"
	"go.uber.org/zap"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *queue.SQLite, *queue.Cleaner, *config.Config, runtime.Runtime, *stream.Hub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()

	tmp := t.TempDir()
	storeDir := filepath.Join(tmp, "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(context.Background(), tmp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	hub, err := stream.NewHub(tmp, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	cfg := config.Default()
	cfg.DataDir = tmp
	cfg.Queue.MaxConcurrentBuilds = 4

	fakeRt := runtime.NewFake()
	q := queue.NewSQLite(database.DB, nil, nil, nil, queue.Options{
		DataDir:             tmp,
		MaxConcurrentBuilds: 4,
	})

	artifactStore := &store.Local{Dir: storeDir, Logger: zap.NewNop()}
	cleaner := &queue.Cleaner{
		Jobs:     q,
		Store:    artifactStore,
		StoreDir: storeDir,
		Logger:   zap.NewNop(),
	}

	adminGroup := router.Group("/admin/api")
	RegisterJobs(adminGroup, q)
	RegisterLogs(adminGroup, stream.Service{Hub: hub, Jobs: q})
	RegisterEvents(adminGroup, q, fakeRt, &cfg, stream.Service{Hub: hub, Jobs: q})
	RegisterOverview(adminGroup, q, fakeRt, &cfg)
	RegisterSettings(adminGroup, &cfg, fakeRt)
	RegisterStore(adminGroup, cleaner)

	return router, q, cleaner, &cfg, fakeRt, hub
}

func TestOverviewEndpoint(t *testing.T) {
	router, q, _, _, _, _ := setupTestRouter(t)

	// 插入一个测试任务
	now := time.Now().UTC()
	_, err := q.DB.Exec(`INSERT INTO jobs (request_hash, version, target, profile, status, enqueued_at, request) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"hash1", "24.10.0", "x86/64", "generic", "running", now, `{"profile":"generic"}`)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp OverviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Slots.Running != 1 || resp.Slots.Max != 4 {
		t.Fatalf("unexpected slots: %+v", resp.Slots)
	}
	if len(resp.Active) != 1 {
		t.Fatalf("expected 1 active build, got %d", len(resp.Active))
	}
}

func TestSettingsAndProbeEndpoint(t *testing.T) {
	router, _, _, _, _, _ := setupTestRouter(t)

	// GET /settings
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/settings", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var s SettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Runner.Concurrency != 4 {
		t.Fatalf("expected concurrency 4, got %d", s.Runner.Concurrency)
	}

	// POST /engine/probe
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/admin/api/engine/probe", nil)
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var eng EngineInfo
	if err := json.Unmarshal(w2.Body.Bytes(), &eng); err != nil {
		t.Fatal(err)
	}
	if eng.Type != "fake" {
		t.Fatalf("expected fake engine, got %s", eng.Type)
	}
}

func TestStorageEndpoints(t *testing.T) {
	router, q, cleaner, _, _, _ := setupTestRouter(t)

	// 创建一个模拟产物目录与完成任务
	hash := "deadbeef00000000deadbeef00000000deadbeef00000000deadbeef00000000"
	targetDir := filepath.Join(cleaner.StoreDir, hash)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(targetDir, "openwrt.bin"), []byte("firmware-data"), 0o644)

	now := time.Now().UTC()
	_, err := q.DB.Exec(`INSERT INTO jobs (request_hash, version, target, profile, status, enqueued_at, expires_at, request) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hash, "24.10.0", "x86/64", "generic", "done", now, now.Add(time.Hour), `{"profile":"generic"}`)
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET /storage
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/storage", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stOverview StorageOverviewResponse
	_ = json.Unmarshal(w.Body.Bytes(), &stOverview)
	if stOverview.Artifacts.Count != 1 {
		t.Fatalf("expected 1 artifact, got %d", stOverview.Artifacts.Count)
	}

	// 2. GET /storage/artifacts
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/admin/api/storage/artifacts", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. POST /storage/clean (dry_run)
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/admin/api/storage/clean", strings.NewReader(`{"scope":"orphans","dry_run":true}`))
	req3.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestJobsFilterExtended(t *testing.T) {
	router, q, _, _, _, _ := setupTestRouter(t)

	now := time.Now().UTC()
	_, _ = q.DB.Exec(`INSERT INTO jobs (request_hash, version, target, profile, status, enqueued_at, client, request) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"hashA", "24.10.0", "x86/64", "router_a", "done", now, "owut/1.2", `{"profile":"router_a"}`)
	_, _ = q.DB.Exec(`INSERT INTO jobs (request_hash, version, target, profile, status, enqueued_at, client, request) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"hashB", "23.05.5", "mediatek/filogic", "router_b", "failed", now, "luci/23", `{"profile":"router_b"}`)

	// 查询 q=router_a
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/jobs?q=router_a", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["total"] != float64(1) {
		t.Fatalf("expected 1 match, got %v", res["total"])
	}

	// 查询 status 逗号分隔
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/admin/api/jobs?status=done,failed", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var res2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &res2)
	if res2["total"] != float64(2) {
		t.Fatalf("expected 2 matches, got %v", res2["total"])
	}
}
