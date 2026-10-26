package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/sources"
)

func sourceError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "来源目录操作失败"
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status, message = http.StatusNotFound, "来源不存在"
	case errors.Is(err, sources.ErrInvalid):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, sources.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	}
	c.JSON(status, gin.H{"error": message})
}

func decode(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效 JSON: " + err.Error()})
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只允许一个 JSON 文档"})
		return false
	}
	return true
}

func sourceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 必须为正整数"})
		return 0, false
	}
	return id, true
}

// RegisterSources 接收已挂载鉴权的组，供 CLI 与后续后台共享。
func RegisterSources(group *gin.RouterGroup, store *sources.Store) {
	group.GET("/sources", func(c *gin.Context) {
		catalog, err := store.List(c.Request.Context())
		if err != nil {
			sourceError(c, err)
			return
		}
		c.JSON(http.StatusOK, catalog)
	})
	group.GET("/sources/:id", func(c *gin.Context) {
		id, ok := sourceID(c)
		if !ok {
			return
		}
		source, err := store.Get(c.Request.Context(), id)
		if err != nil {
			sourceError(c, err)
			return
		}
		c.JSON(http.StatusOK, source)
	})
	group.POST("/sources", func(c *gin.Context) {
		source := sources.NewSource()
		if !decode(c, &source) {
			return
		}
		created, err := store.Create(c.Request.Context(), source)
		if err != nil {
			sourceError(c, err)
			return
		}
		c.JSON(http.StatusCreated, created)
	})
	group.PATCH("/sources/:id", func(c *gin.Context) {
		id, ok := sourceID(c)
		if !ok {
			return
		}
		var patch map[string]json.RawMessage
		if !decode(c, &patch) {
			return
		}
		if patch == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "需要字段对象"})
			return
		}
		updated, err := store.Patch(c.Request.Context(), id, patch)
		if err != nil {
			sourceError(c, err)
			return
		}
		c.JSON(http.StatusOK, updated)
	})
	group.DELETE("/sources/:id", func(c *gin.Context) {
		id, ok := sourceID(c)
		if !ok {
			return
		}
		if err := store.Delete(c.Request.Context(), id); err != nil {
			sourceError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	group.POST("/sources/import", func(c *gin.Context) {
		var catalog []sources.Source
		if !decode(c, &catalog) {
			return
		}
		if err := store.Import(c.Request.Context(), catalog); err != nil {
			sourceError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"imported": len(catalog)})
	})
}
