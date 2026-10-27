// Package runner 编排隔离构建与重启恢复，不依赖 HTTP 或具体容器引擎。
package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	logging "github.com/gzzchh/asu-go/internal/logcontext"
	"github.com/gzzchh/asu-go/internal/runtime"
	"github.com/gzzchh/asu-go/internal/stream"
	"go.uber.org/zap"
)

type Stage struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}
type Job struct {
	Distribution, Version, Target   string
	ID, Image, Profile, VersionCode string
	// WorkingDir 由 Runner 从镜像或接管容器元数据填入，不接受公开请求覆盖。
	WorkingDir                    string
	inputUID, inputGID            int
	Packages                      []string
	PackageVersions               map[string]string
	Defaults                      string
	Repositories                  map[string]string
	RepositoriesMode              string
	RepositoryKeys                []string
	RootFSSizeMB                  int
	Filesystem                    string
	DiffPackages                  bool
	Verbose, KeepContainer, Setup bool
	PullPolicy                    string
	Rolling                       bool
	Env                           []string
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
	Logger         *zap.Logger
	Runtime        runtime.Runtime
	Hub            *stream.Hub
	JobTimeout     time.Duration
	RollingRecheck time.Duration
	// BuildScript 仅供受信任的组装层注入；nil 保持生产构建脚本。
	BuildScript func(Job) (string, error)
	pullMu      sync.Mutex
	checked     map[string]time.Time
	images      map[string]runtime.ImageMetadata
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
		logging.OrNop(r.Logger).Warn("接管环境检查失败", zap.String("component", "runner"), zap.String("request_hash", j.ID), zap.String("container", string(h)), zap.Error(err))
		if parent.Err() != nil {
			// 取消可能先于接管的 Inspect；仍走统一 Kill/Remove 收尾。
			return r.execute(parent, j, h)
		}
		return Result{}, err
	}
	if !s.Exists {
		logging.OrNop(r.Logger).Warn("接管环境已不存在", zap.String("component", "runner"), zap.String("request_hash", j.ID), zap.String("container", string(h)), zap.Error(errors.New("interrupted")))
		return Result{Handle: h, ExitCode: -1}, &Error{Kind: "interrupted", ExitCode: -1, Cause: errors.New("interrupted")}
	}
	j.WorkingDir = workingDir(s.WorkingDir)
	return r.execute(parent, j, h)
}

func (r *Runner) prepare(ctx context.Context, j Job) (runtime.ImageMetadata, error) {
	r.pullMu.Lock()
	defer r.pullMu.Unlock()
	interval := r.RollingRecheck
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	policy := j.PullPolicy
	last := r.checked[j.Image]
	if j.Rolling && (last.IsZero() || time.Since(last) >= interval) {
		policy = "always"
	}
	if err := r.Runtime.Prepare(ctx, runtime.ImageSpec{Reference: j.Image, PullPolicy: policy}); err != nil {
		return runtime.ImageMetadata{}, err
	}
	if r.checked == nil {
		r.checked = make(map[string]time.Time)
	}
	if policy == "always" {
		r.checked[j.Image] = time.Now()
	}
	metadata, ok := r.images[j.Image]
	if !ok || policy == "always" {
		var err error
		metadata, err = r.Runtime.InspectImage(ctx, j.Image)
		if err != nil {
			return runtime.ImageMetadata{}, err
		}
		if r.images == nil {
			r.images = make(map[string]runtime.ImageMetadata)
		}
		r.images[j.Image] = metadata
	}
	return metadata, nil
}

