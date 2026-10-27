package admin

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/queue"
	"github.com/gzzchh/asu-go/internal/runtime"
)

type OverviewResponse struct {
	Slots  SlotsInfo  `json:"slots"`
	Queued int        `json:"queued"`
	Active []any      `json:"active"`
	Queue  []any      `json:"queue"`
	Recent []any      `json:"recent"`
	Today  TodayStats `json:"today"`
	Disk   DiskInfo   `json:"disk"`
	Engine EngineInfo `json:"engine"`
	Stats  *StatsInfo `json:"stats,omitempty"`
}

type SlotsInfo struct {
	Running int `json:"running"`
	Max     int `json:"max"`
}

type TodayStats struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

type DiskInfo struct {
	UsedBytes  int64  `json:"used_bytes"`
	LimitBytes *int64 `json:"limit_bytes"`
	FreeBytes  int64  `json:"free_bytes"`
}

type EngineInfo struct {
	Type       string `json:"type"`
	Version    string `json:"version"`
	Socket     string `json:"socket"`
	LatencyMs  int64  `json:"latency_ms"`
	Containers int    `json:"containers"`
}

type StatsInfo struct {
	TopProfiles []NameCount `json:"top_profiles"`
	TopPackages []NameCount `json:"top_packages"`
}

type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// RegisterOverview 注册集群概览接口到管理路由组。
func RegisterOverview(group *gin.RouterGroup, jobs *queue.SQLite, rt runtime.Runtime, cfg *config.Config) {
	group.GET("/overview", func(c *gin.Context) {
		ctx := c.Request.Context()
		res, err := BuildOverview(ctx, jobs, rt, cfg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, res)
	})
}

// BuildOverview 组装当前系统的完整概览数据。
func BuildOverview(ctx context.Context, jobs *queue.SQLite, rt runtime.Runtime, cfg *config.Config) (OverviewResponse, error) {
	maxSlots := 4
	if cfg != nil && cfg.Queue.MaxConcurrentBuilds > 0 {
		maxSlots = cfg.Queue.MaxConcurrentBuilds
	}

	var res OverviewResponse
	res.Slots.Max = maxSlots
	res.Active = []any{}
	res.Queue = []any{}
	res.Recent = []any{}

	// 1. 查询活跃与排队任务列表
	if activeList, err := jobs.JobsWithFilter(ctx, queue.JobFilter{Status: "running", Page: 1, Size: 50}); err == nil {
		res.Slots.Running = activeList.Total
		for _, item := range activeList.Items {
			res.Active = append(res.Active, item)
		}
	}
	if queueList, err := jobs.JobsWithFilter(ctx, queue.JobFilter{Status: "queued", Page: 1, Size: 50}); err == nil {
		res.Queued = queueList.Total
		for _, item := range queueList.Items {
			res.Queue = append(res.Queue, item)
		}
	}

	// 2. 查询近期完成任务 (最近 10 条)
	if recentList, err := jobs.JobsWithFilter(ctx, queue.JobFilter{Status: "finished", Page: 1, Size: 10}); err == nil {
		for _, item := range recentList.Items {
			res.Recent = append(res.Recent, item)
		}
	}

	// 3. 统计今日构建数据 (按 UTC 当天 0 点)
	now := time.Now().UTC()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	_ = jobs.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE enqueued_at >= ?`, startOfToday).Scan(&res.Today.Total)
	_ = jobs.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE enqueued_at >= ? AND status='done'`, startOfToday).Scan(&res.Today.Success)
	_ = jobs.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE enqueued_at >= ? AND status IN ('failed', 'timeout')`, startOfToday).Scan(&res.Today.Failed)

	// 4. 磁盘占用与剩余空间
	storeDir := "./data/store"
	if cfg != nil {
		storeDir = cfg.StoreDir()
	}
	res.Disk = calculateDiskInfo(storeDir)

	// 5. 容器引擎状态探测
	res.Engine = probeEngineInfo(ctx, rt, cfg)

	return res, nil
}

func calculateDiskInfo(path string) DiskInfo {
	var info DiskInfo
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err == nil {
		info.FreeBytes = int64(stat.Bavail) * int64(stat.Bsize)
	}

	// 计算 store 目录内文件实际占用
	var used int64
	_ = filepath.Walk(path, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && !fi.IsDir() {
			used += fi.Size()
		}
		return nil
	})
	info.UsedBytes = used
	return info
}

func probeEngineInfo(ctx context.Context, rt runtime.Runtime, cfg *config.Config) EngineInfo {
	engineType := "podman"
	socketPath := "/run/podman/podman.sock"
	if cfg != nil && cfg.Runtime.Driver != "" {
		engineType = cfg.Runtime.Driver
		socketPath = cfg.Runtime.Socket
	}

	info := EngineInfo{
		Type:       engineType,
		Version:    "unknown",
		Socket:     socketPath,
		LatencyMs:  1,
		Containers: 0,
	}

	if rt == nil {
		return info
	}

	start := time.Now()
	rtInfo, err := rt.Info(ctx)
	info.LatencyMs = max(time.Since(start).Milliseconds(), 1)
	if err == nil {
		if rtInfo.Name != "" {
			info.Type = rtInfo.Name
		}
		info.Version = rtInfo.Version
	}

	if list, err := rt.List(ctx); err == nil {
		runningContainers := 0
		for _, s := range list {
			if s.Running {
				runningContainers++
			}
		}
		info.Containers = runningContainers
	}

	return info
}
