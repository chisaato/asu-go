// Package store 定义产物存储接口与本地安全存储实现。
package store

import "github.com/chisaato/asu-go/internal/driver"

type Driver interface{ Name() string }

var registry driver.Registry[Driver]

func init() { _ = Register(driver.Name("local")) }

func Register(d Driver) error            { return registry.Register(d) }
func Lookup(name string) (Driver, error) { return registry.Lookup(name) }
