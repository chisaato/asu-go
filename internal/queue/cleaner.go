package queue

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	logging "github.com/chisaato/asu-go/internal/logcontext"
	"github.com/chisaato/asu-go/internal/store"
	"github.com/chisaato/asu-go/internal/stream"
	"go.uber.org/zap"
)

// Cleaner 与 Submit/claim/Complete 共用队列锁，避免清理旧结果时删除同 hash 的新构建。
// 放在 queue 包内是为了让这个边界覆盖文件系统操作，而非仅保护数据库更新。
type Cleaner struct {
	Jobs                  *SQLite
	Store                 store.Store
	StoreDir              string
	Interval, OrphanGrace time.Duration
	Now                   func() time.Time
	Logger                *zap.Logger
	PackageCache          interface{ Sweep(context.Context) error }
}

func (c *Cleaner) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Cleaner) warn(id string, err error) {
	if err != nil && c.Logger != nil {
		c.Logger.Warn("清理失败，将在下轮重试", zap.String("request_hash", id), zap.Error(err))
	}
}

// Run 启动立即扫描；单项失败不终止服务，取消由 serve 的 errgroup 传播。
func (c *Cleaner) Run(ctx context.Context) error {
	interval := c.Interval
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	c.warn("", c.Sweep(ctx))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.warn("", c.Sweep(ctx))
		}
	}
}

