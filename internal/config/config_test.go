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

func TestDefaultDistro(t *testing.T) {
	if Default().Server.DefaultDistro != "openwrt" {
		t.Fatal("默认发行版必须为 openwrt")
	}
	for _, distro := range []string{"immortalwrt", "admin", "api", "json", "store", "healthz", "stats", "bad/name", ""} {
		cfg, err := Decode(strings.NewReader("server:\n  default_distro: '" + distro + "'\n"))
		if err != nil {
			t.Fatal(err)
		}
		err = cfg.Validate()
		if distro == "immortalwrt" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "server.default_distro") {
			t.Fatalf("未拒绝默认发行版 %q: %v", distro, err)
		}
	}
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

func TestCORSConfig(t *testing.T) {
	for _, tc := range []struct {
		input  string
		public []string
		admin  []string
	}{
		{"{}", []string{"*"}, []string{}},
		{"server:\n  cors:\n    public_origins: []", []string{}, []string{}},
		{"server:\n  cors:\n    public_origins: [https://selector.example]\n    admin_origins: [https://admin.example]", []string{"https://selector.example"}, []string{"https://admin.example"}},
	} {
		cfg, err := Decode(strings.NewReader(tc.input))
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.Server.CORS, CORS{PublicOrigins: tc.public, AdminOrigins: tc.admin}) {
			t.Fatalf("CORS 配置不符: %+v", cfg.Server.CORS)
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
		{"server.cors.admin_origins", func(c *Config) { c.Server.CORS.AdminOrigins = []string{"https://admin.example", "*"} }},
		{"log.format", func(c *Config) { c.Log.Format = "invalid" }},
		{"log.level", func(c *Config) { c.Log.Level = "invalid" }},
		{"log.file", func(c *Config) { c.Log.File = "\x00invalid" }},
		{"queue.driver", func(c *Config) { c.Queue.Driver = "redis" }},
		{"store.driver", func(c *Config) { c.Store.Driver = "s3" }},
		{"cleaner.interval", func(c *Config) { c.Cleaner.Interval = 0 }},
		{"cleaner.orphan_grace", func(c *Config) { c.Cleaner.OrphanGrace = -1 }},
		{"stats.driver", func(c *Config) { c.Stats.Driver = "redis" }},
		{"runtime.driver", func(c *Config) { c.Runtime.Driver = "kubernetes" }},
		{"runtime.socket", func(c *Config) { c.Runtime.Socket = "" }},
		{"runtime.rolling_recheck", func(c *Config) { c.Runtime.RollingRecheck = 0 }},
		{"runtime.scavenger_interval", func(c *Config) { c.Runtime.ScavengerInterval = -1 }},
		{"queue.job_timeout", func(c *Config) { c.Queue.JobTimeout = 0 }},
		{"queue.max_pending_jobs", func(c *Config) { c.Queue.MaxPendingJobs = 0 }},
		{"queue.build_ttl", func(c *Config) { c.Queue.BuildTTL = 0 }},
		{"queue.build_unversioned_ttl", func(c *Config) { c.Queue.BuildUnversionedTTL = 0 }},
		{"queue.build_defaults_ttl", func(c *Config) { c.Queue.BuildDefaultsTTL = 0 }},
		{"queue.build_failure_ttl", func(c *Config) { c.Queue.BuildFailureTTL = 0 }},
		{"build.max_defaults_length", func(c *Config) { c.Build.MaxDefaultsLength = 0 }},
		{"build.max_custom_rootfs_size_mb", func(c *Config) { c.Build.MaxCustomRootfsSizeMB = 0 }},
		{"admin.session_ttl", func(c *Config) { c.Admin.SessionTTL = 0 }},
		{"metadata.cache_ttl", func(c *Config) { c.Metadata.CacheTTL = 0 }},
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

func TestLogFileConfig(t *testing.T) {
	for _, path := range []string{"", "/tmp/opencode/asu.log", "logs/asu.log"} {
		cfg, err := Decode(strings.NewReader("log:\n  file: '" + path + "'\n"))
		if err != nil || cfg.Log.File != path {
			t.Fatal(cfg.Log, err)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{" ", "\n", ".", "/", "logs/"} {
		cfg := Default()
		cfg.Log.File = path
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "log.file") {
			t.Fatalf("未拒绝无效文件路径 %q: %v", path, err)
		}
	}
	f, err := os.Open("../../configs/asu.compose.yaml")
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
}
