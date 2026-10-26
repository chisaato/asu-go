package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/buildreq"
	"github.com/gzzchh/asu-go/internal/queue"
)

func RegisterBuild(router *gin.Engine, q *queue.SQLite) {
	write := func(c *gin.Context, response queue.Response, err error) {
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": 500, "detail": "Internal Server Error"})
			return
		}
		if response.Stage != "" {
			c.Header("X-Imagebuilder-Status", response.Stage)
			c.Header("X-Queue-Position", strconv.Itoa(response.Position))
		}
		if c.Request.Method == http.MethodHead {
			c.Header("Content-Type", "application/json")
			c.Status(response.Status)
			return
		}
		c.JSON(response.Status, response.Body)
	}
	router.POST("/api/v1/build", func(c *gin.Context) {
		var request buildreq.Request
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
		if err := decoder.Decode(&request); err != nil {
			write(c, queue.ValidationResponse(err), nil)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			write(c, queue.ValidationResponse(&buildreq.ValidationError{Status: 422, Field: "request", Detail: "Invalid JSON document"}), nil)
			return
		}
		response, err := q.Submit(c.Request.Context(), request, c.GetHeader("User-Agent"))
		write(c, response, err)
	})
	poll := func(c *gin.Context) {
		response, err := q.Get(c.Request.Context(), c.Param("hash"))
		write(c, response, err)
	}
	router.GET("/api/v1/build/:hash", poll)
	router.HEAD("/api/v1/build/:hash", poll)
	router.GET("/api/v1/stats", func(c *gin.Context) {
		length, err := q.Length(c.Request.Context())
		if err != nil {
			write(c, queue.Response{}, err)
			return
		}
		c.JSON(200, gin.H{"queue_length": length})
	})
}
