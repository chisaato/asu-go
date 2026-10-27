package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerConfiguration(t *testing.T) {
	cfg, err := Decode(strings.NewReader("data_dir: custom\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Runner.Verbose || !cfg.Runner.Cache.Enabled || cfg.Runner.Cache.MaxSizeGB != 20 || cfg.CacheDir() != filepath.Join("custom", "cache") {
		t.Fatal(cfg.Runner, cfg.CacheDir())
	}
	for _, body := range []string{"runner:\n  cache:\n    max_size_gb: -1\n", "runner:\n  cache:\n    max_size_gb: 9223372036854775807\n"} {
		cfg, err := Decode(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("接受无效容量")
		}
	}
	cfg, err = Decode(strings.NewReader("runner:\n  verbose: false\n  cache:\n    enabled: false\n    dir: /cache\n    max_size_gb: 0\n"))
	if err != nil || cfg.Validate() != nil || cfg.Runner.Verbose || cfg.Runner.Cache.Enabled || cfg.CacheDir() != "/cache" {
		t.Fatal(cfg.Runner, err)
	}
	if _, err := Decode(strings.NewReader("runner:\n  cache:\n    max_size: 20\n")); err == nil {
		t.Fatal("未严格解码")
	}
	for _, filename := range []string{"asu.example.yaml", "asu.compose.yaml"} {
		f, err := os.Open(filepath.Join("../../configs", filename))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := Decode(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "asu.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASU_DATA_DIR", "environment")
	cfg, err = Load(path, Overrides{})
	if err != nil || cfg.CacheDir() != filepath.Join("environment", "cache") {
		t.Fatal(cfg.CacheDir(), err)
	}
}
