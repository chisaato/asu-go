// Package runner 编排隔离构建与重启恢复，不依赖 HTTP 或具体容器引擎。
package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stream"
)

type Stage struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}
type Job struct {
	ID, Image, Profile, VersionCode string
	Packages                        []string
	PackageVersions                 map[string]string
	Defaults                        string
	Repositories                    map[string]string
	RepositoriesMode                string
	RepositoryKeys                  []string
	RootFSSizeMB                    int
	Filesystem                      string
	DiffPackages                    bool
	Verbose, KeepContainer, Setup   bool
	PullPolicy                      string
	Rolling                         bool
	Env                             []string
	// ArtifactDir 由调用方提供，建议为空的任务专属临时目录。
	ArtifactDir string
	// OnHandle 必须同步持久化句柄；失败会终止执行并清理。
	OnHandle func(context.Context, runtime.Handle) error
	OnStage  func(context.Context, Stage) error
}
type Result struct {
	Handle      runtime.Handle
	Stages      []Stage
	ExitCode    int64
	ArtifactDir string
}
type Error struct {
	Kind     string
	ExitCode int64
	Tail     string
	Cause    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (exit=%d): %v\n%s", e.Kind, e.ExitCode, e.Cause, e.Tail)
}
func (e *Error) Unwrap() error { return e.Cause }

type Runner struct {
	Runtime        runtime.Runtime
	Hub            *stream.Hub
	JobTimeout     time.Duration
	RollingRecheck time.Duration
	// BuildScript 仅供受信任的组装层注入；nil 保持生产构建脚本。
	BuildScript func(Job) (string, error)
	pullMu      sync.Mutex
	checked     map[string]time.Time
}

func (r *Runner) Run(parent context.Context, j Job) (result Result, err error) {
	return r.execute(parent, j, "")
}

// Resume 不重新创建、注入或启动环境；容器日志从头覆写，随后共享 Run 的收尾。
func (r *Runner) Resume(parent context.Context, j Job, h runtime.Handle) (Result, error) {
	if h == "" {
		return Result{ExitCode: -1}, &Error{Kind: "interrupted", ExitCode: -1, Cause: errors.New("interrupted")}
	}
	s, err := r.Runtime.Inspect(parent, h)
	if err != nil {
		return Result{}, err
	}
	if !s.Exists {
		return Result{Handle: h, ExitCode: -1}, &Error{Kind: "interrupted", ExitCode: -1, Cause: errors.New("interrupted")}
	}
	return r.execute(parent, j, h)
}

func (r *Runner) prepare(ctx context.Context, j Job) error {
	if !j.Rolling {
		return r.Runtime.Prepare(ctx, runtime.ImageSpec{Reference: j.Image, PullPolicy: j.PullPolicy})
	}
	r.pullMu.Lock()
	defer r.pullMu.Unlock()
	interval := r.RollingRecheck
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	policy := j.PullPolicy
	last := r.checked[j.Image]
	if last.IsZero() || time.Since(last) >= interval {
		policy = "always"
	}
	if err := r.Runtime.Prepare(ctx, runtime.ImageSpec{Reference: j.Image, PullPolicy: policy}); err != nil {
		return err
	}
	if r.checked == nil {
		r.checked = make(map[string]time.Time)
	}
	if policy == "always" {
		r.checked[j.Image] = time.Now()
	}
	return nil
}

