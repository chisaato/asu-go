package cli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/admin"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/sources"
)

func TestSourceCommandsHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	admin.RegisterSources(router.Group("/admin/api", admin.RequireAuth(admin.BearerAuthenticator("secret"))), sources.NewStore(database))
	server := httptest.NewServer(router)
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "asu.yaml")
	if err := os.WriteFile(configPath, []byte("admin:\n  api_token: secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(input string, args ...string) (string, error) {
		cmd := NewCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetIn(strings.NewReader(input))
		cmd.SetArgs(append([]string{"--server", server.URL, "--config", configPath, "source"}, args...))
		err := cmd.Execute()
		return output.String(), err
	}
	if _, err := run("", "add", "--distribution", "custom", "--version", "1.0.0", "--branch", "1.0", "--upstream-url", "https://example.com", "--image", "example/imagebuilder"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"edit", "1", "--image", "custom/builder"}, {"disable", "1"}, {"enable", "1"}} {
		if _, err := run("", args...); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := run("", "list")
	if err != nil || !strings.Contains(listed, "custom/builder") || !strings.Contains(listed, "enabled: true") {
		t.Fatalf("列表: %s %v", listed, err)
	}
	exported, err := run("", "export")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(exported, "import", "-"); err != nil {
		t.Fatal(err)
	}
	again, err := run("", "export")
	if err != nil || exported != again {
		t.Fatalf("导入导出不一致: %s %s %v", exported, again, err)
	}
	before, err := sources.DecodeCatalog(strings.NewReader(exported))
	if err != nil {
		t.Fatal(err)
	}
	before[0].Image = "imported/builder"
	var imported bytes.Buffer
	if err := sources.Export(&imported, before); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(file, imported.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run("", "import", file); err != nil {
		t.Fatal(err)
	}
	updated, err := sources.NewStore(database).Get(context.Background(), 1)
	if err != nil || updated.Image != "imported/builder" {
		t.Fatalf("upsert 失败: %+v %v", updated, err)
	}
	if _, err := run("", "list", "--token", "wrong"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("错误 token: %v", err)
	}
	t.Setenv("ASU_ADMIN_TOKEN", "wrong")
	if _, err := run("", "list"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("环境 token 未覆盖配置: %v", err)
	}
	if _, err := run("", "list", "--token", "secret"); err != nil {
		t.Fatalf("flag token 未覆盖环境: %v", err)
	}
	t.Setenv("ASU_ADMIN_TOKEN", "secret")
	if _, err := run("", "list", "--token", ""); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("显式空 token 未覆盖环境: %v", err)
	}
	if !reflect.DeepEqual(updated.Overrides, before[0].Overrides) {
		t.Fatalf("已有 overrides 未保留: %v", updated.Overrides)
	}
}
