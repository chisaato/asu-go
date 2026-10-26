package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// FakePlan 在 Create 时复制；Archives 保存路径对应的原始 tar 数据。
type FakePlan struct {
	Output   string
	ExitCode int64
	Delay    time.Duration
	Archives map[string][]byte
}

// Fake 不产生后台协程；等待由调用方的 context 或 Kill 中断。
type Fake struct {
	mu   sync.Mutex
	Next FakePlan
	jobs map[Handle]*fakeJob
	seq  int
}
type fakeJob struct {
	spec              RunSpec
	plan              FakePlan
	start             time.Time
	killed            chan struct{}
	killOnce          sync.Once
	inputs            map[string][]byte
	created, finished time.Time
}

func NewFake() *Fake                                           { return &Fake{jobs: make(map[Handle]*fakeJob)} }
func (f *Fake) Prepare(ctx context.Context, _ ImageSpec) error { return ctx.Err() }
func (f *Fake) Info(ctx context.Context) (Info, error) {
	return Info{Name: "fake", Version: "1", LoggingDriver: "memory"}, ctx.Err()
}
func (f *Fake) Close() error { return nil }
func (f *Fake) Create(ctx context.Context, spec RunSpec) (Handle, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	h := Handle(fmt.Sprintf("fake-%d", f.seq))
	p := f.Next
	p.Archives = cloneArchives(p.Archives)
	f.jobs[h] = &fakeJob{spec: spec, plan: p, killed: make(chan struct{}), inputs: make(map[string][]byte), created: time.Now()}
	return h, nil
}
func cloneArchives(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = bytes.Clone(v)
	}
	return out
}
func (f *Fake) job(h Handle) (*fakeJob, error) {
	j := f.jobs[h]
	if j == nil {
		return nil, fmt.Errorf("句柄不存在: %s", h)
	}
	return j, nil
}
func (f *Fake) Start(ctx context.Context, h Handle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, err := f.job(h)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !j.start.IsZero() {
		return fmt.Errorf("已经启动")
	}
	j.start = time.Now()
	return nil
}
func (f *Fake) PutArchive(ctx context.Context, h Handle, path string, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	j, err := f.job(h)
	if err != nil {
		return err
	}
	if !j.start.IsZero() {
		return fmt.Errorf("输入必须在 Start 前注入")
	}
	j.inputs[path] = data
	return nil
}
func (f *Fake) GetArchive(ctx context.Context, h Handle, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	j, err := f.job(h)
	if err != nil {
		return nil, err
	}
	data, ok := j.plan.Archives[path]
	if !ok {
		data, ok = j.inputs[path]
	}
	if !ok {
		return nil, fmt.Errorf("路径不存在: %s", path)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (f *Fake) Wait(ctx context.Context, h Handle) (ExitStatus, error) {
	f.mu.Lock()
	j, err := f.job(h)
	if err != nil {
		f.mu.Unlock()
		return ExitStatus{}, err
	}
	start := j.start
	f.mu.Unlock()
	if start.IsZero() {
		return ExitStatus{}, fmt.Errorf("尚未启动")
	}
	timer := time.NewTimer(time.Until(start.Add(j.plan.Delay)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ExitStatus{}, ctx.Err()
	case <-j.killed:
		return ExitStatus{Code: 137}, nil
	case <-timer.C:
		return ExitStatus{Code: j.plan.ExitCode}, nil
	}
}
func (f *Fake) Kill(ctx context.Context, h Handle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[h]
	if j != nil {
		j.killOnce.Do(func() { j.finished = time.Now(); close(j.killed) })
	}
	return nil
}
func (f *Fake) Remove(ctx context.Context, h Handle) error {
	if err := f.Kill(ctx, h); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.jobs, h)
	return nil
}

type fakeLogs struct {
	io.Reader
	ctx    context.Context
	cancel context.CancelFunc
	f      *Fake
	h      Handle
	follow bool
	closed chan struct{}
	once   sync.Once
}

func (r *fakeLogs) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.follow {
		ctx, cancel := context.WithCancel(r.ctx)
		defer cancel()
		select {
		case <-r.closed:
			return 0, io.ErrClosedPipe
		default:
		}
		_, err = r.f.Wait(ctx, r.h)
		if err == nil {
			err = io.EOF
		}
	}
	return n, err
}
func (r *fakeLogs) Close() error { r.once.Do(func() { close(r.closed); r.cancel() }); return nil }
func (f *Fake) Logs(ctx context.Context, h Handle, opts LogsOptions) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, err := f.job(h)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	output := j.plan.Output
	if opts.Timestamps {
		var b strings.Builder
		for _, line := range strings.SplitAfter(output, "\n") {
			if line != "" {
				b.WriteString(j.start.UTC().Format(time.RFC3339Nano) + " " + line)
			}
		}
		output = b.String()
	}
	return &fakeLogs{Reader: bytes.NewBufferString(output), ctx: ctx, cancel: cancel, f: f, h: h, follow: opts.Follow, closed: make(chan struct{})}, nil
}

var _ Runtime = (*Fake)(nil)
var _ Runtime = (*engine)(nil)

func (f *Fake) state(h Handle, j *fakeJob) State {
	s := State{Handle: h, Exists: true, CreatedAt: j.created, FinishedAt: j.finished,
		Labels: map[string]string{"managed-by": "asu-go", "asu.request_hash": j.spec.RequestHash}}
	if j.spec.KeepContainer {
		s.Labels["asu.keep_container"] = "true"
	}
	if !j.start.IsZero() && s.FinishedAt.IsZero() {
		s.Running = time.Now().Before(j.start.Add(j.plan.Delay))
		if !s.Running {
			s.FinishedAt = j.start.Add(j.plan.Delay)
		}
	}
	return s
}
func (f *Fake) Inspect(ctx context.Context, h Handle) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if j := f.jobs[h]; j != nil {
		return f.state(h, j), nil
	}
	return State{Handle: h}, nil
}
func (f *Fake) List(ctx context.Context) ([]State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []State
	for h, j := range f.jobs {
		out = append(out, f.state(h, j))
	}
	return out, nil
}
