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
	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/logging"
	"github.com/gzzchh/asu-go/internal/metadata"
	"github.com/gzzchh/asu-go/internal/sources"
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
	logger = logger.With(zap.String("component", "server"))
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
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	webui.Register(router)
	router.Use(func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("HTTP 请求处理异常", zap.Any("panic", recovered))
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	})
	adminGroup, err := admin.RegisterAuth(router, cfg.Admin, logger)
	if err != nil {
		return err
	}
	admin.RegisterSources(adminGroup, catalog)
	upstream := metadata.NewClient("asu-go/" + Version)
	api.RegisterMetadata(router, &metadata.Service{Store: catalog, Client: upstream, Version: Version})
	hub, err := stream.NewHub(cfg.DataDir, stream.DefaultCapacity)
	if err != nil {
		return fmt.Errorf("日志 Hub: %w", err)
	}
	defer func() { _ = hub.Close() }()
	// jobs/队列接入时替换此内存适配；当前已可读取落盘历史日志。
	admin.RegisterLogs(adminGroup, stream.Service{Hub: hub, Jobs: stream.NewMemoryJobSource()})
	router.GET("/healthz", func(c *gin.Context) {
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
	syncer := &metadata.Syncer{Store: catalog, Client: upstream, Logger: logger.With(zap.String("component", "metadata"))}
	group.Go(func() error { return syncer.Run(groupCtx, cfg.Metadata.RefreshInterval) })
	group.Go(func() error {
		logger.Info("服务已启动", zap.String("listen", listener.Addr().String()))
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("正在关闭服务")
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("关闭 HTTP 服务: %w", err)
		}
		return nil
	})
	return group.Wait()
}
