package driver_test

import (
	"testing"

	"github.com/gzzchh/asu-go/internal/driver"
	"github.com/gzzchh/asu-go/internal/queue"
	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stats"
	"github.com/gzzchh/asu-go/internal/store"
)

func TestRegistry(t *testing.T) {
	var r driver.Registry[driver.Name]
	if err := r.Register("test"); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Lookup("test"); err != nil || got.Name() != "test" {
		t.Fatalf("注册驱动未找到: %v, %v", got, err)
	}
	if err := r.Register("test"); err == nil {
		t.Fatal("未拒绝重复注册")
	}
	if err := r.Register(""); err == nil {
		t.Fatal("未拒绝空名称")
	}
	if _, err := r.Lookup("missing"); err == nil {
		t.Fatal("未知驱动未报错")
	}
}

func TestBuiltins(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lookup  func(string) (string, error)
		valid   []string
		invalid string
	}{
		{"queue", func(n string) (string, error) {
			d, err := queue.Lookup(n)
			if err != nil {
				return "", err
			}
			return d.Name(), nil
		}, []string{"sqlite"}, "redis"},
		{"store", func(n string) (string, error) {
			d, err := store.Lookup(n)
			if err != nil {
				return "", err
			}
			return d.Name(), nil
		}, []string{"local"}, "s3"},
		{"stats", func(n string) (string, error) {
			d, err := stats.Lookup(n)
			if err != nil {
				return "", err
			}
			return d.Name(), nil
		}, []string{"sqlite"}, "redis"},
		{"runtime", func(n string) (string, error) {
			d, err := runtime.Lookup(n)
			if err != nil {
				return "", err
			}
			return d.Name(), nil
		}, []string{"docker", "podman"}, "kubernetes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range tc.valid {
				if got, err := tc.lookup(name); err != nil || got != name {
					t.Fatalf("选择 %s: %q, %v", name, got, err)
				}
			}
			if _, err := tc.lookup(tc.invalid); err == nil {
				t.Fatal("未实现的驱动未报错")
			}
		})
	}
}
