package runtime

import (
	"context"
	"io"
)

// Handle 是可持久化的不透明值，只能保存、恢复并传回驱动。
type Handle string
type ImageSpec struct{ Reference, PullPolicy string }
type RunSpec struct {
	Image                         string
	Command, Env                  []string
	WorkingDir, User, RequestHash string
	KeepContainer                 bool
}
type LogsOptions struct {
	Follow     bool
	Since      string
	Timestamps bool
}
type ExitStatus struct{ Code int64 }
type Info struct{ Name, Version, LoggingDriver string }

// Options 由配置组装层映射，避免 runtime 反向依赖 config。
type Options struct {
	Socket      string
	MemoryLimit int64
	CPUs        float64
	PullPolicy  string
}
type Runtime interface {
	Prepare(context.Context, ImageSpec) error
	// Create 分配环境，允许在 Start 前注入输入。
	Create(context.Context, RunSpec) (Handle, error)
	PutArchive(context.Context, Handle, string, io.Reader) error
	Start(context.Context, Handle) error
	Logs(context.Context, Handle, LogsOptions) (io.ReadCloser, error)
	Wait(context.Context, Handle) (ExitStatus, error)
	GetArchive(context.Context, Handle, string) (io.ReadCloser, error)
	// Kill 保留日志和产物；Remove 最终清理，两者均幂等。
	Kill(context.Context, Handle) error
	Remove(context.Context, Handle) error
	Info(context.Context) (Info, error)
	Close() error
}

func Open(name string, opts Options) (Runtime, error) {
	d, err := Lookup(name)
	if err != nil {
		return nil, err
	}
	return d.Open(opts)
}
