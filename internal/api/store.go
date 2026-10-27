package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/store"
	"mime"
	"net/http"
	"path"
	"strings"
)

// RegisterStore 提供匿名下载，按原版 FileResponse 强制二进制附件类型。
func RegisterStore(router gin.IRouter, backend store.Store, visible ...func(context.Context, string) (bool, error)) {
	handler := func(c *gin.Context) {
		hash, name, _ := strings.Cut(strings.TrimPrefix(c.Param("path"), "/"), "/")
		if len(visible) > 0 {
			ok, err := visible[0](c.Request.Context(), hash)
			if err != nil || !ok {
				c.JSON(http.StatusNotFound, gin.H{"detail": "Not found"})
				return
			}
		}
		file, err := backend.Open(c.Request.Context(), hash, name)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Not found"})
			return
		}
		defer func() { _ = file.Close() }()
		info, err := file.Stat()
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"detail": "Not found"})
			return
		}
		c.Header("Content-Type", "application/octet-stream")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
		http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
	}
	router.GET("/store/*path", handler)
	router.HEAD("/store/*path", handler)
}
