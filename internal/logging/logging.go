package logging

import (
	"fmt"

	"github.com/gzzchh/asu-go/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(cfg config.Log) (*zap.Logger, error) {
	if cfg.Format != "json" && cfg.Format != "console" {
		return nil, fmt.Errorf("log.format: 不支持 %q", cfg.Format)
	}
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("log.level: %w", err)
	}
	c := zap.NewProductionConfig()
	c.Encoding = cfg.Format
	c.Level = zap.NewAtomicLevelAt(level)
	c.EncoderConfig.TimeKey = "timestamp"
	c.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	if cfg.Format == "console" {
		c.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	return c.Build()
}