func (r *Runner) execute(parent context.Context, j Job, existing runtime.Handle) (result Result, err error) {
	started := time.Now()
	log := logging.OrNop(r.Logger).With(zap.String("component", "runner"), zap.String("job_id", j.ID), zap.String("request_hash", j.ID), zap.String("request_id", logging.RequestID(parent)), zap.String("distribution", j.Distribution), zap.String("version", j.Version), zap.String("target", j.Target), zap.String("profile", j.Profile))
	log.Info("作业开始", zap.Bool("resumed", existing != ""))
	defer func() {
		fields := []zap.Field{zap.Duration("duration", time.Since(started)), zap.Int64("exit_code", result.ExitCode)}
		if err != nil {
			kind := "failed"
			cause := err
			var failure *Error
			if errors.As(err, &failure) {
				kind, cause = failure.Kind, failure.Cause
			}
			// Error.Error 包含构建输出尾部；应用日志只记录原因，绝不复制构建输出。
			log.Error("作业失败", append(fields, zap.String("reason", kind), zap.Error(cause))...)
		} else {
			log.Info("作业完成", fields...)
		}
	}()
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
				killErr := r.Runtime.Kill(cleanup, result.Handle)
				if killErr != nil {
					log.Error("清理在途环境失败", zap.String("operation", "kill"), zap.String("container", string(result.Handle)), zap.Error(killErr))
				} else {
					log.Info("清理在途环境", zap.String("operation", "kill"), zap.String("container", string(result.Handle)))
				}
				err = errors.Join(err, killErr)
			}
			if err == nil || !j.KeepContainer || cause != nil {
				removeErr := r.Runtime.Remove(cleanup, result.Handle)
				if removeErr != nil {
					log.Error("清理执行环境失败", zap.String("operation", "remove"), zap.String("container", string(result.Handle)), zap.Error(removeErr))
				} else {
					log.Info("清理执行环境", zap.String("operation", "remove"), zap.String("container", string(result.Handle)))
				}
				err = errors.Join(err, removeErr)
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
		metadata, prepareErr := r.prepare(ctx, j)
		if prepareErr != nil {
			return result, prepareErr
		}
		j.WorkingDir = workingDir(metadata.WorkingDir)
		j.inputUID, j.inputGID = archiveOwner(metadata.User, log)
		log.Info("构建镜像元数据", zap.String("image", j.Image), zap.String("working_dir", j.WorkingDir), zap.String("user", metadata.User))
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
		result.Handle, err = r.Runtime.Create(ctx, runtime.RunSpec{Image: j.Image, Command: []string{"sh", "-c", script}, WorkingDir: j.WorkingDir, RequestHash: j.ID, Env: j.Env, KeepContainer: j.KeepContainer})
		if err != nil {
			return result, err
		}
		log.Info("执行环境已创建", zap.String("operation", "create"), zap.String("container", string(result.Handle)), zap.String("image", j.Image))
		if j.OnHandle != nil {
			if err = j.OnHandle(ctx, result.Handle); err != nil {
				return result, err
			}
		}
		if err = r.Runtime.PutArchive(ctx, result.Handle, j.WorkingDir, bytes.NewReader(archive)); err != nil {
			return result, err
		}
		log.Debug("输入已注入", zap.String("operation", "put_archive"), zap.Int("bytes", len(archive)))
		if err = r.Runtime.Start(ctx, result.Handle); err != nil {
			return result, err
		}
		log.Info("执行环境已启动", zap.String("operation", "start"), zap.String("container", string(result.Handle)))
	}
	logs, err := r.Runtime.Logs(ctx, result.Handle, runtime.LogsOptions{Follow: true, Timestamps: existing != ""})
	if err != nil {
		return result, err
	}
	defer func() { _ = logs.Close() }()
	scanner := bufio.NewScanner(logs)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	lines := 0
	lastStage := ""
	stageStarted := time.Now()
	for scanner.Scan() {
		lines++
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
			if name != lastStage {
				log.Info("构建阶段变化", zap.String("stage", name), zap.String("previous_stage", lastStage), zap.Duration("stage_duration", time.Since(stageStarted)))
				lastStage, stageStarted = name, time.Now()
			}
			stage := Stage{Name: name, At: at}
			result.Stages = append(result.Stages, stage)
			if j.OnStage != nil {
				if err = j.OnStage(ctx, stage); err != nil {
					return result, err
				}
			}
		}
	}
	if existing != "" {
		log.Info("接管日志重放完成", zap.Int("replayed_lines", lines))
	}
	if err = scanner.Err(); err != nil {
		return result, err
	}
	exit, err := r.Runtime.Wait(ctx, result.Handle)
	if err != nil {
		return result, err
	}
	result.ExitCode = exit.Code
	log.Info("执行环境已结束", zap.String("operation", "wait"), zap.String("container", string(result.Handle)), zap.Int64("exit_code", exit.Code))
	if exit.Code != 0 {
		return result, fmt.Errorf("Error while building firmware")
	}
	if err = extract(ctx, r.Runtime, result.Handle, path.Join(workingDir(j.WorkingDir), j.ID), j.ArtifactDir, log); err != nil {
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
