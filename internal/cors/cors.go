// Package cors 为公开接口与管理接口提供独立的跨域策略。
package cors

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/sources"
)

// New 返回全局中间件；预检在路由和管理鉴权之前结束。
func New(cfg config.CORS) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		// 仅剥离一个合法发行版前缀，绝不将 /admin/api 归入公开策略。
		first, rest, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		if sources.ValidateDistribution(first) == nil {
			path = "/" + rest
		}
		var origins []string
		admin := strings.HasPrefix(path, "/admin/api/")
		switch {
		case admin:
			origins = cfg.AdminOrigins
		case strings.HasPrefix(path, "/api/"), strings.HasPrefix(path, "/json/"), strings.HasPrefix(path, "/store/"):
			origins = cfg.PublicOrigins
		default:
			c.Next()
			return
		}
		wildcard := !admin && slices.Contains(origins, "*")
		if wildcard || slices.Contains(origins, origin) {
			if wildcard {
				c.Header("Access-Control-Allow-Origin", "*")
			} else {
				c.Header("Access-Control-Allow-Origin", origin)
			}
			methods := "GET, HEAD, POST, OPTIONS"
			headers := "Content-Type, Range, If-Modified-Since, If-None-Match"
			if admin {
				c.Header("Access-Control-Allow-Credentials", "true")
				methods = "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS"
				headers = "Content-Type, Authorization, X-Requested-With"
			} else {
				c.Header("Access-Control-Expose-Headers", "X-Imagebuilder-Status, X-Queue-Position, Content-Length, Content-Range, ETag, Last-Modified")
			}
			c.Header("Access-Control-Allow-Methods", methods)
			c.Header("Access-Control-Allow-Headers", headers)
			c.Header("Access-Control-Max-Age", "86400")
		}
		// 精确来源策略的拒绝响应也必须参与缓存区分，避免污染允许来源的响应。
		if len(origins) > 0 && !wildcard {
			c.Writer.Header().Add("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
