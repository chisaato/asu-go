package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gzzchh/asu-go/internal/queue"
	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stats"
	"github.com/gzzchh/asu-go/internal/store"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      Server      `yaml:"server"`
	Log         Log         `yaml:"log"`
	DataDir     string      `yaml:"data_dir"`
	Admin       Admin       `yaml:"admin"`
	Queue       Queue       `yaml:"queue"`
	Build       Build       `yaml:"build"`
	Store       Store       `yaml:"store"`
	Stats       Stats       `yaml:"stats"`
	Runtime     Runtime     `yaml:"runtime"`
	Metadata    Metadata    `yaml:"metadata"`
	Diagnostics Diagnostics `yaml:"diagnostics"`
}

type Server struct {
	Listen string `yaml:"listen"`
}

type Log struct {
	Format string `yaml:"format"`
	Level  string `yaml:"level"`
}

type Admin struct {
	APIToken      string        `yaml:"api_token"`
	Users         []AdminUser   `yaml:"users"`
	SessionSecret string        `yaml:"session_secret"`
	SessionTTL    time.Duration `yaml:"session_ttl"`
}

type AdminUser struct {
	Name         string `yaml:"name"`
	PasswordHash string `yaml:"password_hash"`
}

type Queue struct {
	Driver              string        `yaml:"driver"`
	MaxConcurrentBuilds int           `yaml:"max_concurrent_builds"`
	MaxPendingJobs      int           `yaml:"max_pending_jobs"`
	BuildTTL            time.Duration `yaml:"build_ttl"`
	BuildUnversionedTTL time.Duration `yaml:"build_unversioned_ttl"`
	BuildDefaultsTTL    time.Duration `yaml:"build_defaults_ttl"`
	BuildFailureTTL     time.Duration `yaml:"build_failure_ttl"`
	JobTimeout          time.Duration `yaml:"job_timeout"`
}

type Build struct {
	AllowDefaults         bool     `yaml:"allow_defaults"`
	MaxDefaultsLength     int      `yaml:"max_defaults_length"`
	MaxCustomRootfsSizeMB int      `yaml:"max_custom_rootfs_size_mb"`
	RepositoryAllowList   []string `yaml:"repository_allow_list"`
}

type Store struct {
	Driver string `yaml:"driver"`
	Dir    string `yaml:"dir"`
}

// StoreDir 延迟解析默认值，确保 data_dir 的环境变量/flag 覆盖生效。
func (c Config) StoreDir() string {
	if c.Store.Dir != "" {
		return c.Store.Dir
	}
	return filepath.Join(c.DataDir, "store")
}

type Stats struct {
	Driver string `yaml:"driver"`
}

type Runtime struct {
	RollingRecheck    time.Duration `yaml:"rolling_recheck"`
	ScavengerInterval time.Duration `yaml:"scavenger_interval"`
	Driver            string        `yaml:"driver"`
	Socket            string        `yaml:"socket"`
	MemoryLimit       int64         `yaml:"memory_limit"`
	CPUs              float64       `yaml:"cpus"`
	PullPolicy        string        `yaml:"pull_policy"`
}

type Metadata struct {
	RefreshInterval time.Duration `yaml:"refresh_interval"`
	CacheTTL        time.Duration `yaml:"cache_ttl"`
}

type Diagnostics struct {
	Retention        time.Duration `yaml:"retention"`
	KeepContainerTTL time.Duration `yaml:"keep_container_ttl"`
	MaxKept          int           `yaml:"max_kept"`
}

func Default() Config {
	return Config{
		Server:      Server{Listen: ":8000"},
		Log:         Log{Format: "json", Level: "info"},
		DataDir:     "data",
		Admin:       Admin{SessionTTL: 24 * time.Hour},
		Queue:       Queue{Driver: "sqlite", MaxConcurrentBuilds: 2, MaxPendingJobs: 200, JobTimeout: 10 * time.Minute, BuildTTL: 7 * 24 * time.Hour, BuildUnversionedTTL: 24 * time.Hour, BuildDefaultsTTL: 30 * time.Minute, BuildFailureTTL: time.Hour},
		Build:       Build{MaxDefaultsLength: 20480, MaxCustomRootfsSizeMB: 1024},
		Store:       Store{Driver: "local"},
		Stats:       Stats{Driver: "sqlite"},
		Runtime:     Runtime{Driver: "docker", Socket: "unix:///var/run/docker.sock", PullPolicy: "missing", RollingRecheck: 6 * time.Hour, ScavengerInterval: 5 * time.Minute},
		Metadata:    Metadata{RefreshInterval: time.Hour, CacheTTL: time.Hour},
		Diagnostics: Diagnostics{Retention: 7 * 24 * time.Hour, KeepContainerTTL: time.Hour, MaxKept: 3},
	}
}

// Overrides 仅包含用户显式设置的 flag，空字符串同样参与覆盖与校验。
type Overrides struct {
	Listen   *string
	LogLevel *string
	DataDir  *string
}

// Decode 保留未指定字段的默认值，拒绝未知键及多个 YAML 文档。
func Decode(r io.Reader) (Config, error) {
	cfg := Default()
	d := yaml.NewDecoder(r)
	d.KnownFields(true)
	if err := d.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("解码配置: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("配置只允许一个 YAML 文档")
	}
	return cfg, nil
}

