package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const buildNetwork = "asu-build"

type engineDriver string

func (d engineDriver) Name() string { return string(d) }
func (d engineDriver) Open(opts Options) (Runtime, error) {
	c, err := client.New(client.WithHost(opts.Socket))
	if err != nil {
		return nil, err
	}
	return &engine{client: c, name: string(d), opts: opts}, nil
}

type engine struct {
	client *client.Client
	name   string
	opts   Options
}

func (e *engine) Close() error { return e.client.Close() }
func checkLoggingDriver(name string) error {
	if name == "none" || name == "" {
		return fmt.Errorf("运行时日志驱动 %q 不可读，无法重放构建日志", name)
	}
	return nil
}
func (e *engine) Info(ctx context.Context) (Info, error) {
	i, err := e.client.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Info{}, fmt.Errorf("检测运行时: %w", err)
	}
	v, err := e.client.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return Info{}, err
	}
	name := e.name
	for _, component := range v.Components {
		if component.Name == "Podman Engine" {
			name = "podman"
		}
	}
	if name == "podman" && i.Info.LoggingDriver == "" {
		// Podman 兼容 /info 不报告默认日志驱动，仅此处使用原生只读接口。
		i.Info.LoggingDriver, err = e.podmanLogDriver(ctx)
		if err != nil {
			return Info{}, err
		}
	}
	if err = checkLoggingDriver(i.Info.LoggingDriver); err != nil {
		return Info{}, err
	}
	return Info{Name: name, Version: v.Version, LoggingDriver: i.Info.LoggingDriver}, nil
}

