// Package api 提供与原版 ASU 兼容的公开 API。
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/metadata"
)

func metadataError(c *gin.Context, err error) {
	var revision *metadata.RevisionError
	if errors.As(err, &revision) {
		c.JSON(revision.Status, gin.H{"detail": revision.Detail, "status": revision.Status})
		return
	}
	c.Status(http.StatusInternalServerError)
}

func RegisterMetadata(router *gin.Engine, service *metadata.Service) {
	router.GET("/json/v1/latest.json", func(c *gin.Context) {
		overview, err := service.Overview(c.Request.Context())
		if err != nil {
			metadataError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"latest": overview.Latest})
	})
	router.GET("/json/v1/branches.json", func(c *gin.Context) {
		branches, err := service.Branches(c.Request.Context())
		if err != nil {
			metadataError(c, err)
			return
		}
		c.JSON(http.StatusOK, branches)
	})
	router.GET("/json/v1/overview.json", func(c *gin.Context) {
		overview, err := service.Overview(c.Request.Context())
		if err != nil {
			metadataError(c, err)
			return
		}
		c.JSON(http.StatusOK, overview)
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
