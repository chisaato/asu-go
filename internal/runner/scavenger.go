package runner

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/gzzchh/asu-go/internal/runtime"
)

type Scavenger struct {
	Runtime          runtime.Runtime
	Jobs             JobStore
	Interval         time.Duration
	KeepContainerTTL time.Duration
	// MaxKept=0 表示不保留。配置层提供默认值 3。
	MaxKept int
}

func (s *Scavenger) Sweep(ctx context.Context) error {
	// 先列举资源，再读最新活跃任务，避免新建任务落在过时快照之外。
	states, err := s.Runtime.List(ctx)
	if err != nil {
		return err
	}
	tasks, err := s.Jobs.Active(ctx)
	if err != nil {
		return err
	}
	ttl := s.KeepContainerTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	var kept []runtime.State
	var cleanup []runtime.State
	for _, state := range states {
		if state.Labels["managed-by"] != "asu-go" {
			continue
		}
		if activeTask(tasks, state) {
			continue
		}
		if !state.Running && state.Labels["asu.keep_container"] == "true" {
			at := state.FinishedAt
			if at.IsZero() {
				at = state.CreatedAt
			}
			if time.Since(at) < ttl {
				kept = append(kept, state)
				continue
			}
		}
		cleanup = append(cleanup, state)
	}
	// 按结束时间保留最新的诊断环境，运行中任务不参与数量限制。
	sort.Slice(kept, func(i, j int) bool {
		a, b := kept[i].FinishedAt, kept[j].FinishedAt
		if a.IsZero() {
			a = kept[i].CreatedAt
		}
		if b.IsZero() {
			b = kept[j].CreatedAt
		}
		return a.After(b)
	})
	limit := max(s.MaxKept, 0)
	if len(kept) > limit {
		cleanup = append(cleanup, kept[limit:]...)
	}
	for _, state := range cleanup {
		// 每次删除前刷新任务状态；数据库错误时绝不猜测“没有活跃任务”。
		tasks, err := s.Jobs.Active(ctx)
		if err != nil {
			return err
		}
		if activeTask(tasks, state) {
			continue
		}
		if err := s.Runtime.Remove(ctx, state.Handle); err != nil {
			return err
		}
	}
	return nil
}

func activeTask(tasks []Task, state runtime.State) bool {
	for _, task := range tasks {
		if task.Handle == state.Handle || (task.ID != "" && task.ID == state.Labels["asu.request_hash"]) {
			return true
		}
	}
	return false
}

// Run 启动立即回收一次，之后周期执行；没有脱离调用方的协程。
func (s *Scavenger) Run(ctx context.Context) error {
	interval := s.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Sweep(ctx); err != nil {
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
