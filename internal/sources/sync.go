package sources

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SyncUpstream 在写事务中重新读取 overrides，避免同步期间的管理员编辑丢失。
// 每个版本及其 targets 原子更新；失败的上游请求不应调用本方法。
func (s *Store) SyncUpstream(ctx context.Context, incoming Source) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	before, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM sources WHERE distribution=? AND version=?", incoming.Distribution, incoming.Version))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if before.Origin == "local" {
			return nil
		}
		data, err := json.Marshal(before)
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		keep := map[string]json.RawMessage{}
		for _, field := range before.Overrides {
			keep[field] = fields[field]
		}
		data, err = json.Marshal(keep)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &incoming); err != nil {
			return err
		}
		incoming.ID, incoming.Overrides = before.ID, before.Overrides
	} else {
		incoming.ID, incoming.Overrides = 0, []string{}
	}
	if _, err := save(ctx, tx, incoming); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordSync 只记录已完成一轮同步的状态；部分失败也可由调用方明确记录。
func (s *Store) RecordSync(ctx context.Context, at time.Time, failures int) error {
	value, err := json.Marshal(struct {
		At       time.Time `json:"at"`
		Failures int       `json:"failures"`
	}{at, failures})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('metadata.last_sync',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(value))
	return err
}

// Defaults 返回独立的内嵌目录副本，用于新增版本继承发行版镜像配置。
func Defaults() ([]Source, error) { return DecodeCatalog(bytes.NewReader(defaults)) }
