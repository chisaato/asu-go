package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/queue"
)

// RegisterStore 只接受已鉴权的管理路由组。
func RegisterStore(group *gin.RouterGroup, cleaner *queue.Cleaner) {
	group.DELETE("/store/:hash", func(c *gin.Context) {
		response, err := cleaner.Delete(c.Request.Context(), c.Param("hash"))
		if err != nil {
			c.JSON(500, gin.H{"error": "internal error"})
			return
		}
		c.JSON(response.Status, response.Body)
	})
}
