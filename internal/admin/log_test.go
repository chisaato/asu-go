package admin

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/stream"
)

func TestLogsAuthHistoryFollow(t *testing.T) {
	h, err := stream.NewHub(t.TempDir(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	if err = h.Begin("job"); err != nil {
		t.Fatal(err)
	}
	if err = h.Append("job", "snapshot\n"); err != nil {
		t.Fatal(err)
	}
	jobs := stream.NewMemoryJobSource()
	jobs.Set("job", false)
	router := gin.New()
	RegisterLogs(router.Group("/admin/api", RequireAuth(BearerAuthenticator("secret"))), stream.Service{Hub: h, Jobs: jobs})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/jobs/job/log", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/admin/api/jobs/job/log?follow=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	scanner := bufio.NewScanner(resp.Body)
	if !scanner.Scan() || scanner.Text() != "snapshot" {
		t.Fatal("未实时收到快照")
	}
	if err = h.Append("job", "live\n"); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() || scanner.Text() != "live" {
		t.Fatal("未实时收到新增行")
	}
	if err = h.Finish("job"); err != nil {
		t.Fatal(err)
	}
	jobs.Set("job", true)
	if scanner.Scan() || scanner.Err() != nil {
		t.Fatal("结束后未关闭", scanner.Err())
	}
	h.Forget("job")
	for _, id := range []string{"job", "missing"} {
		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/admin/api/jobs/"+id+"/log", nil)
		req.Header.Set("Authorization", "Bearer secret")
		router.ServeHTTP(w, req)
		if id == "missing" {
			if w.Code != 404 {
				t.Fatal(w.Code)
			}
		} else {
			body, _ := io.ReadAll(w.Body)
			if !strings.Contains(string(body), "snapshot\nlive\n") || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatal(w.Body.String())
			}
		}
	}
}
