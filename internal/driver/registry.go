// Package driver 提供驱动描述符注册，不在此阶段构造业务实现。
package driver

import (
	"fmt"
	"sync"
)

// Name 是内置驱动占位描述符；具体业务接口由对应实现阶段补齐。
type Name string

func (n Name) Name() string { return string(n) }

type Registry[T interface{ Name() string }] struct {
	mu      sync.RWMutex
	drivers map[string]T
}

func (r *Registry[T]) Register(d T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := d.Name()
	if name == "" {
		return fmt.Errorf("驱动名称不得为空")
	}
	if r.drivers == nil {
		r.drivers = make(map[string]T)
	}
	if _, exists := r.drivers[name]; exists {
		return fmt.Errorf("驱动 %q 已注册", name)
	}
	r.drivers[name] = d
	return nil
}

func (r *Registry[T]) Lookup(name string) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if d, ok := r.drivers[name]; ok {
		return d, nil
	}
	var zero T
	return zero, fmt.Errorf("驱动 %q 未实现或未注册", name)
}
