package queue_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/admin"
	"github.com/chisaato/asu-go/internal/api"
	"github.com/chisaato/asu-go/internal/db"
	"github.com/chisaato/asu-go/internal/queue"
	"github.com/chisaato/asu-go/internal/store"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func cleanerFixture(t *testing.T) (*queue.Cleaner, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	q := queue.NewSQLite(database.DB, nil, nil, nil, queue.Options{DataDir: dir})
	backend := &store.Local{Dir: filepath.Join(dir, "store")}
	return &queue.Cleaner{Jobs: q, Store: backend, StoreDir: backend.Dir, OrphanGrace: time.Hour, Now: func() time.Time { return now }}, &now
}

func addJob(t *testing.T, c *queue.Cleaner, id, status string, expires, purge time.Time) {
	t.Helper()
	_, err := c.Jobs.DB.Exec(`INSERT INTO jobs(request_hash,version,target,profile,status,enqueued_at,request,result,expires_at,purge_at) VALUES(?,'v','t','p',?,?,'{}','null',?,?)`, id, status, expires.Add(-time.Hour), expires, purge)
	if err != nil {
		t.Fatal(err)
	}
	dir := c.StoreDir
	if strings.HasPrefix(id, "rp_") {
		dir = filepath.Join(c.Jobs.Options.DataDir, "diagnostics")
	}
	writeFile(t, filepath.Join(dir, id, "image.bin"))
	writeFile(t, filepath.Join(c.Jobs.Options.DataDir, "logs", id+".log"))
}

func writeFile(t *testing.T, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("firmware"), 0644); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, name string, want bool) {
	t.Helper()
	_, err := os.Stat(name)
	if (err == nil) != want || (err != nil && !errors.Is(err, os.ErrNotExist)) {
		t.Fatalf("%s exists=%v: %v", name, want, err)
	}
}

func TestCleanerBoundariesAndReplay(t *testing.T) {
	c, now := cleanerFixture(t)
	id := strings.Repeat("a", 64)
	replay := "rp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	expires, purge := *now, now.Add(time.Hour)
	addJob(t, c, id, "done", expires, purge)
	addJob(t, c, replay, "done", expires.Add(-time.Hour), purge)
	kept := strings.Repeat("d", 64)
	addJob(t, c, kept, "failed", expires.Add(-time.Hour), purge)
	if _, err := c.Jobs.DB.Exec(`UPDATE jobs SET options='{"keep_container":true}' WHERE request_hash=?`, kept); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, status string }{{strings.Repeat("b", 64), "queued"}, {strings.Repeat("c", 64), "running"}} {
		addJob(t, c, item.id, item.status, expires.Add(-2*time.Hour), expires.Add(-time.Hour))
	}
	_, err := c.Jobs.DB.Exec(`INSERT INTO stat_counters VALUES('2026-01-01','requests','v',7)`)
	if err != nil {
		t.Fatal(err)
	}
	*now = expires.Add(-time.Nanosecond)
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	exists(t, filepath.Join(c.StoreDir, id), true)
	*now = expires
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	exists(t, filepath.Join(c.StoreDir, id), false)
	exists(t, filepath.Join(c.Jobs.Options.DataDir, "logs", id+".log"), true)
	exists(t, filepath.Join(c.Jobs.Options.DataDir, "diagnostics", replay), true)
	var count int
	if err := c.Jobs.DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 5 {
		t.Fatal(count, err)
	}
	*now = purge.Add(-time.Nanosecond)
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	exists(t, filepath.Join(c.Jobs.Options.DataDir, "diagnostics", replay), true)
	exists(t, filepath.Join(c.StoreDir, kept), true)
	*now = purge
	for range 2 {
		if err := c.Sweep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	exists(t, filepath.Join(c.Jobs.Options.DataDir, "diagnostics", replay), false)
	exists(t, filepath.Join(c.StoreDir, kept), false)
	exists(t, filepath.Join(c.Jobs.Options.DataDir, "logs", id+".log"), false)
	if err := c.Jobs.DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	for _, id := range []string{strings.Repeat("b", 64), strings.Repeat("c", 64)} {
		exists(t, filepath.Join(c.StoreDir, id), true)
		exists(t, filepath.Join(c.Jobs.Options.DataDir, "logs", id+".log"), true)
	}
	if err := c.Jobs.DB.QueryRow(`SELECT value FROM stat_counters`).Scan(&count); err != nil || count != 7 {
		t.Fatal(count, err)
	}
}

