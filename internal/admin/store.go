package admin

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/queue"
)

type StorageOverviewResponse struct {
	Artifacts struct {
		Count int   `json:"count"`
		Bytes int64 `json:"bytes"`
	} `json:"artifacts"`
	Orphans struct {
		Count int   `json:"count"`
		Bytes int64 `json:"bytes"`
	} `json:"orphans"`
	TTL       map[string]string `json:"ttl"`
	LastClean *CleanHistory     `json:"last_clean"`
}

type CleanHistory struct {
	At         time.Time `json:"at"`
	Removed    int       `json:"removed"`
	FreedBytes int64     `json:"freed_bytes"`
}

type ArtifactItem struct {
	RequestHash string    `json:"request_hash"`
	Distro      string    `json:"distro"`
	Version     string    `json:"version"`
	Target      string    `json:"target"`
	Profile     string    `json:"profile"`
	Bytes       int64     `json:"bytes"`
	Files       []string  `json:"files"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type CleanRequest struct {
	Scope  string `json:"scope"`
	DryRun bool   `json:"dry_run"`
}

type CleanResponse struct {
	Items int   `json:"items"`
	Bytes int64 `json:"bytes"`
}

// RegisterStore 挂载存储管理与清理路由。
func RegisterStore(group *gin.RouterGroup, cleaner *queue.Cleaner) {
	group.DELETE("/store/:hash", func(c *gin.Context) {
		response, err := cleaner.Delete(c.Request.Context(), c.Param("hash"))
		if err != nil {
			c.JSON(500, gin.H{"error": "internal error"})
			return
		}
		c.JSON(response.Status, response.Body)
	})

	group.GET("/storage", func(c *gin.Context) {
		ctx := c.Request.Context()
		res := StorageOverviewResponse{
			TTL: map[string]string{
				"defaults":    cleaner.Jobs.Options.BuildDefaultsTTL.String(),
				"versioned":   cleaner.Jobs.Options.BuildTTL.String(),
				"unversioned": cleaner.Jobs.Options.BuildUnversionedTTL.String(),
				"failed":      cleaner.Jobs.Options.BuildFailureTTL.String(),
				"purge_at":    cleaner.Jobs.Options.Retention.String(),
			},
		}

		// 扫描 store 目录统计真实产物文件与孤儿
		entries, err := os.ReadDir(cleaner.StoreDir)
		if err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if !entry.IsDir() {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					continue
				}
				dirBytes := dirSize(filepath.Join(cleaner.StoreDir, name))

				var exists int
				_ = cleaner.Jobs.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE request_hash=?`, name).Scan(&exists)
				if exists > 0 {
					res.Artifacts.Count++
					res.Artifacts.Bytes += dirBytes
				} else if info.ModTime().Add(cleaner.OrphanGrace).Before(time.Now()) {
					res.Orphans.Count++
					res.Orphans.Bytes += dirBytes
				}
			}
		}

		c.JSON(http.StatusOK, res)
	})

	group.GET("/storage/artifacts", func(c *gin.Context) {
		ctx := c.Request.Context()
		rows, err := cleaner.Jobs.DB.QueryContext(ctx, `SELECT request_hash,version,target,profile,enqueued_at,expires_at FROM jobs WHERE status='done' ORDER BY enqueued_at DESC LIMIT 100`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		defer func() { _ = rows.Close() }()

		var items []ArtifactItem
		for rows.Next() {
			var it ArtifactItem
			var expires sql.NullTime
			if err := rows.Scan(&it.RequestHash, &it.Version, &it.Target, &it.Profile, &it.CreatedAt, &expires); err != nil {
				continue
			}
			if expires.Valid {
				it.ExpiresAt = &expires.Time
			}
			it.Distro = "openwrt"

			// 读取实际生成的文件列表与大小
			dirPath := filepath.Join(cleaner.StoreDir, it.RequestHash)
			if fEntries, err := os.ReadDir(dirPath); err == nil {
				for _, fe := range fEntries {
					if !fe.IsDir() {
						it.Files = append(it.Files, fe.Name())
						if fi, err := fe.Info(); err == nil {
							it.Bytes += fi.Size()
						}
					}
				}
			}
			if len(it.Files) > 0 {
				items = append(items, it)
			}
		}

		c.JSON(http.StatusOK, gin.H{"items": items})
	})

	group.POST("/storage/clean", func(c *gin.Context) {
		var req CleanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			req.Scope = "expired"
		}
		ctx := c.Request.Context()
		res := executeClean(ctx, cleaner, req.Scope, req.DryRun)
		c.JSON(http.StatusOK, res)
	})
}

func executeClean(ctx context.Context, cleaner *queue.Cleaner, scope string, dryRun bool) CleanResponse {
	var resp CleanResponse
	now := time.Now().UTC()

	switch scope {
	case "orphans":
		entries, err := os.ReadDir(cleaner.StoreDir)
		if err == nil {
			for _, entry := range entries {
				id := entry.Name()
				if !entry.IsDir() || len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
					continue
				}
				info, err := entry.Info()
				if err != nil || now.Before(info.ModTime().Add(cleaner.OrphanGrace)) {
					continue
				}
				var exists int
				_ = cleaner.Jobs.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE request_hash=?`, id).Scan(&exists)
				if exists == 0 {
					dirPath := filepath.Join(cleaner.StoreDir, id)
					sz := dirSize(dirPath)
					resp.Items++
					resp.Bytes += sz
					if !dryRun {
						_ = cleaner.Store.Delete(ctx, id)
					}
				}
			}
		}

	case "failed":
		rows, err := cleaner.Jobs.DB.QueryContext(ctx, `SELECT request_hash FROM jobs WHERE status IN ('failed', 'timeout')`)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil {
					dirPath := filepath.Join(cleaner.StoreDir, id)
					sz := dirSize(dirPath)
					resp.Items++
					resp.Bytes += sz
					if !dryRun {
						_ = cleaner.Store.Delete(ctx, id)
					}
				}
			}
		}

	default: // expired
		rows, err := cleaner.Jobs.DB.QueryContext(ctx, `SELECT request_hash FROM jobs WHERE expires_at IS NOT NULL AND expires_at <= ?`, now)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil {
					dirPath := filepath.Join(cleaner.StoreDir, id)
					sz := dirSize(dirPath)
					resp.Items++
					resp.Bytes += sz
					if !dryRun {
						_, _ = cleaner.Delete(ctx, id)
					}
				}
			}
		}
	}

	return resp
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total
}