func (r *Runner) execute(parent context.Context, j Job, existing runtime.Handle) (result Result, err error) {
	if err = stream.ValidateID(j.ID); err != nil {
		return result, err
	}
	if j.ArtifactDir == "" {
		return result, fmt.Errorf("必须指定 ArtifactDir")
	}
	if err = r.Hub.Begin(j.ID); err != nil {
		return result, err
	}
	timeout := r.JobTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	result.ExitCode = -1
	result.Handle = existing
	defer func() {
		cause := ctx.Err()
		if cause != nil {
			err = errors.Join(err, cause)
		}
		if result.Handle != "" {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			if err != nil {
				err = errors.Join(err, r.Runtime.Kill(cleanup, result.Handle))
			}
			if err == nil || !j.KeepContainer || cause != nil {
				err = errors.Join(err, r.Runtime.Remove(cleanup, result.Handle))
			}
		}
		err = errors.Join(err, r.Hub.Finish(j.ID))
		if err != nil {
			kind := "failed"
			if errors.Is(cause, context.DeadlineExceeded) {
				kind = "timeout"
			} else if errors.Is(cause, context.Canceled) {
				kind = "cancelled"
			}
			tail := strings.Join(r.Hub.Tail(j.ID), "")
			if len(tail) > 16384 {
				tail = tail[len(tail)-16384:]
			}
			if strings.Contains(tail, "is too big") || strings.Contains(tail, "out of space") {
				err = fmt.Errorf("%s: %w", "Selected packages exceed device storage", err)
			}
			err = &Error{Kind: kind, ExitCode: result.ExitCode, Tail: tail, Cause: err}
		}
	}()
	if existing == "" {
		buildScript := r.BuildScript
		if buildScript == nil {
			buildScript = Script
		}
		script, scriptErr := buildScript(j)
		if scriptErr != nil {
			return result, scriptErr
		}
		archive, archiveErr := inputs(j)
		if archiveErr != nil {
			return result, archiveErr
		}
		if err = r.prepare(ctx, j); err != nil {
			return result, err
		}
		result.Handle, err = r.Runtime.Create(ctx, runtime.RunSpec{Image: j.Image, Command: []string{"sh", "-c", script}, WorkingDir: "/builder", RequestHash: j.ID, Env: j.Env, KeepContainer: j.KeepContainer})
		if err != nil {
			return result, err
		}
		if j.OnHandle != nil {
			if err = j.OnHandle(ctx, result.Handle); err != nil {
				return result, err
			}
		}
		if err = r.Runtime.PutArchive(ctx, result.Handle, "/builder", bytes.NewReader(archive)); err != nil {
			return result, err
		}
		if err = r.Runtime.Start(ctx, result.Handle); err != nil {
			return result, err
		}
	}
	logs, err := r.Runtime.Logs(ctx, result.Handle, runtime.LogsOptions{Follow: true, Timestamps: existing != ""})
	if err != nil {
		return result, err
	}
	defer func() { _ = logs.Close() }()
	scanner := bufio.NewScanner(logs)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		at := time.Now().UTC()
		if existing != "" {
			if prefix, content, ok := strings.Cut(line, " "); ok {
				if stamp, parseErr := time.Parse(time.RFC3339Nano, prefix); parseErr == nil {
					at, line = stamp, content
				}
			}
		}
		if err = r.Hub.Append(j.ID, line+"\n"); err != nil {
			return result, err
		}
		if name, ok := strings.CutPrefix(line, "##asu:stage="); ok && validStage(name) {
			stage := Stage{Name: name, At: at}
			result.Stages = append(result.Stages, stage)
			if j.OnStage != nil {
				if err = j.OnStage(ctx, stage); err != nil {
					return result, err
				}
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return result, err
	}
	exit, err := r.Runtime.Wait(ctx, result.Handle)
	if err != nil {
		return result, err
	}
	result.ExitCode = exit.Code
	if exit.Code != 0 {
		return result, fmt.Errorf("Error while building firmware")
	}
	if err = extract(ctx, r.Runtime, result.Handle, "/builder/"+j.ID, j.ArtifactDir); err != nil {
		return result, fmt.Errorf("取回产物: %w", err)
	}
	result.ArtifactDir = j.ArtifactDir
	return result, nil
}
func validStage(name string) bool {
	switch name {
	case "init", "container_setup", "validate_revision", "validate_manifest", "building_image":
		return true
	}
	return false
}
