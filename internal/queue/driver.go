// Package queue 提供持久化任务队列与驱动注册。
package queue

import "github.com/chisaato/asu-go/internal/driver"

type Driver interface{ Name() string }

var registry driver.Registry[Driver]

func init() { _ = Register(&SQLite{}) }

func (*SQLite) Name() string { return "sqlite" }

func Register(d Driver) error            { return registry.Register(d) }
func Lookup(name string) (Driver, error) { return registry.Lookup(name) }
