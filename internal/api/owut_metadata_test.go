package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/chisaato/asu-go/internal/api"
	"github.com/chisaato/asu-go/internal/metadata"
	"github.com/chisaato/asu-go/internal/sources"
)

func capturedOWUT(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/owut/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

type owutUpstream struct {
	t         *testing.T
	resources map[string][]byte
}

func (u owutUpstream) RoundTrip(r *http.Request) (*http.Response, error) {
	body, ok := u.resources[r.URL.Path]
	status := http.StatusOK
	if !ok {
		// 抓包只有 ASU 已合并的索引，没有原始 profiles/kmods；404 表示不再合并。
		if !strings.HasSuffix(r.URL.Path, "/profiles.json") {
			u.t.Errorf("未声明的上游请求: %s", r.URL)
		}
		status = http.StatusNotFound
	}
	if r.URL.Host != "downloads.immortalwrt.org" {
		u.t.Errorf("元数据上游串发行版: %s", r.URL)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

func TestOWUTCapturedMetadataTransactions(t *testing.T) {
	ctx := context.Background()
	var overview metadata.Overview
	if err := json.Unmarshal(capturedOWUT(t, "overview.json"), &overview); err != nil {
		t.Fatal(err)
	}
	catalog, _ := fixture(t, "https://downloads.openwrt.org")
	// 故意放入更高的 OpenWrt 版本，确保真实 ImmortalWrt latest 不被污染。
	openwrt := mustList(t, catalog)[0]
	openwrt.Version, openwrt.Branch, openwrt.Channel = "25.12.6", "25.12", "stable"
	all := []sources.Source{openwrt}
	for name, branch := range overview.Branches {
		for _, version := range branch.Versions {
			s := sources.NewSource()
			s.Distribution, s.Branch, s.Version = "immortalwrt", name, version
			s.Image, s.UpstreamURL = "fake/imagebuilder", overview.UpstreamURL
			s.Targets, s.Rolling = branch.Targets, strings.Contains(version, "SNAPSHOT")
			for i, latest := range overview.Latest {
				if version == latest {
					s.Channel = []string{"stable", "oldstable"}[i]
				}
			}
			all = append(all, s)
		}
	}
	if err := catalog.Import(ctx, all); err != nil {
		t.Fatal(err)
	}

	resources := map[string][]byte{}
	for _, version := range []string{"24.10.6", "25.12.2"} {
		var index map[string]any
		if err := json.Unmarshal(capturedOWUT(t, "target-index-"+version+".json"), &index); err != nil {
			t.Fatal(err)
		}
		arch := index["architecture"].(string)
		// 夹具是 ASU 响应而非上游原始抓包；为 fake 上游添加协议包装，不改夹具值。
		// 上游 v2 索引携带 version，ASU 返回时会去掉；单个 fake feed 代表合并后的 feeds。
		index["version"] = 2
		encoded, err := json.Marshal(index)
		if err != nil {
			t.Fatal(err)
		}
		base := "/releases/" + version
		resources[base+"/targets/mediatek/filogic/packages/index.json"] = encoded
		resources[base+"/packages/"+arch+"/feeds.conf"] = []byte("src/gz captured https://unused.invalid/captured\n")
		var packages map[string]string
		if err := json.Unmarshal(capturedOWUT(t, "packages-"+version+".json"), &packages); err != nil {
			t.Fatal(err)
		}
		encoded, err = json.Marshal(map[string]any{"version": 2, "architecture": arch, "packages": packages})
		if err != nil {
			t.Fatal(err)
		}
		resources[base+"/packages/"+arch+"/captured/index.json"] = encoded
	}
	client := metadata.NewClient("owut-fixture-test")
	client.HTTP = &http.Client{Transport: owutUpstream{t: t, resources: resources}}
	service := &metadata.Service{Store: catalog, Client: client, Version: overview.Server.Version,
		Cache: &metadata.Cache{Dir: t.TempDir(), Client: client}}
	router := gin.New()
	router.Use(api.DistroScope(catalog, "openwrt"))
	api.RegisterMetadata(router.Group("/:distro"), service)
	var transactions []struct {
		Stream  int    `json:"stream"`
		Method  string `json:"method"`
		Path    string `json:"path"`
		Status  int    `json:"status"`
		Request struct {
			Headers map[string]string `json:"headers"`
		} `json:"request"`
		Response struct {
			BodyFile string `json:"body_file"`
		} `json:"response"`
	}
	if err := json.Unmarshal(capturedOWUT(t, "transactions.json"), &transactions); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, transaction := range transactions {
		if !strings.Contains(transaction.Path, "/json/") {
			continue
		}
		count++
		t.Run(transaction.Path, func(t *testing.T) {
			request := httptest.NewRequest(transaction.Method, transaction.Path, nil)
			for key, value := range transaction.Request.Headers {
				request.Header.Set(key, value)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			if w.Code != transaction.Status {
				t.Fatalf("流 %d: %d %s", transaction.Stream, w.Code, w.Body)
			}
			// fake 数据源已采用同样的最小片段，完整比较响应，不对待测结果做字段修正。
			equalJSON(t, w.Body.Bytes(), string(capturedOWUT(t, transaction.Response.BodyFile)))
		})
	}
	if count != 9 {
		t.Fatalf("元数据事务数量=%d，期望 9", count)
	}
	// 抓包没有独立 latest/revision/profile 请求；latest 期望直接取真实 overview 的字段。
	// 不将其他发行版（例如 OpenWrt 25.12.6）混入 latest，也不伪造缺失的 revision/profile 抓包。
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/immortalwrt/json/v1/latest.json", nil))
	if w.Code != 200 {
		t.Fatalf("latest: %d %s", w.Code, w.Body)
	}
	want, err := json.Marshal(map[string]any{"latest": overview.Latest})
	if err != nil {
		t.Fatal(err)
	}
	equalJSON(t, w.Body.Bytes(), string(want))
}
