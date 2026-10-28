package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/chisaato/asu-go/internal/buildreq"
	logging "github.com/chisaato/asu-go/internal/logcontext"
	"github.com/chisaato/asu-go/internal/metadata"
	"github.com/chisaato/asu-go/internal/runner"
	"github.com/chisaato/asu-go/internal/runtime"
	"github.com/chisaato/asu-go/internal/sources"
	"github.com/chisaato/asu-go/internal/store"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	Logger                                                                      *zap.Logger
	DataDir                                                                     string
	MaxConcurrentBuilds, MaxPendingJobs                                         int
	BuildTTL, BuildUnversionedTTL, BuildDefaultsTTL, BuildFailureTTL, Retention time.Duration
	Limits                                                                      buildreq.Limits
}

// Response 不依赖 HTTP 框架；错误与正常轮询共用协议响应。
type Response struct {
	Status   int
	Body     map[string]any
	Stage    string
	Position int
}

type SQLite struct {
	*runner.SQLiteJobStore
	Sources   *sources.Store
	Metadata  metadata.ProfileProvider
	Runner    *runner.Runner
	Publisher ArtifactPublisher
	Options   Options
	mu        sync.Mutex
	wake      chan struct{}
	active    map[string]context.CancelFunc
}

func NewSQLite(db *sql.DB, catalog *sources.Store, meta metadata.ProfileProvider, r *runner.Runner, opts Options) *SQLite {
	return &SQLite{SQLiteJobStore: &runner.SQLiteJobStore{DB: db}, Sources: catalog, Metadata: meta, Runner: r, Options: opts, Publisher: &store.Local{Dir: filepath.Join(opts.DataDir, "store")}, wake: make(chan struct{}, 1)}
}

func failure(status int, detail string) Response {
	body := map[string]any{"status": status, "detail": detail}
	if status == 404 {
		body["title"] = "Not Found"
	}
	if status == 529 {
		body["title"] = "Server overloaded"
	}
	return Response{Status: status, Body: body}
}

func ValidationResponse(err error) Response {
	var v *buildreq.ValidationError
	if errors.As(err, &v) {
		if v.Status == 422 {
			kind := "value_error"
			if v.Detail == "Field required" {
				kind = "missing"
			} else if strings.HasPrefix(v.Detail, "String should match pattern") {
				kind = "string_pattern_mismatch"
			}
			return Response{Status: 422, Body: map[string]any{"detail": []any{map[string]any{"type": kind, "loc": []string{"body", v.Field}, "msg": v.Detail}}}}
		}
		return failure(v.Status, v.Detail)
	}
	return Response{Status: 422, Body: map[string]any{"detail": []any{map[string]any{"type": "json_invalid", "loc": []any{"body", 0}, "msg": "JSON decode error"}}}}
}

