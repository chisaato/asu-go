// Package webui 提供嵌入式管理后台与 SPA 回退。
package webui

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/web"
)

// Register 挂载静态后台，不占用 /admin/api 路由空间。
func Register(r *gin.Engine) {
	files, err := fs.Sub(web.Files, "dist")
	if err != nil {
		panic(err)
	}
	register(r, files)
}

func register(r *gin.Engine, files fs.FS) {
	assets := http.StripPrefix("/admin/", http.FileServer(http.FS(files)))
	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		// 显式路由优先；API、非后台路径和写请求不参与 SPA 回退。
		if c.FullPath() != "" || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) ||
			(p != "/admin" && !strings.HasPrefix(p, "/admin/")) ||
			p == "/admin/api" || strings.HasPrefix(p, "/admin/api/") {
			c.Next()
			return
		}
		name := strings.TrimPrefix(p, "/admin/")
		if name != "" && fs.ValidPath(name) {
			if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
				assets.ServeHTTP(c.Writer, c.Request)
				c.Abort()
				return
			}
		}
		index, err := fs.ReadFile(files, "index.html")
		if err != nil {
			index, err = fs.ReadFile(files, "placeholder.html")
		}
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-cache")
		if c.Request.Method == http.MethodHead {
			c.Status(http.StatusOK)
			c.Header("Content-Type", "text/html; charset=utf-8")
		} else {
			c.Data(http.StatusOK, "text/html; charset=utf-8", index)
		}
		c.Abort()
	})
}
