package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/admin"
	"github.com/gzzchh/asu-go/internal/api"
	"github.com/gzzchh/asu-go/internal/buildreq"
	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/cors"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/httplog"
	"github.com/gzzchh/asu-go/internal/logcontext"
	"github.com/gzzchh/asu-go/internal/logging"
	"github.com/gzzchh/asu-go/internal/metadata"
	"github.com/gzzchh/asu-go/internal/queue"
	"github.com/gzzchh/asu-go/internal/runner"
	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/sources"
	"github.com/gzzchh/asu-go/internal/store"
	"github.com/gzzchh/asu-go/internal/stream"
	"github.com/gzzchh/asu-go/internal/webui"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func runServe(parent context.Context, cfg config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	logger, err := logging.New(cfg.Log)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()
	logger.Info("生效配置", zap.String("component", "server"), zap.String("version", Version), zap.String("listen", cfg.Server.Listen), zap.String("data_dir", cfg.DataDir), zap.String("engine", cfg.Runtime.Driver), zap.String("socket", cfg.Runtime.Socket), zap.Int("max_concurrent_builds", cfg.Queue.MaxConcurrentBuilds), zap.String("queue_driver", cfg.Queue.Driver), zap.String("store_driver", cfg.Store.Driver), zap.String("stats_driver", cfg.Stats.Driver), zap.String("log_level", cfg.Log.Level), zap.String("log_format", cfg.Log.Format))
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	database, err := db.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(ctx); err != nil {
		return err
	}
	catalog := sources.NewStore(database)
	if err := catalog.SeedDefaults(ctx); err != nil {
		return err
	}
	_ = catalog.PruneObsolete(ctx)
	if err := api.ValidateCatalog(ctx, catalog, cfg.Server.DefaultDistro); err != nil {
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("HTTP 请求处理异常", zap.String("component", "http"), zap.String("request_id", logcontext.RequestID(c.Request.Context())), zap.String("panic_type", fmt.Sprintf("%T", recovered)), zap.Error(errors.New("HTTP handler panic")))
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	})
	router.Use(httplog.HTTP(logger))
	router.Use(api.DistroScope(catalog, cfg.Server.DefaultDistro))
	router.Use(cors.New(cfg.Server.CORS))
	public := []gin.IRouter{router, router.Group("/:distro")}
	webui.Register(router)
	adminGroup, err := admin.RegisterAuth(router, cfg.Admin, logger)
	if err != nil {
		return err
	}
	admin.RegisterSources(adminGroup, catalog)
	upstream := metadata.NewClient("asu-go/" + Version)
	metadataCache := &metadata.Cache{Dir: cfg.DataDir, TTL: cfg.Metadata.CacheTTL, Client: upstream, Logger: logger.With(zap.String("component", "metadata-cache"))}
	meta := &metadata.Service{Store: catalog, Client: upstream, Version: Version, Cache: metadataCache}
	for _, routes := range public {
		api.RegisterMetadata(routes, meta)
	}
	hub, err := stream.NewHub(cfg.DataDir, stream.DefaultCapacity)
	if err != nil {
		return fmt.Errorf("日志 Hub: %w", err)
	}
	defer func() { _ = hub.Close() }()
	rt, err := runtime.Open(cfg.Runtime.Driver, runtime.Options{Logger: logger, Socket: cfg.Runtime.Socket, MemoryLimit: cfg.Runtime.MemoryLimit, CPUs: cfg.Runtime.CPUs, PullPolicy: cfg.Runtime.PullPolicy})
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if _, err := rt.Info(ctx); err != nil {
		return err
	}
	buildRunner := &runner.Runner{Logger: logger, Runtime: rt, Hub: hub, JobTimeout: cfg.Queue.JobTimeout, RollingRecheck: cfg.Runtime.RollingRecheck}
	jobs := queue.NewSQLite(database.DB, catalog, meta, buildRunner, queue.Options{
		Logger: logger, DataDir: cfg.DataDir, MaxConcurrentBuilds: cfg.Queue.MaxConcurrentBuilds, MaxPendingJobs: cfg.Queue.MaxPendingJobs,
		BuildTTL: cfg.Queue.BuildTTL, BuildUnversionedTTL: cfg.Queue.BuildUnversionedTTL, BuildDefaultsTTL: cfg.Queue.BuildDefaultsTTL, BuildFailureTTL: cfg.Queue.BuildFailureTTL, Retention: cfg.Diagnostics.Retention,
		Limits: buildreq.Limits{AllowDefaults: cfg.Build.AllowDefaults, MaxDefaultsLength: cfg.Build.MaxDefaultsLength, MaxCustomRootfsSizeMB: cfg.Build.MaxCustomRootfsSizeMB, RepositoryAllowList: cfg.Build.RepositoryAllowList},
	})
	logService := stream.Service{Hub: hub, Jobs: jobs}
	admin.RegisterLogs(adminGroup, logService)
	admin.RegisterEvents(adminGroup, jobs, rt, &cfg, logService)
	admin.RegisterOverview(adminGroup, jobs, rt, &cfg)
	admin.RegisterSettings(adminGroup, &cfg, rt)
	artifactStore := &store.Local{Dir: cfg.StoreDir(), Logger: logger}
	jobs.Publisher = artifactStore
	for _, routes := range public {
		api.RegisterStore(routes, artifactStore, jobs.StoreVisible)
	}
	cleaner := &queue.Cleaner{Jobs: jobs, Store: artifactStore, StoreDir: cfg.StoreDir(), Interval: cfg.Cleaner.Interval, OrphanGrace: cfg.Cleaner.OrphanGrace, Logger: logger.With(zap.String("component", "cleaner"))}
	admin.RegisterStore(adminGroup, cleaner)
	admin.RegisterJobs(adminGroup, jobs)
	for _, routes := range public {
		api.RegisterBuild(routes, jobs)
	}
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/:distro/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	listener, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("server.listen: %w", err)
	}
	server := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return cleaner.Run(groupCtx) })
	group.Go(func() error { return jobs.Run(groupCtx) })
	scavenger := &runner.Scavenger{Logger: logger, Runtime: rt, Jobs: jobs, Interval: cfg.Runtime.ScavengerInterval, KeepContainerTTL: cfg.Diagnostics.KeepContainerTTL, MaxKept: cfg.Diagnostics.MaxKept}
	group.Go(func() error { return scavenger.Run(groupCtx) })
	syncer := &metadata.Syncer{Store: catalog, Client: upstream, Logger: logger.With(zap.String("component", "metadata"))}
	group.Go(func() error { return syncer.Run(groupCtx, cfg.Metadata.RefreshInterval) })
	group.Go(func() error {
		logger.Info("服务已启动", zap.String("component", "server"), zap.String("listen", listener.Addr().String()))
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("正在关闭服务", zap.String("component", "server"))
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("关闭 HTTP 服务: %w", err)
		}
		return nil
	})
	return group.Wait()
}
