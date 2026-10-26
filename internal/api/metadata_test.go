package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/api"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/metadata"
	"github.com/gzzchh/asu-go/internal/sources"
	"go.uber.org/zap"
)

func fixture(t *testing.T, url string) (*sources.Store, *db.DB) {
	t.Helper()
	database, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := sources.NewStore(database)
	source := sources.NewSource()
	source.Distribution, source.Branch, source.Version = "openwrt", "24.10", "24.10.1"
	source.Image, source.UpstreamURL, source.Origin = "ghcr.io/openwrt/imagebuilder", url, "upstream"
	source.Channel = "stable"
	if err := store.Import(context.Background(), []sources.Source{source}); err != nil {
		t.Fatal(err)
	}
	return store, database
}

func equalJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("JSON %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(aa) != string(bb) {
		t.Fatalf("实际 %s\n期望 %s", aa, bb)
	}
}

func TestMetadataSyncAndCompatibility(t *testing.T) {
	ctx := context.Background()
	var fail atomic.Bool
	var conditional atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "asu-go/test" {
			t.Error("缺少 User-Agent")
		}
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		if strings.HasSuffix(r.URL.Path, "profiles.json") {
			if strings.Contains(r.URL.Path, "missing") {
				w.WriteHeader(404)
				return
			}
			_, _ = fmt.Fprint(w, `{"version_code":"r27990-abc","profiles":{}}`)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` && r.Header.Get("If-Modified-Since") == "Wed, 21 Oct 2015 07:28:00 GMT" {
			conditional.Add(1)
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		if r.URL.Path == "/.versions.json" {
			_, _ = fmt.Fprint(w, `{"upcoming_version":"24.10.3-rc1","stable_version":"24.10.2","oldstable_version":"24.10.1","versions_list":["24.10.2","24.10.1","23.05.6","24.10.2"]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ath79/generic":"mips_24kc"}`)
	}))
	defer upstream.Close()
	store, database := fixture(t, upstream.URL)
	list, _ := store.List(ctx)
	// 本地修改必须在同步事务中保留；镜像和 enabled 不能被默认值重置。
	if _, err := store.Patch(ctx, list[0].ID, map[string]json.RawMessage{"image": json.RawMessage(`"custom/image"`), "tag_template": json.RawMessage(`"fixed"`)}); err != nil {
		t.Fatal(err)
	}
	local := list[0]
	local.ID = 0
	local.Version = "24.10.9"
	local.Origin = "local"
	local.Channel = ""
	local.Targets = map[string]any{"local": "keep"}
	if err := store.Import(ctx, []sources.Source{local}); err != nil {
		t.Fatal(err)
	}
	client := metadata.NewClient("asu-go/test")
	syncer := &metadata.Syncer{Store: store, Client: client, Logger: zap.NewNop()}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("版本过滤/去重错误: %+v", list)
	}
	for _, source := range list {
		switch source.Version {
		case "24.10.1":
			if source.Image != "custom/image" || source.TagTemplate != "fixed" || source.Channel != "oldstable" || source.SyncedAt == nil {
				t.Fatalf("覆盖或更新错误: %+v", source)
			}
		case "24.10.9":
			if source.Origin != "local" || source.SyncedAt != nil || source.Targets["local"] != "keep" {
				t.Fatalf("本地 source 被修改: %+v", source)
			}
		default:
			if source.Origin != "upstream" || source.Image != "ghcr.io/openwrt/imagebuilder" || source.TagTemplate != "{target}-{version_tag}" || len(source.Overrides) != 0 || source.SyncedAt == nil {
				t.Fatalf("新版本未继承默认值: %+v", source)
			}
		}
	}
	var state string
	if err := database.QueryRowContext(ctx, "SELECT value FROM kv WHERE key='metadata.last_sync'").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state, `"failures":0`) {
		t.Fatal(state)
	}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if conditional.Load() < 5 {
		t.Fatalf("未使用条件请求: %d", conditional.Load())
	}
	before, _ := json.Marshal(mustList(t, store))
	fail.Store(true)
	if err := syncer.Sync(ctx); err == nil {
		t.Fatal("上游失败必须返回错误")
	}
	after, _ := json.Marshal(mustList(t, store))
	if string(before) != string(after) {
		t.Fatal("失败覆盖了旧目录")
	}
	fail.Store(false)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.RegisterMetadata(router, &metadata.Service{Store: store, Client: client, Version: "test"})
	get := func(path string, status int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	equalJSON(t, get("/json/v1/latest.json", 200), `{"latest":["24.10.3-rc1","24.10.2","24.10.1"]}`)
	// 下列字段逐项抄录自原版 config.release/main.generate_branches，而非用待测结构生成。
	branch := `{"path":"releases/{version}","enabled":true,"snapshot":false,"path_packages":"DEPRECATED","branch_off_rev":27990,"package_changes":[{"source":"firewall","target":"firewall4","revision":18611},{"source":"kmod-nft-nat6","revision":20282,"mandatory":true},{"source":"libustream-wolfssl","target":"libustream-mbedtls","revision":21994},{"source":"px5g-wolfssl","target":"px5g-mbedtls","revision":21994},{"source":"wpad-basic-wolfssl","target":"wpad-basic-mbedtls","revision":21994},{"source":"luci-app-diag-core","revision":25984,"mandatory":true},{"source":"auc","target":"owut","revision":26792},{"source":"luci-app-opkg","target":"luci-app-package-manager","revision":27897}],"name":"24.10","versions":["24.10.9","24.10.3-rc1","24.10.2","24.10.1","24.10-SNAPSHOT"],"targets":{"local":"keep"}}`
	equalJSON(t, get("/json/v1/branches.json", 200), "["+branch+"]")
	equalJSON(t, get("/json/v1/overview.json", 200), `{"latest":["24.10.3-rc1","24.10.2","24.10.1"],"branches":{"24.10":`+branch+`},"upstream_url":"`+upstream.URL+`","server":{"version":"test","contact":"mail@aparcar.org","allow_defaults":false,"repository_allow_list":[],"max_custom_rootfs_size_mb":1024,"max_defaults_length":20480}}`)
	equalJSON(t, get("/api/v1/revision/24.10.2/ath79/generic", 200), `{"revision":"r27990-abc"}`)
	equalJSON(t, get("/api/v1/revision/24.10.99/ath79/generic", 200), `{"revision":"r27990-abc"}`)
	equalJSON(t, get("/api/v1/revision/99.99.0/ath79/generic", 400), `{"detail":"Unsupported version: 99.99.0","status":400}`)
	equalJSON(t, get("/api/v1/revision/24.10.2/ath79/missing", 404), `{"detail":"Failed to fetch revision for 24.10.2/ath79/missing","status":404}`)
	for _, name := range []string{"latest", "overview"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/"+name, nil))
		if w.Code != 301 || w.Header().Get("Location") != "/json/v1/"+name+".json" {
			t.Fatal(w)
		}
	}
}

func mustList(t *testing.T, store *sources.Store) []sources.Source {
	t.Helper()
	list, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestTargetsFailureAndRunCancellation(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.versions.json" {
			_, _ = fmt.Fprint(w, `{"upcoming_version":"","stable_version":"24.10.2","oldstable_version":"","versions_list":["24.10.1","24.10.2"]}`)
			return
		}
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		_, _ = fmt.Fprint(w, `{"old":"target"}`)
	}))
	defer server.Close()
	store, _ := fixture(t, server.URL)
	syncer := &metadata.Syncer{Store: store, Client: metadata.NewClient("test"), Logger: zap.NewNop()}
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(mustList(t, store))
	fail.Store(true)
	if err := syncer.Sync(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	after, _ := json.Marshal(mustList(t, store))
	if string(before) != string(after) {
		t.Fatal("target 失败未保留旧数据")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := syncer.Run(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
}
