package runner

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/runtime"
)

// memoryJobs 是队列未接入时的最小测试实现；锁保护并行接管的收尾。
type memoryJobs struct {
	mu       sync.Mutex
	tasks    []Task
	results  map[string]Result
	failures map[string]error
	err      error
}

func (s *memoryJobs) Active(context.Context) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tasks []Task
	for _, task := range s.tasks {
		if task.Status == "running" || task.Status == "queued" {
			tasks = append(tasks, task)
		}
	}
	return tasks, s.err
}
func (s *memoryJobs) Complete(_ context.Context, id string, result Result, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = make(map[string]Result)
		s.failures = make(map[string]error)
	}
	s.results[id], s.failures[id] = result, err
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = "done"
			if err != nil {
				s.tasks[i].Status = "failed"
			}
		}
	}
	return nil
}

func TestTakeoverBranches(t *testing.T) {
	for _, branch := range []string{"running", "exited", "missing", "failed"} {
		t.Run(branch, func(t *testing.T) {
			ctx := context.Background()
			output := "##asu:stage=building_image\nfirst\nfirst\nlast\n"
			plan := runtime.FakePlan{Output: output, Archives: map[string][]byte{"/builder/job": tarData(t, "job/firmware.bin", "ok", tar.TypeReg)}}
			if branch == "running" {
				plan.Delay = 30 * time.Millisecond
			}
			if branch == "failed" {
				plan.ExitCode = 3
			}
			r, observed, j := setupRunner(t, plan)
			h, err := observed.Runtime.Create(ctx, runtime.RunSpec{RequestHash: j.ID})
			if err != nil {
				t.Fatal(err)
			}
			if err := observed.Runtime.Start(ctx, h); err != nil {
				t.Fatal(err)
			}
			if branch == "missing" {
				if err := observed.Runtime.Remove(ctx, h); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Hub.Begin(j.ID); err != nil {
				t.Fatal(err)
			}
			if err := r.Hub.Append(j.ID, "stale partial log\n"); err != nil {
				t.Fatal(err)
			}
			if err := r.Hub.Finish(j.ID); err != nil {
				t.Fatal(err)
			}
			store := &memoryJobs{tasks: []Task{{ID: j.ID, Handle: h, Status: "running"}}}
			if err := r.Takeover(ctx, store, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			failure := store.failures[j.ID]
			if branch == "missing" || branch == "failed" {
				var e *Error
				if !errors.As(failure, &e) {
					t.Fatal(failure)
				}
				if branch == "missing" && e.Kind != "interrupted" {
					t.Fatal(e)
				}
			} else if failure != nil {
				t.Fatal(failure)
			}
			if branch != "missing" {
				if got := strings.Join(r.Hub.Tail(j.ID), ""); got != output {
					t.Fatalf("重放重复/丢失: %q", got)
				}
				file, err := r.Hub.History(j.ID)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(file)
				_ = file.Close()
				if err != nil || string(data) != output {
					t.Fatal(string(data), err)
				}
				if len(store.results[j.ID].Stages) != 1 {
					t.Fatal(store.results)
				}
				if strings.Contains(strings.Join(observed.calls, ","), "create") {
					t.Fatal("接管重新创建了资源")
				}
			}
			state, err := observed.Inspect(ctx, h)
			if err != nil || state.Exists {
				t.Fatal(state, err)
			}
		})
	}
}

type listedRuntime struct {
	runtime.Runtime
	states  []runtime.State
	removed []runtime.Handle
}

func (r *listedRuntime) List(context.Context) ([]runtime.State, error) { return r.states, nil }
func (r *listedRuntime) Remove(_ context.Context, h runtime.Handle) error {
	r.removed = append(r.removed, h)
	return nil
}

func TestScavengerProtectionAndRetention(t *testing.T) {
	now := time.Now()
	state := func(id string, age time.Duration, keep bool) runtime.State {
		labels := map[string]string{"managed-by": "asu-go", "asu.request_hash": id}
		if keep {
			labels["asu.keep_container"] = "true"
		}
		return runtime.State{Handle: runtime.Handle(id), Exists: true, Labels: labels, CreatedAt: now.Add(-age), FinishedAt: now.Add(-age)}
	}
	foreign := state("foreign", 2*time.Hour, false)
	foreign.Labels["managed-by"] = "someone-else"
	active := state("active", 2*time.Hour, true)
	active.Running = true
	queued := state("queued", 2*time.Hour, false)
	rt := &listedRuntime{states: []runtime.State{foreign, active, queued, state("expired", 2*time.Hour, true), state("old", 40*time.Minute, true), state("new", time.Minute, true), state("orphan", time.Minute, false)}}
	jobs := &memoryJobs{tasks: []Task{{ID: "active", Handle: "different", Status: "running"}, {ID: "queued", Status: "queued"}}}
	s := &Scavenger{Runtime: rt, Jobs: jobs, MaxKept: 1, KeepContainerTTL: time.Hour}
	if err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rt.removed, []runtime.Handle{"expired", "orphan", "old"}) {
		t.Fatal(rt.removed)
	}
	// 空表也不能误删其他服务的容器；显式 max_kept=0 不保留诊断容器。
	rt.states, rt.removed = []runtime.State{foreign, state("new", time.Minute, true)}, nil
	s.MaxKept = 0
	jobs.tasks = nil
	if err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rt.removed, []runtime.Handle{"new"}) {
		t.Fatal(rt.removed)
	}
	jobs.err = errors.New("db unavailable")
	rt.removed = nil
	if err := s.Sweep(context.Background()); err == nil || len(rt.removed) != 0 {
		t.Fatal(err, rt.removed)
	}
}