func (c *Cleaner) removeLog(id string) error {
	if err := stream.ValidateID(id); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Join(c.Jobs.Options.DataDir, "logs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	err = root.Remove(id + ".log")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (c *Cleaner) removeArtifacts(ctx context.Context, id string) error {
	if strings.HasPrefix(id, "rp_") {
		return (&store.Local{Dir: filepath.Join(c.Jobs.Options.DataDir, "diagnostics")}).Delete(ctx, id)
	}
	return c.Store.Delete(ctx, id)
}

func (c *Cleaner) Sweep(ctx context.Context) (err error) {
	if c.PackageCache != nil {
		c.warn("", c.PackageCache.Sweep(ctx))
	}
	c.Jobs.mu.Lock()
	defer c.Jobs.mu.Unlock()
	started := time.Now()
	removed := 0
	before, usageErr := c.usage(ctx)
	defer func() {
		after, afterErr := c.usage(ctx)
		fields := []zap.Field{zap.Int("removed", removed), zap.Int64("freed_bytes", max(before-after, 0)), zap.Bool("usage_available", usageErr == nil && afterErr == nil), zap.Duration("duration", time.Since(started))}
		log := logging.OrNop(c.Logger)
		if err != nil {
			log.Warn("TTL 清理未完成", append(fields, zap.Error(err))...)
		} else if removed > 0 {
			log.Info("TTL 清理完成", fields...)
		} else {
			log.Debug("TTL 清理完成", fields...)
		}
	}()
	now := c.now()
	rows, err := c.Jobs.DB.QueryContext(ctx, `SELECT request_hash,expires_at,purge_at,COALESCE(json_extract(options,'$.keep_container'),0),COALESCE(json_extract(result,'$.artifacts_removed'),0) FROM jobs WHERE status IN ('done','failed')`)
	if err != nil {
		return err
	}
	type candidate struct {
		id             string
		expires, purge sql.NullTime
		keep, removed  bool
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.expires, &item.purge, &item.keep, &item.removed); err != nil {
			_ = rows.Close()
			return err
		}
		candidates = append(candidates, item)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for _, item := range candidates {
		purge := item.purge.Valid && !now.Before(item.purge.Time)
		expired := item.expires.Valid && !now.Before(item.expires.Time)
		// 重放和 keep_container 的产物只按 purge_at 回收；手动删除可提前清理。
		if !purge && !item.removed && (!expired || item.keep || strings.HasPrefix(item.id, "rp_")) {
			continue
		}
		artifactDir := c.StoreDir
		if strings.HasPrefix(item.id, "rp_") {
			artifactDir = filepath.Join(c.Jobs.Options.DataDir, "diagnostics")
		}
		_, artifactErr := os.Lstat(filepath.Join(artifactDir, item.id))
		_, logErr := os.Lstat(filepath.Join(c.Jobs.Options.DataDir, "logs", item.id+".log"))
		hadFiles := artifactErr == nil || ((purge || item.removed) && logErr == nil)
		err := c.removeArtifacts(ctx, item.id)
		if purge || item.removed {
			err = errors.Join(err, c.removeLog(item.id))
		}
		if purge {
			// 先完成文件清理再删记录，失败时保留重试依据；统计表独立且不修改。
			if err == nil {
				_, err = c.Jobs.DB.ExecContext(ctx, `DELETE FROM jobs WHERE request_hash=? AND status IN ('done','failed')`, item.id)
			}
		}
		c.warn(item.id, err)
		if err == nil && (purge || hadFiles) {
			removed++
		}
	}
	orphanRemoved, err := c.orphans(ctx, now)
	removed += orphanRemoved
	return err
}

func (c *Cleaner) usage(ctx context.Context) (int64, error) {
	public, err := c.Store.Usage(ctx)
	diagnostics, diagnosticsErr := (&store.Local{Dir: filepath.Join(c.Jobs.Options.DataDir, "diagnostics")}).Usage(ctx)
	logs, logsErr := (&store.Local{Dir: filepath.Join(c.Jobs.Options.DataDir, "logs")}).Usage(ctx)
	return public + diagnostics + logs, errors.Join(err, diagnosticsErr, logsErr)
}

func (c *Cleaner) orphans(ctx context.Context, now time.Time) (int, error) {
	removed := 0
	entries, err := os.ReadDir(c.StoreDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	grace := c.OrphanGrace
	if grace <= 0 {
		grace = time.Hour
	}
	for _, entry := range entries {
		id := entry.Name()
		// 仅识别公开 SHA-256 目录，不碰 rp_、.replace-*、符号链接及未知内部文件。
		if !entry.IsDir() || len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			c.warn(id, err)
			continue
		}
		if now.Before(info.ModTime().Add(grace)) {
			continue
		}
		var exists int
		err = c.Jobs.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE request_hash=?`, id).Scan(&exists)
		if err == nil && exists == 0 {
			err = c.Store.Delete(ctx, id)
			if err == nil {
				removed++
			}
		}
		c.warn(id, err)
	}
	return removed, nil
}

// Delete 保留诊断记录，将结果标记并使公开轮询立即与过期一致（404）。
func (c *Cleaner) Delete(ctx context.Context, id string) (Response, error) {
	c.Jobs.mu.Lock()
	defer c.Jobs.mu.Unlock()
	var status string
	err := c.Jobs.DB.QueryRowContext(ctx, `SELECT status FROM jobs WHERE request_hash=?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return failure(404, "job not found"), nil
	}
	if err != nil {
		return Response{}, err
	}
	if status == "queued" || status == "running" {
		return failure(409, "job still active"), nil
	}
	// 先撤销可见性；即使磁盘删除失败，也不能继续向客户端暴露已撤销的产物。
	_, err = c.Jobs.DB.ExecContext(ctx, `UPDATE jobs SET expires_at=?,result=json_set(CASE WHEN json_type(result)='object' THEN result ELSE '{}' END,'$.artifacts_removed',json('true')) WHERE request_hash=?`, c.now(), id)
	if err != nil {
		return Response{}, err
	}
	err = errors.Join(c.removeArtifacts(ctx, id), c.removeLog(id))
	c.warn(id, err)
	return Response{Status: 200, Body: map[string]any{"id": id, "artifacts_removed": true}}, err
}

// StoreVisible 用于每次下载的逻辑 TTL 检查，不依赖 Cleaner 的扫描间隔。
func (q *SQLite) StoreVisible(ctx context.Context, id string) (bool, error) {
	if strings.HasPrefix(id, "rp_") {
		return false, nil
	}
	var n int
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE request_hash=? AND status='done' AND (expires_at IS NULL OR expires_at>?)`, id, time.Now().UTC()).Scan(&n)
	return n == 1, err
}
