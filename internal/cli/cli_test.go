package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/db"
)

func TestCommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"version"}, Version},
		{[]string{"config", "check", "--config", "../../configs/asu.example.yaml", "--listen", ":8400", "--log-level", "debug", "--data-dir", "cli_data"}, "listen: :8400"},
	} {
		t.Run(strings.Join(tc.args[:1], " "), func(t *testing.T) {
			cmd := NewCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("命令输出不符: %s", out.String())
			}
		})
	}
}

func TestDBMigrate(t *testing.T) {
	dir := t.TempDir()
	run := func() error {
		cmd := NewCommand()
		cmd.SetArgs([]string{"db", "migrate", "--data-dir", dir})
		return cmd.Execute()
	}
	for range 2 {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, db.Filename)); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := run(); err == nil || !strings.Contains(err.Error(), "服务未运行") {
		t.Fatalf("离线迁移未拒绝数据库占用: %v", err)
	}
}

func TestUnimplementedDrivers(t *testing.T) {
	for section, name := range map[string]string{"queue": "redis", "store": "s3", "stats": "redis", "runtime": "kubernetes"} {
		t.Run(section, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "asu.yaml")
			if err := os.WriteFile(path, []byte(section+":\n  driver: "+name+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"config", "check"}, {"serve"}} {
				cmd := NewCommand()
				cmd.SetArgs(append(args, "--config", path))
				if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), section+".driver") || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "未实现") {
					t.Fatalf("命令 %v 未明确拒绝未实现驱动: %v", args, err)
				}
			}
		})
	}
	// 直接调用启动入口也必须校验，避免绕过 CLI 时使用无效配置。
	cfg := config.Default()
	cfg.Stats.Driver = "redis"
	if err := runServe(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "stats.driver") {
		t.Fatalf("启动入口缺少驱动校验: %v", err)
	}
}
