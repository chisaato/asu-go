// Package runtime 仅预留运行时注册机制；完整 Runtime 生命周期接口由 runtime-runner/01 定义。
package runtime

import "github.com/gzzchh/asu-go/internal/driver"

// Driver 是注册描述符，不代表已能执行构建。
type Driver interface{ Name() string }

var registry driver.Registry[Driver]

func init() {
	_ = Register(driver.Name("docker"))
	_ = Register(driver.Name("podman"))
}

func Register(d Driver) error            { return registry.Register(d) }
func Lookup(name string) (Driver, error) { return registry.Lookup(name) }