func (q *SQLite) ttl(r buildreq.Request) time.Duration {
	if r.Defaults != nil && *r.Defaults != "" {
		return q.Options.BuildDefaultsTTL
	}
	if len(r.PackagesVersions) > 0 {
		return q.Options.BuildTTL
	}
	return q.Options.BuildUnversionedTTL
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func count(ctx context.Context, db executor, metric, dim string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO stat_counters(day,metric,dim,value) VALUES(?,?,?,1) ON CONFLICT(day,metric,dim) DO UPDATE SET value=value+1`, time.Now().UTC().Format("2006-01-02"), metric, dim)
	return err
}

// Submit 先查缓存再做策略/元数据验证；事务内重新检查，保证并发提交只入队一次。
func (q *SQLite) Submit(ctx context.Context, r buildreq.Request, userAgent string) (Response, error) {
	r = r.Normalize()
	id, err := r.Hash()
	if err != nil {
		return Response{}, err
	}
	if err = count(ctx, q.DB, "requests", r.Version); err != nil {
		return Response{}, err
	}
	resp, err := q.Get(ctx, id)
	if err != nil {
		return Response{}, err
	}
	if resp.Status != 404 && resp.Stage != "failed" {
		logging.OrNop(q.Options.Logger).Debug("复用缓存作业", zap.String("component", "queue"), zap.String("request_hash", id), zap.String("request_id", logging.RequestID(ctx)))
		if err = count(ctx, q.DB, "cache_hits", r.Version); err != nil {
			return Response{}, err
		}
		return resp, nil
	}
	if err = buildreq.Validate(r, q.Options.Limits); err != nil {
		return ValidationResponse(err), nil
	}
	source, profile, err := q.validate(ctx, r)
	if err != nil {
		var v *buildreq.ValidationError
		if errors.As(err, &v) {
			return ValidationResponse(err), nil
		}
		return Response{}, err
	}
	if r.Client == nil || *r.Client == "" {
		client := "unknown/0"
		if strings.HasPrefix(userAgent, "auc") {
			client = strings.NewReplacer(" (", "/", ")", "").Replace(userAgent)
		}
		r.Client = &client
	}
	data, err := r.Canonical()
	if err != nil {
		return Response{}, err
	}
	resolved, _ := profile["id"].(string)
	if resolved == "" {
		resolved = r.Profile
	}
	packages := executionPackages(r, resolved)
	extra := []string{}
	defaults := profilePackages(profile)
	for _, p := range packages {
		if !strings.HasPrefix(p, "-") && !defaults[p] {
			extra = append(extra, p)
		}
	}
	options, err := json.Marshal(map[string]any{"build_cmd_packages": packages, "stat_packages": extra})
	if err != nil {
		return Response{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	tx, err := q.DB.BeginTx(ctx, nil)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	now := time.Now().UTC()
	// 运行中的任务不因排队时间超过 TTL 而重复执行。
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE request_hash=? AND status!='failed' AND (status IN ('queued','running') OR expires_at IS NULL OR expires_at>?)`, id, now).Scan(&exists)
	if err != nil {
		return Response{}, err
	}
	if exists != 0 {
		if err = count(ctx, tx, "cache_hits", r.Version); err != nil {
			return Response{}, err
		}
		if err = tx.Commit(); err != nil {
			return Response{}, err
		}
		return q.Get(ctx, id)
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE status='queued'`).Scan(&pending); err != nil {
		return Response{}, err
	}
	if pending >= q.Options.MaxPendingJobs {
		logging.OrNop(q.Options.Logger).Warn("构建队列已满", zap.String("component", "queue"), zap.Int("pending", pending), zap.String("request_hash", id))
		return failure(529, fmt.Sprintf("server overload, queue contains too many build requests: %d", pending)), nil
	}
	// 失败或过期记录由同主键的新任务替换。日志仍由同 hash 管理，
	// 下次执行时 Hub 截断重建，未执行则由 Cleaner 按新任务 purge_at 清理；
	// 已退出容器仍由 Scavenger 按标签与 TTL 回收，不依赖旧 handle。
	if _, err = tx.ExecContext(ctx, `DELETE FROM jobs WHERE request_hash=?`, id); err != nil {
		return Response{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs(request_hash,source_id,version,target,profile,status,stage,enqueued_at,expires_at,purge_at,client,request,options) VALUES(?,?,?,?,?,'queued','init',?,?,?,?,?,?)`, id, source.ID, r.Version, r.Target, resolved, now, now.Add(q.ttl(r)), now.Add(q.Options.Retention), r.Client, string(data), string(options))
	if err != nil {
		return Response{}, err
	}
	if err = tx.Commit(); err != nil {
		return Response{}, err
	}
	// 先返回确定的 queued 快照，再唤醒 worker，避免快速构建改变提交响应。
	logging.OrNop(q.Options.Logger).Info("作业已入队", zap.String("component", "queue"), zap.String("request_hash", id), zap.String("job_id", id), zap.String("request_id", logging.RequestID(ctx)), zap.String("distribution", r.Distro), zap.String("version", r.Version), zap.String("target", r.Target), zap.String("profile", resolved))
	resp, err = q.Get(ctx, id)
	q.notify()
	return resp, err
}

func (q *SQLite) validate(ctx context.Context, r buildreq.Request) (sources.Source, map[string]any, error) {
	deny := func(s string) (sources.Source, map[string]any, error) {
		return sources.Source{}, nil, &buildreq.ValidationError{Status: 400, Detail: s}
	}
	all, err := q.Sources.List(ctx)
	if err != nil {
		return sources.Source{}, nil, err
	}
	var source sources.Source
	distro, branch := false, false
	for _, s := range all {
		if s.Enabled && s.Distribution == r.Distro {
			distro = true
			if s.Branch == metadata.Branch(r.Version) {
				branch = true
			}
			if s.Version == r.Version {
				source = s
			}
		}
	}
	if !distro {
		return deny("Unsupported distro: " + r.Distro)
	}
	if !branch {
		return deny("Unsupported branch: " + r.Version)
	}
	if source.ID == 0 {
		return deny("Unsupported version: " + r.Version)
	}
	targets, err := q.Metadata.Targets(ctx, r.Distro, r.Version)
	if err != nil {
		return sources.Source{}, nil, err
	}
	if _, ok := targets[r.Target]; !ok {
		return deny("Unsupported target: " + r.Target + ". The requested target was either dropped, is still being built or is not supported by the selected version. Please check the forums or try again later.")
	}
	var profile map[string]any
	if resolver, ok := q.Metadata.(interface {
		ResolveBuildProfile(context.Context, string, string, string, string) (map[string]any, error)
	}); ok {
		profile, err = resolver.ResolveBuildProfile(ctx, r.Distro, r.Version, r.Target, r.Profile)
	} else {
		profile, err = q.Metadata.Profile(ctx, r.Distro, r.Version, r.Target, r.Profile)
	}
	if err != nil {
		var missing *metadata.ProfileNotFoundError
		if errors.As(err, &missing) {
			return deny("Unsupported profile: " + r.Profile + ". The requested profile was either dropped or never existed. Please check the forums for more information.")
		}
		return sources.Source{}, nil, err
	}
	return source, profile, nil
}

