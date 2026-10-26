package admin

import (
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/stream"
)

type flushingWriter struct{ writer gin.ResponseWriter }

func (w flushingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.writer.Flush()
	return n, err
}

// RegisterLogs 必须挂载到已应用 RequireAuth 的管理路由组。
func RegisterLogs(group *gin.RouterGroup, service stream.Service) {
	group.GET("/jobs/:id/log", func(c *gin.Context) {
		log, err := service.Open(c.Request.Context(), c.Param("id"), c.Query("follow") == "1")
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
		defer func() { _ = log.Close() }()
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		c.Writer.Flush()
		_ = log.Copy(c.Request.Context(), flushingWriter{c.Writer})
	})
}
