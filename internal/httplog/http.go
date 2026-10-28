package httplog

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/logcontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// HTTP 在请求结束时记录摘要；不记录查询串、请求体或认证头。
func HTTP(logger *zap.Logger) gin.HandlerFunc {
	logger = logcontext.OrNop(logger).With(zap.String("component", "http"))
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n\x00") {
			var b [16]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		c.Request = c.Request.WithContext(logcontext.WithRequestID(c.Request.Context(), id))
		c.Header("X-Request-Id", id)
		start := time.Now()
		defer func() {
			p := recover()
			status := c.Writer.Status()
			if p != nil {
				status = http.StatusInternalServerError
			}
			level := zapcore.InfoLevel
			if status >= 500 {
				level = zapcore.ErrorLevel
			} else if status >= 400 {
				level = zapcore.WarnLevel
			} else if c.Request.URL.Path == "/healthz" || c.FullPath() == "/:distro/healthz" || strings.HasPrefix(c.Request.URL.Path, "/admin/_nuxt/") || strings.HasPrefix(c.Request.URL.Path, "/admin/favicon") {
				level = zapcore.DebugLevel
			}
			fields := []zap.Field{zap.String("method", c.Request.Method), zap.String("path", c.Request.URL.Path), zap.String("route", c.FullPath()), zap.Int("status", status), zap.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000), zap.String("client_ip", c.ClientIP()), zap.Int("bytes", max(c.Writer.Size(), 0)), zap.String("user_agent", c.Request.UserAgent()), zap.String("request_id", id)}
			if len(c.Errors) > 0 {
				causes := make([]error, 0, len(c.Errors))
				for _, cause := range c.Errors {
					causes = append(causes, cause.Err)
				}
				fields = append(fields, zap.Error(errors.Join(causes...)))
			}
			if hash := c.GetString("request_hash"); hash != "" {
				fields = append(fields, zap.String("request_hash", hash))
			}
			logger.Log(level, "HTTP 请求", fields...)
			if p != nil {
				panic(p)
			}
		}()
		c.Next()
	}
}