func TestCleanerOrphans(t *testing.T) {
	c, now := cleanerFixture(t)
	old, fresh := strings.Repeat("d", 64), strings.Repeat("e", 64)
	for _, id := range []string{old, fresh, ".replace-old", "rp_01ARZ3NDEKTSV4RRFFQ69G5FAV", "unknown"} {
		writeFile(t, filepath.Join(c.StoreDir, id, "image.bin"))
		mtime := now.Add(-time.Hour)
		if id == fresh {
			mtime = mtime.Add(time.Nanosecond)
		}
		if err := os.Chtimes(filepath.Join(c.StoreDir, id), mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	exists(t, filepath.Join(c.StoreDir, old), false)
	for _, id := range []string{fresh, ".replace-old", "rp_01ARZ3NDEKTSV4RRFFQ69G5FAV", "unknown"} {
		exists(t, filepath.Join(c.StoreDir, id), true)
	}
}

func request(router *gin.Engine, method, url string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(method, url, nil))
	return w
}

func TestDeleteStoreAndDownloadVisibility(t *testing.T) {
	c, now := cleanerFixture(t)
	id := strings.Repeat("a", 64)
	addJob(t, c, id, "done", now.Add(time.Hour), now.Add(2*time.Hour))
	router := gin.New()
	admin.RegisterStore(router.Group("/admin/api"), c)
	api.RegisterStore(router, c.Store, c.Jobs.StoreVisible)
	if w := request(router, "DELETE", "/admin/api/store/missing"); w.Code != 404 {
		t.Fatal(w)
	}
	for _, status := range []string{"queued", "running"} {
		if _, err := c.Jobs.DB.Exec(`UPDATE jobs SET status=? WHERE request_hash=?`, status, id); err != nil {
			t.Fatal(err)
		}
		if w := request(router, "DELETE", "/admin/api/store/"+id); w.Code != 409 {
			t.Fatal(w)
		}
		exists(t, filepath.Join(c.StoreDir, id), true)
	}
	if _, err := c.Jobs.DB.Exec(`UPDATE jobs SET status='done' WHERE request_hash=?`, id); err != nil {
		t.Fatal(err)
	}
	if w := request(router, "GET", "/store/"+id+"/image.bin"); w.Code != 200 {
		t.Fatal(w)
	}
	if w := request(router, "DELETE", "/admin/api/store/"+id); w.Code != 200 {
		t.Fatal(w)
	}
	exists(t, filepath.Join(c.StoreDir, id), false)
	exists(t, filepath.Join(c.Jobs.Options.DataDir, "logs", id+".log"), false)
	result, err := c.Jobs.Get(context.Background(), id)
	if err != nil || result.Status != 404 {
		t.Fatal(result, err)
	}
	if w := request(router, "GET", "/store/"+id+"/image.bin"); w.Code != 404 {
		t.Fatal(w)
	}
	var removed bool
	if err := c.Jobs.DB.QueryRow(`SELECT json_extract(result,'$.artifacts_removed') FROM jobs WHERE request_hash=?`, id).Scan(&removed); err != nil || !removed {
		t.Fatal(removed, err)
	}
	if w := request(router, "DELETE", "/admin/api/store/"+id); w.Code != 200 {
		t.Fatal(w)
	}
}

func TestRestartDownloadAndLogicalExpiry(t *testing.T) {
	dir := t.TempDir()
	first, err := db.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	backend := &store.Local{Dir: filepath.Join(dir, "store")}
	c := &queue.Cleaner{Jobs: queue.NewSQLite(first.DB, nil, nil, nil, queue.Options{DataDir: dir}), Store: backend, StoreDir: backend.Dir}
	id := strings.Repeat("f", 64)
	addJob(t, c, id, "done", now.Add(time.Hour), now.Add(2*time.Hour))
	// 关闭并重开数据库、Store 与路由，模拟进程重启而非复用内存结果。
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(context.Background(), c.Jobs.Options.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	c.Jobs = queue.NewSQLite(database.DB, nil, nil, nil, c.Jobs.Options)
	c.Store = &store.Local{Dir: c.StoreDir}
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api.RegisterStore(router, c.Store, c.Jobs.StoreVisible)
	for _, method := range []string{"GET", "HEAD"} {
		if w := request(router, method, "/store/"+id+"/image.bin"); w.Code != 200 {
			t.Fatal(w)
		}
	}
	if _, err := database.Exec(`UPDATE jobs SET expires_at=?`, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	// 未扫描前磁盘文件尚在，GET/HEAD 与公开轮询仍立即返回 404。
	exists(t, filepath.Join(c.StoreDir, id), true)
	for _, method := range []string{"GET", "HEAD"} {
		if w := request(router, method, "/store/"+id+"/image.bin"); w.Code != 404 {
			t.Fatal(w)
		}
	}
}

type failingDelete struct {
	store.Store
	id     string
	cancel context.CancelFunc
}

func (s failingDelete) Delete(ctx context.Context, id string) error {
	if id == s.id {
		return errors.New("模拟删除失败")
	}
	err := s.Store.Delete(ctx, id)
	if s.cancel != nil {
		s.cancel()
	}
	return err
}

func TestCleanerContinuesAndRunsAtStartup(t *testing.T) {
	c, now := cleanerFixture(t)
	bad, good := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, id := range []string{bad, good} {
		addJob(t, c, id, "done", now.Add(-time.Hour), now.Add(time.Hour))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Store = failingDelete{Store: c.Store, id: bad, cancel: cancel}
	core, logs := observer.New(zap.WarnLevel)
	c.Logger = zap.New(core)
	// 不等待 interval：首次成功删除即取消，Run 应正常退出。
	if err := c.Run(ctx); err != nil {
		t.Fatal(err)
	}
	exists(t, filepath.Join(c.StoreDir, bad), true)
	exists(t, filepath.Join(c.StoreDir, good), false)
	if logs.Len() == 0 {
		t.Fatal("删除失败应记录告警")
	}
	*now = now.Add(time.Hour)
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := c.Jobs.DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// 失败时保留记录作为重试依据；恢复后可幂等删除。
	c.Store = &store.Local{Dir: c.StoreDir}
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Jobs.DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestManualDeleteFailureIsRetried(t *testing.T) {
	c, now := cleanerFixture(t)
	id := "rp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	// 故意以非空目录占据日志路径，模拟单项日志删除失败。
	addJob(t, c, id, "failed", now.Add(time.Hour), now.Add(2*time.Hour))
	log := filepath.Join(c.Jobs.Options.DataDir, "logs", id+".log")
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(log, "blocked"))
	if _, err := c.Delete(context.Background(), id); err == nil {
		t.Fatal("应报告删除失败")
	}
	if err := os.Remove(filepath.Join(log, "blocked")); err != nil {
		t.Fatal(err)
	}
	// 即使是尚未到 purge_at 的重放，手动标记也让后续扫描重试清理。
	if err := c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	exists(t, log, false)
	var count int
	if err := c.Jobs.DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
