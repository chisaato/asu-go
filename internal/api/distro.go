package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/sources"
)

// ValidateCatalog 在启动时检查已有目录（包括旧数据库）与默认发行版。
func ValidateCatalog(ctx context.Context, catalog *sources.Store, defaultDistro string) error {
	all, err := catalog.List(ctx)
	if err != nil {
		return err
	}
	enabled := false
	for _, source := range all {
		if err := sources.ValidateDistribution(source.Distribution); err != nil {
			return err
		}
		enabled = enabled || source.Distribution == defaultDistro && source.Enabled
	}
	if !enabled {
		return fmt.Errorf("server.default_distro: %q 没有已启用的 source", defaultDistro)
	}
	return nil
}

// DistroScope 必须先于 CORS 注册，以便未知/已禁用发行版的预检也返回 404。
// 每次查询目录，使管理后台启用/禁用发行版立即生效，无须重启路由。
func DistroScope(catalog *sources.Store, defaultDistro string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if prefix := c.Param("distro"); c.FullPath() != "" && prefix != "" && sources.ValidateDistribution(prefix) != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		first, rest, _ := strings.Cut(path, "/")
		prefix := ""
		switch first {
		case "api", "json", "store":
		default:
			second, _, _ := strings.Cut(rest, "/")
			if second != "api" && second != "json" && second != "store" && second != "healthz" {
				c.Next()
				return
			}
			// 管理 API 独立鉴权，绝不能被当作发行版镜像。
			if first == "admin" && second == "api" {
				c.Next()
				return
			}
			prefix = first
		}
		distro := prefix
		if distro == "" {
			distro = defaultDistro
		}
		all, err := catalog.List(c.Request.Context())
		if err != nil {
			_ = c.Error(err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		for _, source := range all {
			if source.Distribution == distro && source.Enabled {
				c.Set("public_distro", distro)
				c.Next()
				return
			}
		}
		if prefix != "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"status": 503, "detail": fmt.Sprintf("server.default_distro: %s has no enabled source", distro)})
	}
}

func requestDistro(c *gin.Context) string {
	if distro := c.GetString("public_distro"); distro != "" {
		return distro
	}
	if distro := c.Param("distro"); distro != "" {
		return distro
	}
	return "openwrt"
}