// Load 显式指定的文件必须存在；否则依次查找当前目录和系统配置。
func Load(path string, flags Overrides) (Config, error) {
	cfg := Default()
	paths := []string{"asu.yaml", "/etc/asu/asu.yaml"}
	if path != "" {
		paths = []string{path}
	}
	for _, candidate := range paths {
		f, err := os.Open(candidate)
		if errors.Is(err, os.ErrNotExist) && path == "" {
			continue
		}
		if err != nil {
			return Config{}, fmt.Errorf("读取配置 %s: %w", candidate, err)
		}
		cfg, err = Decode(f)
		closeErr := f.Close()
		if err != nil {
			return Config{}, fmt.Errorf("配置 %s: %w", candidate, err)
		}
		if closeErr != nil {
			return Config{}, fmt.Errorf("关闭配置 %s: %w", candidate, closeErr)
		}
		break
	}
	for key, dst := range map[string]*string{
		"ASU_SERVER_LISTEN": &cfg.Server.Listen,
		"ASU_LOG_LEVEL":     &cfg.Log.Level,
		"ASU_DATA_DIR":      &cfg.DataDir,
		"ASU_ADMIN_TOKEN":   &cfg.Admin.APIToken,
	} {
		if value, ok := os.LookupEnv(key); ok {
			*dst = value
		}
	}
	if flags.Listen != nil {
		cfg.Server.Listen = *flags.Listen
	}
	if flags.LogLevel != nil {
		cfg.Log.Level = *flags.LogLevel
	}
	if flags.DataDir != nil {
		cfg.DataDir = *flags.DataDir
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if _, port, err := net.SplitHostPort(c.Server.Listen); err != nil {
		return fmt.Errorf("server.listen: %w", err)
	} else if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return errors.New("server.listen: 端口必须为 0 到 65535 的整数")
	}
	if c.Log.Format != "json" && c.Log.Format != "console" {
		return errors.New("log.format: 仅支持 json 或 console")
	}
	if _, err := zapcore.ParseLevel(c.Log.Level); err != nil {
		return fmt.Errorf("log.level: %w", err)
	}
	if c.DataDir == "" {
		return errors.New("data_dir: 不得为空")
	}
	seen := make(map[string]bool)
	for i, user := range c.Admin.Users {
		if strings.TrimSpace(user.Name) == "" || seen[user.Name] {
			return fmt.Errorf("admin.users[%d].name: 不得为空或重复", i)
		}
		seen[user.Name] = true
		if _, err := bcrypt.Cost([]byte(user.PasswordHash)); err != nil {
			return fmt.Errorf("admin.users[%d].password_hash: 必须为 bcrypt 哈希: %w", i, err)
		}
	}
	if _, err := queue.Lookup(c.Queue.Driver); err != nil {
		return fmt.Errorf("queue.driver: %w", err)
	}
	if _, err := store.Lookup(c.Store.Driver); err != nil {
		return fmt.Errorf("store.driver: %w", err)
	}
	if _, err := stats.Lookup(c.Stats.Driver); err != nil {
		return fmt.Errorf("stats.driver: %w", err)
	}
	if _, err := runtime.Lookup(c.Runtime.Driver); err != nil {
		return fmt.Errorf("runtime.driver: %w", err)
	}
	if c.Runtime.Socket == "" {
		return errors.New("runtime.socket: 不得为空")
	}
	if c.Runtime.PullPolicy != "missing" && c.Runtime.PullPolicy != "always" && c.Runtime.PullPolicy != "never" {
		return errors.New("runtime.pull_policy: 仅支持 missing、always 或 never")
	}
	for path, value := range map[string]time.Duration{
		"runtime.rolling_recheck":        c.Runtime.RollingRecheck,
		"runtime.scavenger_interval":     c.Runtime.ScavengerInterval,
		"admin.session_ttl":              c.Admin.SessionTTL,
		"queue.job_timeout":              c.Queue.JobTimeout,
		"queue.build_ttl":                c.Queue.BuildTTL,
		"queue.build_unversioned_ttl":    c.Queue.BuildUnversionedTTL,
		"queue.build_defaults_ttl":       c.Queue.BuildDefaultsTTL,
		"queue.build_failure_ttl":        c.Queue.BuildFailureTTL,
		"metadata.refresh_interval":      c.Metadata.RefreshInterval,
		"metadata.cache_ttl":             c.Metadata.CacheTTL,
		"diagnostics.retention":          c.Diagnostics.Retention,
		"diagnostics.keep_container_ttl": c.Diagnostics.KeepContainerTTL,
	} {
		if value <= 0 {
			return fmt.Errorf("%s: 必须大于 0", path)
		}
	}
	if c.Queue.MaxConcurrentBuilds <= 0 {
		return errors.New("queue.max_concurrent_builds: 必须大于 0")
	}
	if c.Queue.MaxPendingJobs <= 0 {
		return errors.New("queue.max_pending_jobs: 必须大于 0")
	}
	if c.Build.MaxDefaultsLength <= 0 {
		return errors.New("build.max_defaults_length: 必须大于 0")
	}
	if c.Build.MaxCustomRootfsSizeMB <= 0 {
		return errors.New("build.max_custom_rootfs_size_mb: 必须大于 0")
	}
	if c.Diagnostics.MaxKept < 0 {
		return errors.New("diagnostics.max_kept: 不得小于 0")
	}
	if c.Runtime.MemoryLimit < 0 {
		return errors.New("runtime.memory_limit: 不得小于 0")
	}
	if c.Runtime.CPUs < 0 {
		return errors.New("runtime.cpus: 不得小于 0")
	}
	return nil
}
