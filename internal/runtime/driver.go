// Package runtime 定义与引擎无关的隔离构建生命周期。
package runtime

import "github.com/gzzchh/asu-go/internal/driver"

type Driver interface {
	Name() string
	Open(Options) (Runtime, error)
}

var registry driver.Registry[Driver]

func init() {
	_ = Register(engineDriver("docker"))
	_ = Register(engineDriver("podman"))
}

func Register(d Driver) error            { return registry.Register(d) }
func Lookup(name string) (Driver, error) { return registry.Lookup(name) }
