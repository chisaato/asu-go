// Package store 定义产物存储驱动注册；文件操作在存储实现阶段补齐。
package store

import "github.com/gzzchh/asu-go/internal/driver"

type Driver interface{ Name() string }

var registry driver.Registry[Driver]

func init() { _ = Register(driver.Name("local")) }

func Register(d Driver) error            { return registry.Register(d) }
func Lookup(name string) (Driver, error) { return registry.Lookup(name) }
