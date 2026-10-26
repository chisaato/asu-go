package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// File 是可用于 Range 下载的文件视图，调用方负责关闭。
type File interface {
	io.ReadSeekCloser
	Stat() (fs.FileInfo, error)
}

// Store 是发布、下载与 Cleaner 共用的边界。Publish 成功后接管源目录。
// Delete 幂等；Usage 返回普通文件的逻辑字节数，不跟随符号链接。
type Store interface {
	Publish(context.Context, string, string) error
	Open(context.Context, string, string) (File, error)
	Stat(context.Context, string, string) (fs.FileInfo, error)
	Delete(context.Context, string) error
	Usage(context.Context) (int64, error)
}

var validHash = regexp.MustCompile(`^(?:[0-9a-f]{64}|rp_[0-7][0-9A-HJKMNP-TV-Z]{25})$`)

// ValidateHash 接受 SHA-256 或 rp_ 加规范大写 ULID（128 位）。
func ValidateHash(hash string) error {
	if !validHash.MatchString(hash) {
		return fs.ErrNotExist
	}
	return nil
}

func validateFile(name string) error {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\x00") {
		return fs.ErrNotExist
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return fs.ErrNotExist
		}
	}
	return nil
}

// Local 串行化目录切换，使读取者看不到替换间隙；os.Root 限制符号链接边界。
type Local struct {
	Dir string
	mu  sync.Mutex
}

func (s *Local) Publish(ctx context.Context, hash, srcDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateHash(hash); err != nil {
		return err
	}
	info, err := os.Lstat(srcDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("产物源必须为目录")
	}
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return err
	}
	dst := filepath.Join(s.Dir, hash)
	backup, err := os.MkdirTemp(s.Dir, ".replace-")
	if err != nil {
		return err
	}
	removeBackup := true
	defer func() {
		if removeBackup {
			_ = os.RemoveAll(backup)
		}
	}()
	old := filepath.Join(backup, "old")
	err = os.Rename(dst, old)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err = os.Rename(srcDir, dst); err != nil {
		if restoreErr := os.Rename(old, dst); restoreErr != nil && !errors.Is(restoreErr, fs.ErrNotExist) {
			removeBackup = false
			return errors.Join(err, fmt.Errorf("恢复旧产物（保留于 %s）: %w", old, restoreErr))
		}
		return err
	}
	return nil
}

func (s *Local) Open(ctx context.Context, hash, name string) (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateHash(hash); err != nil {
		return nil, err
	}
	if err := validateFile(name); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	job, err := root.OpenRoot(hash)
	if err != nil {
		return nil, err
	}
	defer func() { _ = job.Close() }()
	// 拒绝链接别名，防止借公开名称读取隐藏文件；os.Root 另保证并发替换无法逃逸。
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := job.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return nil, fs.ErrNotExist
		}
	}
	file, err := job.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fs.ErrNotExist
	}
	return file, nil
}

func (s *Local) Stat(ctx context.Context, hash, name string) (fs.FileInfo, error) {
	f, err := s.Open(ctx, hash, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return f.Stat()
}

func (s *Local) Delete(ctx context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateHash(hash); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.RemoveAll(hash)
}

func (s *Local) Usage(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := os.OpenRoot(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	var size int64
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}
