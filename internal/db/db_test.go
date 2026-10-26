package db

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMigrations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	for range 2 {
		if err := database.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, Filename)); err != nil {
		t.Fatal(err)
	}
	for pragma, want := range map[string]string{"journal_mode": "wal", "busy_timeout": "5000", "foreign_keys": "1"} {
		var got string
		if err := database.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %q, want %q: %v", pragma, got, want, err)
		}
	}
	for table, want := range map[string][]string{
		"sources":       {"id", "distribution", "branch", "version", "channel", "upstream_url", "image", "tag_template", "package_manager", "origin", "rolling", "enabled", "overrides", "targets", "synced_at"},
		"jobs":          {"request_hash", "replay_of", "source_id", "version", "target", "profile", "status", "stage", "enqueued_at", "started_at", "finished_at", "expires_at", "purge_at", "detail", "error", "client", "request", "result", "handle", "stages", "options"},
		"stat_counters": {"day", "metric", "dim", "value"},
		"kv":            {"key", "value"},
	} {
		rows, err := database.QueryContext(ctx, "SELECT * FROM "+table+" LIMIT 0")
		if err != nil {
			t.Fatal(err)
		}
		got, err := rows.Columns()
		_ = rows.Close()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s columns = %v, want %v: %v", table, got, want, err)
		}
	}
	for _, index := range []string{"jobs_version_target_profile", "jobs_status", "jobs_expires_at", "jobs_purge_at", "jobs_replay_of"} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&count); err != nil || count != 1 {
			t.Fatalf("缺少索引 %s: %v", index, err)
		}
	}
	var versions int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id=1 AND is_applied=1").Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("重复迁移: count=%d, %v", versions, err)
	}
	// 验证 source 唯一性及删除时任务外键置空。
	insert := "INSERT INTO sources (id,distribution,branch,version,upstream_url,image,tag_template,package_manager,origin) VALUES (1,'openwrt','24.10','24.10.1','url','image','tag','opkg','upstream')"
	if _, err := database.ExecContext(ctx, insert); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, strings.Replace(insert, "(1,", "(2,", 1)); err == nil {
		t.Fatal("未拒绝重复 distribution/version")
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO jobs (request_hash,source_id,version,target,profile,status,enqueued_at,request) VALUES ('hash',1,'24.10.1','target','profile','queued',CURRENT_TIMESTAMP,'{}'); DELETE FROM sources WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	var nullSource bool
	if err := database.QueryRowContext(ctx, "SELECT source_id IS NULL FROM jobs WHERE request_hash='hash'").Scan(&nullSource); err != nil || !nullSource {
		t.Fatalf("ON DELETE SET NULL 未生效: %v", err)
	}
}

func TestExclusiveOwnership(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	if second, err := Open(ctx, dir); err == nil {
		_ = second.Close()
		t.Fatal("未拒绝并发所有者")
	} else if !strings.Contains(err.Error(), "服务未运行") {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("关闭后锁未释放: %v", err)
	}
	defer func() { _ = second.Close() }()
	if err := second.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}
