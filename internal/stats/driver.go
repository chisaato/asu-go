// Package stats 定义统计驱动注册；计数操作在统计实现阶段补齐。
package stats

import "github.com/gzzchh/asu-go/internal/driver"

type Driver interface{ Name() string }

var registry driver.Registry[Driver]

func init() { _ = Register(driver.Name("sqlite")) }

func Register(d Driver) error            { return registry.Register(d) }
func Lookup(name string) (Driver, error) { return registry.Lookup(name) }
