package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/buildreq"
	"github.com/gzzchh/asu-go/internal/queue"
)

// RegisterJobs 仅挂载到已鉴权的管理路由组。
func RegisterJobs(group *gin.RouterGroup, jobs *queue.SQLite) {
	respond := func(c *gin.Context, response queue.Response, err error) {
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(response.Status, response.Body)
	}
	group.POST("/jobs/:id/cancel", func(c *gin.Context) {
		r, err := jobs.Cancel(c.Request.Context(), c.Param("id"))
		respond(c, r, err)
	})
	replay := func(c *gin.Context) {
		var body map[string]json.RawMessage
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20))
		err := decoder.Decode(&body)
		if err != nil && (!errors.Is(err, io.EOF) || c.Param("id") == "") {
			c.JSON(400, gin.H{"error": "invalid JSON"})
			return
		}
		if err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				c.JSON(400, gin.H{"error": "invalid JSON"})
				return
			}
		}
		var options queue.ReplayOptions
		if raw, ok := body["options"]; ok {
			if err := json.Unmarshal(raw, &options); err != nil {
				c.JSON(400, gin.H{"error": "invalid options"})
				return
			}
			delete(body, "options")
		}
		request := buildreq.NewRequest()
		if c.Param("id") == "" {
			raw, err := json.Marshal(body)
			if err == nil {
				err = json.Unmarshal(raw, &request)
			}
			if err != nil {
				respond(c, queue.ValidationResponse(err), nil)
				return
			}
		}
		r, err := jobs.Replay(c.Request.Context(), c.Param("id"), request, options)
		respond(c, r, err)
	}
	group.POST("/jobs/:id/replay", replay)
	group.POST("/replay", replay)
	group.GET("/jobs/:id", func(c *gin.Context) {
		j, err := jobs.Job(c.Request.Context(), c.Param("id"))
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": "job not found"})
		} else if err != nil {
			c.JSON(500, gin.H{"error": "internal error"})
		} else {
			c.JSON(200, j)
		}
	})
	group.GET("/jobs", func(c *gin.Context) {
		page, errPage := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, errSize := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		status := c.Query("status")
		if errPage != nil || errSize != nil || page < 1 || size < 1 || size > 200 || (status != "" && status != "all" && status != "queued" && status != "running" && status != "done" && status != "failed" && status != "finished") {
			c.JSON(400, gin.H{"error": "invalid status or pagination"})
			return
		}
		list, err := jobs.Jobs(c.Request.Context(), status, page, size)
		if err != nil {
			c.JSON(500, gin.H{"error": "internal error"})
			return
		}
		c.JSON(200, list)
	})
}
