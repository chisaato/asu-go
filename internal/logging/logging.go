package logging

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chisaato/asu-go/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(cfg config.Log) (*zap.Logger, error) {
	switch cfg.Format {
	case "json", "text", "plain", "console":
	default:
		return nil, fmt.Errorf("log.format: 不支持 %q", cfg.Format)
	}
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("log.level: %w", err)
	}
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	fileEncoder := zapcore.NewJSONEncoder(encoderConfig)
	stdoutEncoder := fileEncoder.Clone()
	if cfg.Format != "json" {
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderConfig.EncodeDuration = zapcore.StringDurationEncoder
		fileEncoder = newTextEncoder(encoderConfig)
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		stdoutEncoder = newTextEncoder(encoderConfig)
	}
	cores := []zapcore.Core{zapcore.NewCore(stdoutEncoder, zapcore.Lock(os.Stdout), level)}
	if cfg.File != "" {
		file, err := openFile(cfg.File)
		if err != nil {
			return nil, fmt.Errorf("log.file: %w", err)
		}
		cores = append(cores, zapcore.NewCore(fileEncoder, zapcore.Lock(file), level))
	}
	return zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.DPanicLevel)), nil
}

const maxFileSize = 50 << 20

// 仅启动时轮转，保留一份 .1；运行期间不引入后台协程或额外依赖。
func openFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件: %s", path)
	}
	if err == nil && info.Size() > maxFileSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
