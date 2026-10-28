package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	logging "github.com/chisaato/asu-go/internal/logcontext"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// Cache 将原始上游资源保存在 data_dir/metadata/<upstream hash>/<规范路径>。
// TTL 为零时每次重新验证，适合测试；生产配置要求正数。
type Cache struct {
	Dir    string
	TTL    time.Duration
	Client *Client
	Logger *zap.Logger
	group  singleflight.Group
}

type cacheMeta struct {
	ETag         string    `json:"etag"`
	LastModified string    `json:"last_modified"`
	FetchedAt    time.Time `json:"fetched_at"`
	SHA256       string    `json:"sha256"`
}

func safePath(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, r := range segment {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._+-", r) {
				return false
			}
		}
	}
	return true
}

func atomicFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".metadata-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (c *Cache) warn(message string, err error, url string) {
	if c.Logger != nil {
		c.Logger.Warn(message, zap.String("url", url), zap.Error(err))
	}
}

func (c *Cache) get(ctx context.Context, upstream, path string) ([]byte, error) {
	if !safePath(path) {
		return nil, &RevisionError{400, "Invalid metadata path"}
	}
	upstream = strings.TrimRight(upstream, "/")
	key := fmt.Sprintf("%x/%s", sha256.Sum256([]byte(upstream)), path)
	result := c.group.DoChan(key, func() (any, error) {
		file := filepath.Join(c.Dir, "metadata", filepath.FromSlash(key))
		body, readErr := os.ReadFile(file)
		var meta cacheMeta
		sidecar, metaErr := os.ReadFile(file + ".meta.json")
		valid := readErr == nil && metaErr == nil && json.Unmarshal(sidecar, &meta) == nil && meta.SHA256 == fmt.Sprintf("%x", sha256.Sum256(body))
		if valid && time.Since(meta.FetchedAt) < c.TTL {
			logging.OrNop(c.Logger).Debug("元数据缓存命中", zap.String("path", path))
			return body, nil
		}
		if !valid {
			meta = cacheMeta{}
		}
		url := upstream + "/" + path
		res, err := c.Client.Fetch(ctx, url, meta.ETag, meta.LastModified)
		logging.OrNop(c.Logger).Debug("元数据拉取完成", zap.String("path", path), zap.Int("status", res.Status), zap.Error(err))
		if err == nil {
			switch res.Status {
			case 200:
				if strings.HasSuffix(path, ".json") && !json.Valid(res.Body) {
					err = fmt.Errorf("上游 JSON 无效: %s", url)
				} else {
					body = res.Body
				}
			case 304:
				if !valid {
					err = fmt.Errorf("无缓存却返回 304: %s", url)
				}
			default:
				err = &RevisionError{res.Status, fmt.Sprintf("Failed to fetch metadata: %s", path)}
			}
		}
		if err != nil {
			if valid {
				c.warn("上游元数据失败，使用过期缓存", err, url)
				return body, nil
			}
			return nil, err
		}
		if res.Status == 200 {
			meta.ETag, meta.LastModified = res.ETag, res.LastModified
		} else {
			if res.ETag != "" {
				meta.ETag = res.ETag
			}
			if res.LastModified != "" {
				meta.LastModified = res.LastModified
			}
		}
		meta.FetchedAt = time.Now().UTC()
		meta.SHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
		encoded, err := json.Marshal(meta)
		if err != nil {
			return nil, err
		}
		if res.Status == 200 {
			err = atomicFile(file, body)
		}
		if err == nil {
			err = atomicFile(file+".meta.json", encoded)
		}
		if err != nil {
			c.warn("元数据缓存写入失败", err, url)
		}
		logging.OrNop(c.Logger).Info("元数据缓存已刷新", zap.String("path", path), zap.Int("status", res.Status), zap.Int("bytes", len(body)))
		return body, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.([]byte), nil
	}
}