func selectedPackages(r buildreq.Request, profile map[string]any) []string {
	packages := slices.Clone(r.Packages)
	if len(r.PackagesVersions) > 0 {
		packages = []string{}
		for p := range r.PackagesVersions {
			packages = append(packages, strings.TrimPrefix(p, "+"))
		}
		slices.Sort(packages)
	}
	if !r.DiffPackages {
		return packages
	}
	defaults := profilePackages(profile)
	remove := []string{}
	for p := range defaults {
		if !slices.Contains(packages, p) {
			remove = append(remove, "-"+strings.TrimPrefix(p, "-"))
		}
	}
	slices.Sort(remove)
	return append(remove, packages...)
}

func profilePackages(profile map[string]any) map[string]bool {
	defaults := map[string]bool{}
	for _, key := range []string{"default_packages", "device_packages"} {
		switch values := profile[key].(type) {
		case []any:
			for _, p := range values {
				if name, ok := p.(string); ok {
					defaults[name] = true
				}
			}
		case []string:
			for _, p := range values {
				defaults[p] = true
			}
		}
	}
	return defaults
}

func (q *SQLite) Get(ctx context.Context, id string) (Response, error) {
	if strings.HasPrefix(id, "rp_") {
		return failure(404, "could not find provided request hash"), nil
	}
	var status, stage, detail, message, result string
	var enqueued time.Time
	err := q.DB.QueryRowContext(ctx, `SELECT status,COALESCE(stage,'init'),COALESCE(detail,''),COALESCE(error,''),COALESCE(result,'{}'),enqueued_at FROM jobs WHERE request_hash=? AND (status IN ('queued','running') OR expires_at IS NULL OR expires_at>?)`, id, time.Now().UTC()).Scan(&status, &stage, &detail, &message, &result, &enqueued)
	if errors.Is(err, sql.ErrNoRows) {
		return failure(404, "could not find provided request hash"), nil
	}
	if err != nil {
		return Response{}, err
	}
	resp := Response{Status: 202, Stage: stage, Body: map[string]any{"enqueued_at": enqueued, "request_hash": id}}
	switch status {
	case "queued":
		resp.Stage = "queued"
		if err = q.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE status='queued' AND (enqueued_at<? OR (enqueued_at=? AND request_hash<?))`, enqueued, enqueued, id).Scan(&resp.Position); err != nil {
			return Response{}, err
		}
		resp.Body["detail"] = "queued"
		resp.Body["queue_position"] = resp.Position
	case "running":
		resp.Body["detail"] = "started"
		resp.Body["imagebuilder_status"] = stage
	case "done":
		resp.Status = 200
		resp.Stage = "done"
		var data map[string]any
		if err = json.Unmarshal([]byte(result), &data); err != nil {
			return Response{}, err
		}
		for k, v := range data {
			resp.Body[k] = v
		}
		resp.Body["detail"] = "done"
	case "failed":
		resp.Status = 500
		resp.Stage = "failed"
		if detail == "" || detail == "init" {
			detail = "failed"
		}
		resp.Body["detail"] = detail
		resp.Body["stderr"] = message
	default:
		return Response{}, fmt.Errorf("未知任务状态 %q", status)
	}
	resp.Body["status"] = resp.Status
	return resp, nil
}

func (q *SQLite) Length(ctx context.Context) (int, error) {
	var n int
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE status='queued'`).Scan(&n)
	return n, err
}
func (q *SQLite) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Run 由 serve 的 errgroup 持有。先接管旧 running，再按并发上限消费持久 queued。
func (q *SQLite) Run(ctx context.Context) error {
	if err := q.Runner.Takeover(ctx, q, filepath.Join(q.Options.DataDir, "recovery")); err != nil {
		return err
	}
	g, ctx := errgroup.WithContext(ctx)
	for i := 0; i < q.Options.MaxConcurrentBuilds; i++ {
		g.Go(func() error {
			for {
				if ctx.Err() != nil {
					return nil
				}
				id, err := q.claim(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				if id == "" {
					select {
					case <-ctx.Done():
						return nil
					case <-q.wake:
					}
					continue
				}
				q.notify()
				if err = q.execute(ctx, id); err != nil {
					return err
				}
			}
		})
	}
	return g.Wait()
}

func (q *SQLite) claim(ctx context.Context) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var id string
	err := q.DB.QueryRowContext(ctx, `UPDATE jobs SET status='running',stage='init',started_at=? WHERE request_hash=(SELECT request_hash FROM jobs WHERE status='queued' ORDER BY enqueued_at,request_hash LIMIT 1) AND status='queued' RETURNING request_hash`, time.Now().UTC()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (q *SQLite) execute(ctx context.Context, id string) error {
	ctx, done := q.ExecutionContext(ctx, id)
	defer done()
	job, err := q.job(ctx, id)
	if err == nil {
		var enqueued time.Time
		if queryErr := q.DB.QueryRowContext(ctx, `SELECT enqueued_at FROM jobs WHERE request_hash=?`, id).Scan(&enqueued); queryErr == nil {
			logging.OrNop(q.Options.Logger).Info("作业已出队", zap.String("component", "queue"), zap.String("request_hash", id), zap.String("job_id", id), zap.Duration("queue_duration", time.Since(enqueued)), zap.String("distribution", job.Distribution), zap.String("version", job.Version), zap.String("target", job.Target), zap.String("profile", job.Profile))
		}
	}
	if job.ArtifactDir != "" {
		defer func() { _ = os.RemoveAll(job.ArtifactDir) }()
	}
	var result runner.Result
	if err == nil {
		result, err = q.Runner.Run(ctx, job)
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return q.Complete(persist, id, result, err)
}

func (q *SQLite) job(ctx context.Context, id string) (runner.Job, error) {
	var raw, profile, options string
	var sourceID int64
	err := q.DB.QueryRowContext(ctx, `SELECT request,profile,source_id,COALESCE(options,'{}') FROM jobs WHERE request_hash=?`, id).Scan(&raw, &profile, &sourceID, &options)
	if err != nil {
		return runner.Job{}, err
	}
	var r buildreq.Request
	if err = json.Unmarshal([]byte(raw), &r); err != nil {
		return runner.Job{}, err
	}
	source, err := q.Sources.Get(ctx, sourceID)
	if err != nil {
		return runner.Job{}, err
	}
	image, err := sources.ResolveImage(source, r.Target)
	if err != nil {
		return runner.Job{}, err
	}
	var opts struct {
		Packages []string `json:"build_cmd_packages"`
		ReplayOptions
	}
	if err = json.Unmarshal([]byte(options), &opts); err != nil {
		return runner.Job{}, err
	}
	j := runner.Job{Distribution: r.Distro, Version: r.Version, Target: r.Target, ID: id, Image: image, Profile: profile, VersionCode: r.VersionCode, Packages: opts.Packages, PackageVersions: r.PackagesVersions, Repositories: r.Repositories, RepositoriesMode: r.RepositoriesMode, RepositoryKeys: r.RepositoryKeys, Rolling: source.Rolling, ArtifactDir: filepath.Join(q.Options.DataDir, "work", id)}
	applyReplayOptions(&j, opts.ReplayOptions)
	j.DiffPackages = r.DiffPackages
	j.Packages = executionPackages(r, profile)
	if r.Defaults != nil {
		j.Defaults = *r.Defaults
	}
	if r.Filesystem != nil {
		j.Filesystem = *r.Filesystem
	}
	if r.RootfsSizeMB != nil {
		j.RootFSSizeMB = *r.RootfsSizeMB
	}
	j.Setup = strings.HasSuffix(strings.ToLower(r.Version), "snapshot")
	if j.Setup {
		j.Env = []string{"TARGET=" + r.Target, "VERSION_PATH=" + metadata.VersionPath(r.Version)}
	}
	j.OnHandle = func(ctx context.Context, h runtime.Handle) error { return q.SaveHandle(ctx, id, h) }
	j.OnStage = func(ctx context.Context, s runner.Stage) error { return q.RecordStage(ctx, id, s) }
	return j, nil
}

func (q *SQLite) RecordStage(ctx context.Context, id string, s runner.Stage) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = q.DB.ExecContext(ctx, `UPDATE jobs SET stage=?,stages=json_insert(stages,'$[#]',json(?)) WHERE request_hash=? AND status='running'`, s.Name, string(data), id)
	return err
}
