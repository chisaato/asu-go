package admin

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/config"
	"github.com/chisaato/asu-go/internal/runtime"
)

type SettingsResponse struct {
	Server  ServerSummary  `json:"server"`
	Storage StorageSummary `json:"storage"`
	Runner  RunnerSummary  `json:"runner"`
	Build   BuildSummary   `json:"build"`
	Version VersionSummary `json:"version"`
	Engine  *EngineInfo    `json:"engine,omitempty"`
}

type ServerSummary struct {
	Listen      string   `json:"listen"`
	CorsOrigins []string `json:"cors_origins"`
	LogLevel    string   `json:"log_level"`
	LogFormat   string   `json:"log_format"`
}

type StorageSummary struct {
	DataDir string            `json:"data_dir"`
	TTL     map[string]string `json:"ttl"`
	Driver  string            `json:"driver"`
}

type RunnerSummary struct {
	Concurrency  int    `json:"concurrency"`
	EngineType   string `json:"engine_type"`
	EngineSocket string `json:"engine_socket"`
	Network      string `json:"network"`
}

type BuildSummary struct {
	DefaultDistro string `json:"default_distro"`
}

type VersionSummary struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

// RegisterSettings 挂载只读配置摘要与引擎状态探测。
func RegisterSettings(group *gin.RouterGroup, cfg *config.Config, rt runtime.Runtime) {
	group.GET("/settings", func(c *gin.Context) {
		ctx := c.Request.Context()
		eng := probeEngineInfo(ctx, rt, cfg)

		goVer := "unknown"
		if bi, ok := debug.ReadBuildInfo(); ok {
			goVer = bi.GoVersion
		}

		res := SettingsResponse{
			Server: ServerSummary{
				Listen:      cfg.Server.Listen,
				CorsOrigins: cfg.Server.CORS.AdminOrigins,
				LogLevel:    cfg.Log.Level,
				LogFormat:   cfg.Log.Format,
			},
			Storage: StorageSummary{
				DataDir: cfg.DataDir,
				TTL: map[string]string{
					"defaults":    cfg.Queue.BuildDefaultsTTL.String(),
					"versioned":   cfg.Queue.BuildTTL.String(),
					"unversioned": cfg.Queue.BuildUnversionedTTL.String(),
					"failed":      cfg.Queue.BuildFailureTTL.String(),
					"purge_at":    cfg.Diagnostics.Retention.String(),
				},
				Driver: "sqlite",
			},
			Runner: RunnerSummary{
				Concurrency:  cfg.Queue.MaxConcurrentBuilds,
				EngineType:   cfg.Runtime.Driver,
				EngineSocket: cfg.Runtime.Socket,
				Network:      "bridge",
			},
			Build: BuildSummary{
				DefaultDistro: cfg.Server.DefaultDistro,
			},
			Version: VersionSummary{
				Version:   "v1.0.0",
				BuildTime: "2026-10-28",
				GoVersion: goVer,
			},
			Engine: &eng,
		}

		c.JSON(http.StatusOK, res)
	})

	group.POST("/engine/probe", func(c *gin.Context) {
		eng := probeEngineInfo(c.Request.Context(), rt, cfg)
		c.JSON(http.StatusOK, eng)
	})
}
