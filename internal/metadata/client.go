// Package metadata 同步和读取上游发行元数据。
package metadata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Response struct {
	Body         []byte
	Status       int
	ETag         string
	LastModified string
}

// Client 为后续磁盘缓存提供同一条件请求入口，不缓存失败响应。
type Client struct {
	HTTP      *http.Client
	UserAgent string
	mu        sync.Mutex
	cache     map[string]Response
}

func NewClient(userAgent string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, UserAgent: userAgent, cache: map[string]Response{}}
}

// Fetch 支持显式传入磁盘缓存的验证器；304 的 Body 由缓存调用方补足。
func (c *Client) Fetch(ctx context.Context, url, etag, modified string) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if modified != "" {
		req.Header.Set("If-Modified-Since", modified)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = res.Body.Close() }()
	const maxBody = 32 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return Response{}, err
	}
	if len(body) > maxBody {
		return Response{}, fmt.Errorf("上游响应超过 %d 字节", maxBody)
	}
	return Response{body, res.StatusCode, res.Header.Get("ETag"), res.Header.Get("Last-Modified")}, nil
}

// Get 对同一 URL 重新验证；304 复用成功响应，网络失败留给业务层回退旧数据。
func (c *Client) Get(ctx context.Context, url string) (Response, error) {
	c.mu.Lock()
	old, exists := c.cache[url]
	c.mu.Unlock()
	res, err := c.Fetch(ctx, url, old.ETag, old.LastModified)
	if err != nil {
		return Response{}, err
	}
	if res.Status == http.StatusNotModified {
		if !exists {
			return Response{}, fmt.Errorf("上游无缓存却返回 304: %s", url)
		}
		return old, nil
	}
	if res.Status == http.StatusOK {
		c.mu.Lock()
		c.cache[url] = res
		c.mu.Unlock()
	}
	return res, nil
}
