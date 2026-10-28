package sources

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/chisaato/asu-go/internal/db"
)

//go:embed defaults.yaml
var defaults []byte

var ErrInvalid = errors.New("无效来源")
var ErrConflict = errors.New("来源已存在")

var writable = map[string]bool{
	"distribution": true, "branch": true, "version": true, "channel": true,
	"upstream_url": true, "image": true, "tag_template": true, "package_manager": true,
	"origin": true, "rolling": true, "enabled": true,
}

type Store struct{ db *db.DB }

func NewStore(database *db.DB) *Store { return &Store{db: database} }

const columns = `id, distribution, branch, version, COALESCE(channel,''), upstream_url, image, tag_template, package_manager, origin, rolling, enabled, overrides, targets, synced_at`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Source, error) {
	s := NewSource()
	var overrides, targets string
	err := row.Scan(&s.ID, &s.Distribution, &s.Branch, &s.Version, &s.Channel, &s.UpstreamURL, &s.Image, &s.TagTemplate, &s.PackageManager, &s.Origin, &s.Rolling, &s.Enabled, &overrides, &targets, &s.SyncedAt)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal([]byte(overrides), &s.Overrides); err != nil {
		return s, err
	}
	if err := json.Unmarshal([]byte(targets), &s.Targets); err != nil {
		return s, err
	}
	return s, nil
}

func (s *Store) List(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM sources ORDER BY distribution, version")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []Source{}
	for rows.Next() {
		source, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	return result, rows.Err()
}

func (s *Store) Get(ctx context.Context, id int64) (Source, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM sources WHERE id=?", id))
}

// SeedDefaults 只在目录为空时初始化，事务保证失败时不留下半份目录。
func (s *Store) SeedDefaults(ctx context.Context) error {
	catalog, err := DecodeCatalog(bytes.NewReader(defaults))
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sources").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	for _, source := range catalog {
		if _, err := save(ctx, tx, source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func changed(before, after Source) []string {
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	var left, right map[string]json.RawMessage
	_ = json.Unmarshal(a, &left)
	_ = json.Unmarshal(b, &right)
	set := map[string]bool{}
	for _, field := range append(before.Overrides, after.Overrides...) {
		set[field] = true
	}
	for field := range writable {
		if !bytes.Equal(left[field], right[field]) {
			set[field] = true
		}
	}
	fields := make([]string, 0, len(set))
	for field := range set {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func save(ctx context.Context, tx *sql.Tx, source Source) (Source, error) {
	if err := source.Validate(); err != nil {
		return Source{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if source.Overrides == nil {
		source.Overrides = []string{}
	}
	if source.Targets == nil {
		source.Targets = map[string]any{}
	}
	overrides, err := json.Marshal(source.Overrides)
	if err != nil {
		return Source{}, err
	}
	targets, err := json.Marshal(source.Targets)
	if err != nil {
		return Source{}, err
	}
	args := []any{source.Distribution, source.Branch, source.Version, source.Channel, source.UpstreamURL, source.Image, source.TagTemplate, source.PackageManager, source.Origin, source.Rolling, source.Enabled, string(overrides), string(targets), source.SyncedAt}
	var result sql.Result
	if source.ID == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO sources (distribution,branch,version,channel,upstream_url,image,tag_template,package_manager,origin,rolling,enabled,overrides,targets,synced_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	} else {
		args = append(args, source.ID)
		result, err = tx.ExecContext(ctx, `UPDATE sources SET distribution=?,branch=?,version=?,channel=?,upstream_url=?,image=?,tag_template=?,package_manager=?,origin=?,rolling=?,enabled=?,overrides=?,targets=?,synced_at=? WHERE id=?`, args...)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Source{}, ErrConflict
		}
		return Source{}, err
	}
	if source.ID == 0 {
		source.ID, err = result.LastInsertId()
	}
	return source, err
}

func (s *Store) Create(ctx context.Context, source Source) (Source, error) {
	source.ID = 0
	source.Overrides = changed(NewSource(), source)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer func() { _ = tx.Rollback() }()
	created, err := save(ctx, tx, source)
	if err != nil {
		return Source{}, err
	}
	return created, tx.Commit()
}

func (s *Store) Patch(ctx context.Context, id int64, patch map[string]json.RawMessage) (Source, error) {
	for field, value := range patch {
		if !writable[field] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Source{}, fmt.Errorf("%w: 不可修改字段 %q 或 null 值", ErrInvalid, field)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer func() { _ = tx.Rollback() }()
	before, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM sources WHERE id=?", id))
	if err != nil {
		return Source{}, err
	}
	after := before
	data, err := json.Marshal(patch)
	if err != nil {
		return Source{}, err
	}
	if err := json.Unmarshal(data, &after); err != nil {
		return Source{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	after.Overrides = changed(before, after)
	after, err = save(ctx, tx, after)
	if err != nil {
		return Source{}, err
	}
	return after, tx.Commit()
}

// Import 是管理员主动 upsert；同步器应另行按 Overrides 跳过本地字段。
// 忽略外部 ID，按 (distribution, version) 匹配，整个批次原子提交。
func (s *Store) Import(ctx context.Context, catalog []Source) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, source := range catalog {
		before, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM sources WHERE distribution=? AND version=?", source.Distribution, source.Version))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			source.ID = 0
		} else {
			source.ID = before.ID
			source.Overrides = changed(before, source)
		}
		if _, err := save(ctx, tx, source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM sources WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

// PruneObsolete 清理不符合当前保留规则的 upstream 来源（删除 SNAPSHOT、-rc 以及非 24/25 的旧版本）。
func (s *Store) PruneObsolete(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM sources 
		WHERE origin = 'upstream' 
		  AND (
			version LIKE '%SNAPSHOT%' 
			OR version LIKE '%-rc%' 
			OR (version NOT LIKE '24.%' AND version NOT LIKE '25.%')
		  )
	`)
	return err
}

