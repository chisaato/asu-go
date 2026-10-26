package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreDirectory(t *testing.T) {
	cfg := Default()
	cfg.DataDir = "custom-data"
	if cfg.StoreDir() != filepath.Join("custom-data", "store") {
		t.Fatal(cfg.StoreDir())
	}
	cfg, err := Decode(strings.NewReader("store:\n  dir: /custom/artifacts\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StoreDir() != "/custom/artifacts" {
		t.Fatal(cfg.StoreDir())
	}
	for _, driver := range []string{"s3", "unknown"} {
		cfg.Store.Driver = driver
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "store.driver") {
			t.Fatal(err)
		}
	}
}
