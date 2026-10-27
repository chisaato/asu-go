package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/gzzchh/asu-go/internal/runtime"
	"go.uber.org/zap"
)

func inputs(j Job) ([]byte, error) {
	files := map[string]string{}
	if j.Defaults != "" {
		files["asu-files/etc/uci-defaults/99-asu-defaults"] = j.Defaults
	}
	for i, key := range j.RepositoryKeys {
		name := fmt.Sprintf("keys/custom-%d.pem", i)
		if !strings.HasPrefix(strings.TrimSpace(key), "-----BEGIN") {
			lines := strings.Split(strings.TrimSpace(key), "\n")
			decoded, err := base64.StdEncoding.DecodeString(lines[len(lines)-1])
			if err != nil || len(decoded) < 10 {
				return nil, fmt.Errorf("无效 usign 公钥")
			}
			fingerprint := fmt.Sprintf("%x", decoded[2:10])
			name = "keys/" + fingerprint
			key = "untrusted comment: " + fingerprint + "\n" + key
		}
		files[name] = key
	}
	if len(j.Repositories) > 0 {
		names := make([]string, 0, len(j.Repositories))
		for name := range j.Repositories {
			names = append(names, name)
		}
		sort.Strings(names)
		var apk, opkg strings.Builder
		for _, name := range names {
			url := j.Repositories[name]
			if strings.ContainsAny(name+url, "\n\r\x00") {
				return nil, fmt.Errorf("无效仓库配置")
			}
			fmt.Fprintln(&apk, url)
			fmt.Fprintf(&opkg, "src/gz %s %s\n", name, url)
		}
		files[".asu-repositories-apk"] = apk.String()
		files[".asu-repositories-opkg"] = opkg.String()
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// 显式写入父目录，以兼容 Docker/Podman archive 解压行为。
	dirs := map[string]bool{}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parts := strings.Split(path.Dir(name), "/")
		prefix := ""
		for _, part := range parts {
			if part == "." {
				continue
			}
			prefix = path.Join(prefix, part)
			if !dirs[prefix] {
				if err := tw.WriteHeader(&tar.Header{Name: prefix + "/", Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
					return nil, err
				}
				dirs[prefix] = true
			}
		}
		data := files[name]
		mode := int64(0644)
		if strings.HasPrefix(name, "asu-files/") {
			mode = 0755
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
			return nil, err
		}
		if _, err := io.WriteString(tw, data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func extract(ctx context.Context, rt runtime.Runtime, h runtime.Handle, source, dest string, loggers ...*zap.Logger) (err error) {
	log := zap.NewNop()
	if len(loggers) > 0 && loggers[0] != nil {
		log = loggers[0]
	}
	started := time.Now()
	files, bytes := 0, int64(0)
	defer func() {
		fields := []zap.Field{zap.Int("files", files), zap.Int64("bytes", bytes), zap.Duration("duration", time.Since(started))}
		if err != nil {
			log.Error("产物提取失败", append(fields, zap.Error(err))...)
		} else {
			log.Info("产物提取完成", fields...)
		}
	}()
	r, err := rt.GetArchive(ctx, h, source)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	if err = os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	tr := tar.NewReader(r)
	base := path.Base(source)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		hdr, readErr := tr.Next()
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		name := hdr.Name
		if path.IsAbs(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("不安全产物路径: %s", name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return fmt.Errorf("不安全产物路径: %s", name)
			}
		}
		name = path.Clean(name)
		if name == base || name == "." {
			if hdr.Typeflag != tar.TypeDir {
				return fmt.Errorf("无效产物根目录")
			}
			continue
		}
		name = strings.TrimPrefix(name, base+"/")
		if hdr.Typeflag != tar.TypeDir && hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("产物不允许链接或特殊文件: %s", name)
		}
		if fi, e := root.Lstat(name); e == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("产物目标是链接: %s", name)
		}
		if hdr.Typeflag == tar.TypeDir {
			if err = root.MkdirAll(name, 0755); err != nil {
				return err
			}
			continue
		}
		if err = root.MkdirAll(path.Dir(name), 0755); err != nil {
			return err
		}
		f, e := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if e != nil {
			return e
		}
		n, copyErr := io.Copy(f, tr)
		bytes += n
		chmodErr := f.Chmod(0644)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if chmodErr != nil {
			return chmodErr
		}
		if closeErr != nil {
			return closeErr
		}
		files++
	}
}
