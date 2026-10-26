package runner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stream"
	"golang.org/x/sync/errgroup"
)

// Task 是恢复/回收所需的最小持久状态，不包含构建请求模型。
type Task struct {
	ID            string
	Handle        runtime.Handle
	Status        string
	KeepContainer bool
}

type JobStore interface {
	// Active 同时返回 queued/running，避免回收 Create 与持久化 Handle 间的资源。
	Active(context.Context) ([]Task, error)
	Complete(context.Context, string, Result, error) error
}

type SQLiteJobStore struct{ DB *sql.DB }

func (s *SQLiteJobStore) Active(ctx context.Context) ([]Task, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT request_hash,status,COALESCE(handle,'null'),COALESCE(options,'{}') FROM jobs WHERE status IN ('queued','running')`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tasks []Task
	for rows.Next() {
		var t Task
		var h, options string
		if err := rows.Scan(&t.ID, &t.Status, &h, &options); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(h), &t.Handle); err != nil {
			// 接受设计文档的 {driver,id} 包装，id 仍然只作为不透明值传递。
			var envelope struct {
				ID runtime.Handle `json:"id"`
			}
			if err := json.Unmarshal([]byte(h), &envelope); err != nil {
				return nil, fmt.Errorf("任务 %s handle: %w", t.ID, err)
			}
			t.Handle = envelope.ID
		}
		var opts struct {
			KeepContainer bool `json:"keep_container"`
		}
		if err := json.Unmarshal([]byte(options), &opts); err != nil {
			return nil, err
		}
		t.KeepContainer = opts.KeepContainer
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// SaveHandle 可直接作为 Job.OnHandle 回调使用。必须在 Create 前写入 running 记录。
func (s *SQLiteJobStore) SaveHandle(ctx context.Context, id string, h runtime.Handle) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE jobs SET handle=? WHERE request_hash=? AND status='running'`, string(data), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("任务 %s 未处于 running", id)
	}
	return err
}

func (s *SQLiteJobStore) Complete(ctx context.Context, id string, result Result, runErr error) error {
	status, detail, message := "done", "", ""
	if runErr != nil {
		status, detail, message = "failed", "failed", runErr.Error()
		var failure *Error
		if errors.As(runErr, &failure) {
			detail = failure.Kind
		}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	stages, err := json.Marshal(result.Stages)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE jobs SET status=?, detail=?, error=?, result=?, stages=?, finished_at=? WHERE request_hash=? AND status='running'`, status, detail, message, string(data), string(stages), time.Now().UTC(), id)
	return err
}

// State 同时适配日志接口，历史记录无需驻留内存。
func (s *SQLiteJobStore) State(ctx context.Context, id string) (bool, bool, error) {
	var status string
	err := s.DB.QueryRowContext(ctx, `SELECT status FROM jobs WHERE request_hash=?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return err == nil, status == "done" || status == "failed", err
}

// Takeover 本身阻塞到所有接管完成；由 serve 的 errgroup 持有，取消会清理在途容器。
// ArtifactRoot 是恢复产物的暂存根目录，发布/缓存由 build-api 的 JobStore.Complete 实现接续。
func (r *Runner) Takeover(ctx context.Context, store JobStore, artifactRoot string) error {
	tasks, err := store.Active(ctx)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := stream.ValidateID(task.ID); err != nil {
			return err
		}
	}
	g, groupCtx := errgroup.WithContext(ctx)
	for _, task := range tasks {
		if task.Status != "running" {
			continue
		}
		g.Go(func() error {
			runCtx := groupCtx
			if controller, ok := store.(interface {
				ExecutionContext(context.Context, string) (context.Context, func())
			}); ok {
				var done func()
				runCtx, done = controller.ExecutionContext(groupCtx, task.ID)
				defer done()
			}
			j := Job{ID: task.ID, ArtifactDir: filepath.Join(artifactRoot, task.ID), KeepContainer: task.KeepContainer}
			if recorder, ok := store.(interface {
				RecordStage(context.Context, string, Stage) error
			}); ok {
				j.OnStage = func(ctx context.Context, stage Stage) error { return recorder.RecordStage(ctx, task.ID, stage) }
			}
			result, runErr := r.Resume(runCtx, j, task.Handle)
			// 查询失败不等价于环境不存在，不把暂时不可用的运行时标记为构建失败。
			if runErr != nil {
				var failure *Error
				if !errors.As(runErr, &failure) {
					return runErr
				}
			}
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			return store.Complete(persistCtx, task.ID, result, runErr)
		})
	}
	return g.Wait()
}
