package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestYAMLTags(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !valid.MatchString(field.Tag.Get("yaml")) {
				t.Errorf("%s.%s 缺少合法 YAML 标签", typ.Name(), field.Name)
			}
			nested := field.Type
			if nested.Kind() == reflect.Slice {
				nested = nested.Elem()
			}
			if nested.Kind() == reflect.Struct {
				check(nested)
			}
		}
	}
	check(reflect.TypeFor[Config]())
}

func TestExample(t *testing.T) {
	f, err := os.Open("../../configs/asu.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	cfg, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatal("示例配置与默认值不一致")
	}
}

func TestStrictDecode(t *testing.T) {
	for _, input := range []string{"server:\n  lisen: ':8000'", "log:\n  unknown: true", "{}\n---\n{}"} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Errorf("未拒绝非法配置 %q", input)
		}
	}
}

func TestPriority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asu.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: ':8100'\nlog:\n  level: warn\ndata_dir: yaml_data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 清除宿主环境影响，随后分层验证覆盖。
	for _, key := range []string{"ASU_SERVER_LISTEN", "ASU_LOG_LEVEL", "ASU_DATA_DIR"} {
		old, exists := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	check := func(flags Overrides, listen, level, dir string) {
		t.Helper()
		cfg, err := Load(path, flags)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.Listen != listen || cfg.Log.Level != level || cfg.DataDir != dir || cfg.Log.Format != "json" {
			t.Fatalf("覆盖结果不符: %+v", cfg)
		}
	}
	check(Overrides{}, ":8100", "warn", "yaml_data")
	t.Setenv("ASU_SERVER_LISTEN", ":8200")
	t.Setenv("ASU_LOG_LEVEL", "debug")
	t.Setenv("ASU_DATA_DIR", "env_data")
	check(Overrides{}, ":8200", "debug", "env_data")
	listen, level, dir := ":8300", "error", "flag_data"
	check(Overrides{Listen: &listen, LogLevel: &level, DataDir: &dir}, listen, level, dir)
	dir = ""
	if _, err := Load(path, Overrides{DataDir: &dir}); err == nil || !strings.Contains(err.Error(), "data_dir") {
		t.Fatalf("显式空 flag 必须覆盖环境变量并报错: %v", err)
	}
}

func TestValidationPaths(t *testing.T) {
	for _, tc := range []struct {
		path string
		set  func(*Config)
	}{
		{"server.listen", func(c *Config) { c.Server.Listen = "invalid" }},
		{"log.format", func(c *Config) { c.Log.Format = "invalid" }},
		{"log.level", func(c *Config) { c.Log.Level = "invalid" }},
		{"queue.driver", func(c *Config) { c.Queue.Driver = "redis" }},
		{"store.driver", func(c *Config) { c.Store.Driver = "s3" }},
		{"stats.driver", func(c *Config) { c.Stats.Driver = "redis" }},
		{"runtime.driver", func(c *Config) { c.Runtime.Driver = "kubernetes" }},
		{"runtime.socket", func(c *Config) { c.Runtime.Socket = "" }},
		{"runtime.rolling_recheck", func(c *Config) { c.Runtime.RollingRecheck = 0 }},
		{"runtime.scavenger_interval", func(c *Config) { c.Runtime.ScavengerInterval = -1 }},
		{"queue.job_timeout", func(c *Config) { c.Queue.JobTimeout = 0 }},
		{"admin.session_ttl", func(c *Config) { c.Admin.SessionTTL = 0 }},
		{"admin.users[0].name", func(c *Config) { c.Admin.Users = []AdminUser{{}} }},
		{"admin.users[0].password_hash", func(c *Config) { c.Admin.Users = []AdminUser{{Name: "admin", PasswordHash: "plain"}} }},
	} {
		t.Run(tc.path, func(t *testing.T) {
			cfg := Default()
			tc.set(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("校验错误缺少路径 %s: %v", tc.path, err)
			}
		})
	}
}