type prepareRuntime struct {
	runtime.Runtime
	policies []string
	err      error
}

func (r *prepareRuntime) Prepare(_ context.Context, s runtime.ImageSpec) error {
	r.policies = append(r.policies, s.PullPolicy)
	return r.err
}
func TestRollingRecheck(t *testing.T) {
	rt := &prepareRuntime{Runtime: runtime.NewFake()}
	r := &Runner{Runtime: rt, RollingRecheck: time.Hour}
	j := Job{Image: "rolling", Rolling: true, PullPolicy: "missing"}
	ctx := context.Background()
	if _, err := r.prepare(ctx, j); err != nil {
		t.Fatal(err)
	}
	if _, err := r.prepare(ctx, j); err != nil {
		t.Fatal(err)
	}
	r.checked[j.Image] = time.Now().Add(-2 * time.Hour)
	if _, err := r.prepare(ctx, j); err != nil {
		t.Fatal(err)
	}
	j.Rolling = false
	j.PullPolicy = "never"
	if _, err := r.prepare(ctx, j); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rt.policies, []string{"always", "missing", "always", "never"}) {
		t.Fatal(rt.policies)
	}
	// 失败复查不更新时钟，重启后的新实例也重新 always。
	r2 := &Runner{Runtime: rt}
	j.Rolling = true
	rt.err = errors.New("pull failed")
	if _, err := r2.prepare(ctx, j); err == nil {
		t.Fatal("拉取错误被吞掉")
	}
	if !r2.checked[j.Image].IsZero() {
		t.Fatal(r2.checked)
	}
	rt.err = nil
	if _, err := r2.prepare(ctx, j); err != nil || rt.policies[len(rt.policies)-1] != "always" {
		t.Fatal(err, rt.policies)
	}
}

func TestSQLiteJobStore(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := &SQLiteJobStore{DB: database.DB}
	tasks, err := s.Active(ctx)
	if err != nil || len(tasks) != 0 {
		t.Fatal(tasks, err)
	}
	for _, id := range []string{"string", "envelope", "missing", "queued"} {
		status := "running"
		if id == "queued" {
			status = "queued"
		}
		_, err := database.ExecContext(ctx, `INSERT INTO jobs(request_hash,version,target,profile,status,enqueued_at,request,options) VALUES(?,'v','t','p',?,?,'{}','{"keep_container":true}')`, id, status, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveHandle(ctx, "string", "fake-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE jobs SET handle='{"driver":"fake","id":"fake-2"}' WHERE request_hash='envelope'`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveHandle(ctx, "queued", "fake-3"); err == nil {
		t.Fatal("queued 不应写入句柄")
	}
	tasks, err = s.Active(ctx)
	if err != nil || len(tasks) != 4 {
		t.Fatal(tasks, err)
	}
	for _, task := range tasks {
		if !task.KeepContainer {
			t.Fatal(task)
		}
		if task.ID == "string" && task.Handle != "fake-1" {
			t.Fatal(task)
		}
		if task.ID == "envelope" && task.Handle != "fake-2" {
			t.Fatal(task)
		}
	}
	r, _, _ := setupRunner(t, runtime.FakePlan{})
	if err := r.Takeover(ctx, s, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var status, detail string
	if err := database.QueryRowContext(ctx, `SELECT status,detail FROM jobs WHERE request_hash='missing'`).Scan(&status, &detail); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || detail != "interrupted" {
		t.Fatal(status, detail)
	}
	if err := s.Complete(ctx, "string", Result{ExitCode: 0}, nil); err != nil {
		t.Fatal(err)
	}
	exists, finished, err := s.State(ctx, "missing")
	if err != nil || !exists || !finished {
		t.Fatal(exists, finished, err)
	}
	exists, _, err = s.State(ctx, "not-found")
	if err != nil || exists {
		t.Fatal(exists, err)
	}
}
