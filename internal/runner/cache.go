package runner

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	logging "github.com/chisaato/asu-go/internal/logcontext"
	"github.com/chisaato/asu-go/internal/runtime"
	"go.uber.org/zap"
)

// PackageCache 只共享不可变包文件；索引始终由任务独立下载。
// 一个服务实例共用同一对象，播种、回收和淘汰使用同一把锁。
type PackageCache struct {
	Enabled  bool
	Dir      string
	MaxBytes int64
	Logger   *zap.Logger
	mu       sync.Mutex
}

func cacheKey(image string) string {
	image, _, _ = strings.Cut(image, "@")
	var b strings.Builder
	for _, c := range image {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	key := b.String()
	if key == "" || key == "." || key == ".." {
		return "_"
	}
	return key
}

func packageName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "/\\\x00") && !strings.Contains(name, "..") && (strings.HasSuffix(name, ".apk") || strings.HasSuffix(name, ".ipk"))
}

func (c *PackageCache) usable(j Job) bool {
	reason := ""
	if c == nil {
		return false
	}
	if !c.Enabled {
		reason = "disabled"
	} else if len(j.Repositories) > 0 {
		reason = "custom_repos"
	} else if j.Image == "" {
		reason = "unknown_image"
	}
	if reason != "" {
		logging.OrNop(c.Logger).Debug("跳过包缓存", zap.String("component", "runner"), zap.String("request_hash", j.ID), zap.String("reason", reason))
		return false
	}
	return true
}

func (c *PackageCache) seed(ctx context.Context, rt runtime.Runtime, h runtime.Handle, j Job) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	dir := filepath.Join(c.Dir, cacheKey(j.Image))
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// 在磁盘上暂存归档，避免缓存增长时占用同等大小的堆内存。
	f, err := os.CreateTemp("", "asu-cache-*.tar")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	tw := tar.NewWriter(f)
	if err := tw.WriteHeader(&tar.Header{Name: "dl/", Typeflag: tar.TypeDir, Mode: 0755, Uid: j.inputUID, Gid: j.inputGID}); err != nil {
		return err
	}
	count, size := 0, int64(0)
	var seeded []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !packageName(entry.Name()) || !info.Mode().IsRegular() || info.Size() <= 0 {
			continue
		}
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		err = tw.WriteHeader(&tar.Header{Name: "dl/" + entry.Name(), Mode: 0644, Size: info.Size(), Uid: j.inputUID, Gid: j.inputGID})
		if err == nil {
			_, err = io.CopyN(tw, file, info.Size())
		}
		err = errors.Join(err, file.Close())
		if err != nil {
			return err
		}
		count++
		size += info.Size()
		seeded = append(seeded, entry.Name())
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := rt.PutArchive(ctx, h, workingDir(j.WorkingDir), f); err != nil {
		return err
	}
	now := time.Now()
	for _, name := range seeded {
		if err := os.Chtimes(filepath.Join(dir, name), now, now); err != nil {
			return err
		}
	}
	logging.OrNop(c.Logger).Info("包缓存播种完成", zap.String("component", "runner"), zap.String("request_hash", j.ID), zap.Int("packages", count), zap.Int64("bytes", size))
	return nil
}

func (c *PackageCache) collect(ctx context.Context, rt runtime.Runtime, h runtime.Handle, j Job) error {
	r, err := rt.GetArchive(ctx, h, path.Join(workingDir(j.WorkingDir), "dl"))
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	c.mu.Lock()
	defer c.mu.Unlock()
	dir := filepath.Join(c.Dir, cacheKey(j.Image))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tr := tar.NewReader(r)
	count, size := 0, int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		// 引擎会添加一个 dl/ 根目录；不接受嵌套目录或路径清洗后的穿越路径。
		name := strings.TrimPrefix(hdr.Name, "dl/")
		if hdr.Typeflag != tar.TypeReg || hdr.Size <= 0 || !packageName(name) {
			continue
		}
		dest := filepath.Join(dir, name)
		if _, err := os.Lstat(dest); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := publishPackage(dir, dest, tr, hdr.Size); err != nil {
			return err
		}
		count++
		size += hdr.Size
	}
	logging.OrNop(c.Logger).Info("包缓存回收完成", zap.String("component", "runner"), zap.String("request_hash", j.ID), zap.Int("packages", count), zap.Int64("bytes", size))
	return nil
}

func publishPackage(dir, dest string, r io.Reader, size int64) error {
	f, err := os.CreateTemp(dir, ".package-*")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err := io.CopyN(f, r, size); err != nil {
		return err
	}
	if err := f.Chmod(0644); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dest)
}

// Sweep 按文件 mtime 淘汰；零容量表示不保留包，而不是无限容量。
func (c *PackageCache) Sweep(ctx context.Context) error {
	if c == nil || !c.Enabled {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	type candidate struct {
		name string
		info fs.FileInfo
	}
	var files []candidate
	var total int64
	err := filepath.WalkDir(c.Dir, func(name string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !packageName(d.Name()) || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, candidate{name, info})
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().Before(files[j].info.ModTime()) })
	count, size := 0, int64(0)
	for _, f := range files {
		if total <= c.MaxBytes {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(f.name); err != nil {
			return err
		}
		total -= f.info.Size()
		count++
		size += f.info.Size()
	}
	logging.OrNop(c.Logger).Info("包缓存淘汰完成", zap.String("component", "cleaner"), zap.Int("removed", count), zap.Int64("bytes", size))
	return nil
}
