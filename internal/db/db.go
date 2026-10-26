// Package db 管理 SQLite 的单进程所有权与内嵌迁移。
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

const Filename = "asu.sqlite"

type DB struct {
	*sql.DB
	lock *os.File
}

// Open 持有同目录 advisory flock 直到 Close；serve 与离线迁移共用此入口。
// 锁文件不能删除，否则其他进程可能锁住不同 inode。仅支持本地 Unix 文件系统，
// 不防御绕过本入口直接打开 SQLite 的程序，也不支持网络文件系统。
func Open(ctx context.Context, dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 data_dir: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(dataDir, Filename+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开数据库锁: %w", err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("数据库已被占用，db migrate 要求服务未运行: %w", err)
	}
	conn, err := sql.Open("sqlite", filepath.Join(dataDir, Filename))
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}
	db := &DB{DB: conn, lock: lock}
	// 单连接保证每次访问沿用本连接的 PRAGMA，并串行化写操作。
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 5000; PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 SQLite: %w", err)
	}
	return db, nil
}

func (d *DB) Migrate(ctx context.Context) error {
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("读取内嵌迁移: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.DB, migrationFS)
	if err != nil {
		return fmt.Errorf("创建迁移器: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("迁移数据库: %w", err)
	}
	return nil
}

func (d *DB) Close() error {
	// 先关闭连接，后释放文件锁。
	return errors.Join(d.DB.Close(), d.lock.Close())
}
