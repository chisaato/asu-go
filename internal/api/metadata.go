// Package api 提供与原版 ASU 兼容的公开 API。
package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/metadata"
)

func metadataError(c *gin.Context, err error) {
	var profile *metadata.ProfileNotFoundError
	if errors.As(err, &profile) {
		c.JSON(http.StatusNotFound, gin.H{"detail": profile.Error()})
		return
	}
	var revision *metadata.RevisionError
	if errors.As(err, &revision) {
		c.JSON(revision.Status, gin.H{"detail": revision.Detail, "status": revision.Status})
		return
	}
	c.Status(http.StatusInternalServerError)
}

func RegisterMetadata(router *gin.Engine, service *metadata.Service) {
	// Gin 不允许在同一前缀下混用静态路由和 *path；在一个入口中分派。
	router.GET("/json/v1/*path", func(c *gin.Context) {
		path := strings.TrimPrefix(c.Param("path"), "/")
		switch path {
		case "latest.json":
			overview, err := service.Overview(c.Request.Context())
			if err != nil {
				metadataError(c, err)
				return
			}
			c.JSON(http.StatusOK, gin.H{"latest": overview.Latest})
		case "branches.json":
			branches, err := service.Branches(c.Request.Context())
			if err != nil {
				metadataError(c, err)
				return
			}
			c.JSON(http.StatusOK, branches)
		case "overview.json":
			overview, err := service.Overview(c.Request.Context())
			if err != nil {
				metadataError(c, err)
				return
			}
			c.JSON(http.StatusOK, overview)
		default:
			value, err := service.JSONPath(c.Request.Context(), path)
			if err != nil {
				metadataError(c, err)
				return
			}
			c.JSON(http.StatusOK, value)
		}
	})
	router.GET("/api/v1/revision/:version/:target/:subtarget", func(c *gin.Context) {
		revision, err := service.Revision(c.Request.Context(), c.Param("version"), c.Param("target"), c.Param("subtarget"))
		if err != nil {
			metadataError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"revision": revision})
	})
	for _, name := range []string{"latest", "overview"} {
		router.GET("/api/v1/"+name, func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/json/v1/"+name+".json") })
	}
}
