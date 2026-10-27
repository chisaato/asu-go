// Package logcontext 提供独立于配置与 HTTP 框架的日志上下文辅助函数。
package logcontext

import (
	"context"

	"go.uber.org/zap"
)

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// OrNop 保持未注入 logger 的测试与可选组件可用。
func OrNop(logger *zap.Logger) *zap.Logger {
	if logger == nil {
		return zap.NewNop()
	}
	return logger
}
