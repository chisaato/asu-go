// Package queue 定义队列驱动注册；任务操作在队列实现阶段补齐。
package queue

import "github.com/gzzchh/asu-go/internal/driver"

type Driver interface{ Name() string }

var registry driver.Registry[Driver]

func init() { _ = Register(driver.Name("sqlite")) }

func Register(d Driver) error            { return registry.Register(d) }
func Lookup(name string) (Driver, error) { return registry.Lookup(name) }
