// Package webui 提供嵌入式管理后台与 SPA 回退。
package webui

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/web"
)

// Register 挂载静态后台到 /ui 与 /admin 别名，不占用公开服务与 /admin/api 路由空间。
func Register(r *gin.Engine) {
	files, err := fs.Sub(web.Files, "dist")
	if err != nil {
		panic(err)
	}
	register(r, files)
}

func register(r *gin.Engine, files fs.FS) {
	uiAssets := http.StripPrefix("/ui/", http.FileServer(http.FS(files)))
	adminAssets := http.StripPrefix("/admin/", http.FileServer(http.FS(files)))

	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path

		// 显式路由优先；API、非后台路径和写请求不参与 SPA 回退。
		if c.FullPath() != "" || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			c.Next()
			return
		}
		if p == "/admin/api" || strings.HasPrefix(p, "/admin/api/") {
			c.Next()
			return
		}
		// 容错处理：如果请求带有 /ui/admin/api 前缀，重写路径并交由后端 API 处理，避免误落入 SPA 回退返回 200 HTML
		if strings.HasPrefix(p, "/ui/admin/api") {
			c.Request.URL.Path = strings.TrimPrefix(p, "/ui")
			r.HandleContext(c)
			c.Abort()
			return
		}

		isUI := p == "/ui" || strings.HasPrefix(p, "/ui/")
		isAdmin := p == "/admin" || strings.HasPrefix(p, "/admin/")
		if !isUI && !isAdmin {
			c.Next()
			return
		}

		var name string
		if isUI {
			name = strings.TrimPrefix(p, "/ui/")
		} else {
			name = strings.TrimPrefix(p, "/admin/")
		}

		if name != "" && fs.ValidPath(name) {
			if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
				if isUI {
					uiAssets.ServeHTTP(c.Writer, c.Request)
				} else {
					adminAssets.ServeHTTP(c.Writer, c.Request)
				}
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
