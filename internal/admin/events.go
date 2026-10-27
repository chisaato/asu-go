package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/queue"
	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stream"
)

// RegisterEvents 注册全局与单任务 SSE 事件流。
func RegisterEvents(group *gin.RouterGroup, jobs *queue.SQLite, rt runtime.Runtime, cfg *config.Config, service stream.Service) {
	// 全局 SSE 流
	group.GET("/events", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		c.Writer.Flush()

		ctx := c.Request.Context()

		// 首次连接时发送一次当前集群概览
		if overview, err := BuildOverview(ctx, jobs, rt, cfg); err == nil {
			if raw, err := json.Marshal(overview); err == nil {
				_, _ = fmt.Fprintf(c.Writer, "event: overview\ndata: %s\n\n", raw)
				c.Writer.Flush()
			}
		}

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 发送心跳保活
				_, err := fmt.Fprintf(c.Writer, ": keepalive\n\n")
				if err != nil {
					return
				}
				c.Writer.Flush()
			}
		}
	})

	// 单任务实时 SSE 流
	group.GET("/jobs/:id/stream", func(c *gin.Context) {
		id := c.Param("id")
		ctx := c.Request.Context()

		if retained, ok := service.Jobs.(interface {
			LogVisible(context.Context, string) (bool, error)
		}); ok {
			visible, err := retained.LogVisible(ctx, id)
			if err != nil || !visible {
				c.JSON(404, gin.H{"error": "job not found"})
				return
			}
		}

		logStream, err := service.Open(ctx, id, true)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, stream.ErrInvalidID) {
				status = http.StatusBadRequest
			} else if os.IsNotExist(err) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": http.StatusText(status)})
			return
		}
		defer func() { _ = logStream.Close() }()

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		c.Writer.Flush()

		writer := &sseLogWriter{writer: c.Writer}
		_ = logStream.Copy(ctx, writer)

		// 检查最终任务状态
		if j, err := jobs.Job(ctx, id); err == nil {
			_, _ = fmt.Fprintf(c.Writer, "event: status\ndata: %s\n\n", j.Status)
		}
		_, _ = fmt.Fprintf(c.Writer, "event: end\ndata: {}\n\n")
		c.Writer.Flush()
	})
}

type sseLogWriter struct {
	writer     gin.ResponseWriter
	totalBytes int64
	buf        string
}

func (s *sseLogWriter) Write(p []byte) (int, error) {
	s.buf += string(p)
	for {
		idx := strings.IndexByte(s.buf, '\n')
		if idx < 0 {
			break
		}
		line := s.buf[:idx]
		s.buf = s.buf[idx+1:]
		s.totalBytes += int64(len(line) + 1)

		if strings.HasPrefix(line, "##asu:stage=") {
			stageName := strings.TrimPrefix(line, "##asu:stage=")
			stagePayload, _ := json.Marshal(map[string]string{
				"name": stageName,
				"at":   time.Now().UTC().Format(time.RFC3339),
			})
			_, _ = fmt.Fprintf(s.writer, "event: stage\ndata: %s\n\n", stagePayload)
		} else {
			_, _ = fmt.Fprintf(s.writer, "id: %d\nevent: log\ndata: %s\n\n", s.totalBytes, line)
		}
		s.writer.Flush()
	}
	return len(p), nil
}