func (e *engine) podmanLogDriver(ctx context.Context) (string, error) {
	if !strings.HasPrefix(e.opts.Socket, "unix://") {
		return "", fmt.Errorf("podman 日志检测需要 unix socket")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", strings.TrimPrefix(e.opts.Socket, "unix://"))
	}}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/v4.0.0/libpod/info", nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("podman info: HTTP %d", resp.StatusCode)
	}
	var data struct {
		Host struct {
			LogDriver string `json:"logDriver"`
		} `json:"host"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.Host.LogDriver, nil
}
func (e *engine) Prepare(ctx context.Context, spec ImageSpec) error {
	if _, err := e.Info(ctx); err != nil {
		return err
	}
	if _, err := e.client.NetworkInspect(ctx, buildNetwork, client.NetworkInspectOptions{}); err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		if _, err = e.client.NetworkCreate(ctx, buildNetwork, client.NetworkCreateOptions{}); err != nil {
			// 并行创建同名网络时重新检查。
			if _, inspectErr := e.client.NetworkInspect(ctx, buildNetwork, client.NetworkInspectOptions{}); inspectErr != nil {
				return err
			}
		}
	}
	policy := spec.PullPolicy
	if policy == "" {
		policy = e.opts.PullPolicy
	}
	if policy == "" {
		policy = "missing"
	}
	if policy != "missing" && policy != "always" && policy != "never" {
		return fmt.Errorf("无效 pull_policy: %s", policy)
	}
	if policy != "always" {
		_, err := e.client.ImageInspect(ctx, spec.Reference)
		if err == nil {
			return nil
		}
		if !errdefs.IsNotFound(err) || policy == "never" {
			return err
		}
	}
	r, err := e.client.ImagePull(ctx, spec.Reference, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return r.Wait(ctx)
}
func (e *engine) Create(ctx context.Context, spec RunSpec) (Handle, error) {
	labels := map[string]string{"managed-by": "asu-go", "asu.request_hash": spec.RequestHash}
	if spec.KeepContainer {
		labels["asu.keep_container"] = "true"
	}
	r, err := e.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{buildNetwork: {}}},
		Config:           &container.Config{Image: spec.Image, Cmd: spec.Command, Env: spec.Env, WorkingDir: spec.WorkingDir, User: spec.User, Labels: labels},
		HostConfig:       &container.HostConfig{CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"}, NetworkMode: container.NetworkMode(buildNetwork), Resources: container.Resources{Memory: e.opts.MemoryLimit, NanoCPUs: int64(e.opts.CPUs * 1e9)}},
	})
	return Handle(r.ID), err
}
func (e *engine) Start(ctx context.Context, h Handle) error {
	_, err := e.client.ContainerStart(ctx, string(h), client.ContainerStartOptions{})
	return err
}
func (e *engine) PutArchive(ctx context.Context, h Handle, path string, r io.Reader) error {
	_, err := e.client.CopyToContainer(ctx, string(h), client.CopyToContainerOptions{DestinationPath: path, Content: r})
	return err
}
func (e *engine) GetArchive(ctx context.Context, h Handle, path string) (io.ReadCloser, error) {
	r, err := e.client.CopyFromContainer(ctx, string(h), client.CopyFromContainerOptions{SourcePath: path})
	return r.Content, err
}
func (e *engine) Wait(ctx context.Context, h Handle) (ExitStatus, error) {
	r := e.client.ContainerWait(ctx, string(h), client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case <-ctx.Done():
		return ExitStatus{}, ctx.Err()
	case err := <-r.Error:
		return ExitStatus{}, err
	case s := <-r.Result:
		if s.Error != nil {
			return ExitStatus{}, errors.New(s.Error.Message)
		}
		return ExitStatus{Code: s.StatusCode}, nil
	}
}
func (e *engine) Kill(ctx context.Context, h Handle) error {
	_, err := e.client.ContainerKill(ctx, string(h), client.ContainerKillOptions{Signal: "SIGKILL"})
	if errdefs.IsNotFound(err) || errdefs.IsConflict(err) {
		return nil
	}
	return err
}
func (e *engine) Remove(ctx context.Context, h Handle) error {
	_, err := e.client.ContainerRemove(ctx, string(h), client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return err
}

func (e *engine) Inspect(ctx context.Context, h Handle) (State, error) {
	r, err := e.client.ContainerInspect(ctx, string(h), client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return State{Handle: h}, nil
	}
	if err != nil {
		return State{}, err
	}
	s := State{Handle: h, Exists: true, Labels: r.Container.Config.Labels}
	s.CreatedAt, _ = time.Parse(time.RFC3339Nano, r.Container.Created)
	if r.Container.State != nil {
		s.Running = r.Container.State.Running
		s.FinishedAt, _ = time.Parse(time.RFC3339Nano, r.Container.State.FinishedAt)
	}
	return s, nil
}

func (e *engine) List(ctx context.Context) ([]State, error) {
	r, err := e.client.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", "managed-by=asu-go")})
	if err != nil {
		return nil, err
	}
	var states []State
	for _, c := range r.Items {
		if c.Labels["managed-by"] != "asu-go" {
			continue
		}
		s, err := e.Inspect(ctx, Handle(c.ID))
		if err != nil {
			return nil, err
		}
		if s.Exists {
			states = append(states, s)
		}
	}
	return states, nil
}

// 每个 reader 拥有解帧协程，Close 关闭源并等待退出。
type logReader struct {
	*io.PipeReader
	source io.ReadCloser
	done   chan struct{}
	once   sync.Once
}

func (r *logReader) Close() error {
	r.once.Do(func() { _ = r.PipeReader.Close(); _ = r.source.Close(); <-r.done })
	return nil
}
func (e *engine) Logs(ctx context.Context, h Handle, opts LogsOptions) (io.ReadCloser, error) {
	source, err := e.client.ContainerLogs(ctx, string(h), client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Follow: opts.Follow, Since: opts.Since, Timestamps: opts.Timestamps})
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	r := &logReader{PipeReader: pr, source: source, done: make(chan struct{})}
	stop := context.AfterFunc(ctx, func() { _ = pr.CloseWithError(ctx.Err()); _ = source.Close() })
	go func() {
		defer close(r.done)
		defer stop()
		defer func() { _ = source.Close() }()
		_, err := stdcopy.StdCopy(pw, pw, source)
		_ = pw.CloseWithError(err)
	}()
	return r, nil
}
