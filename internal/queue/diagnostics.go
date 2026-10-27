package queue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	logging "github.com/gzzchh/asu-go/internal/logcontext"
	"go.uber.org/zap"

	"github.com/gzzchh/asu-go/internal/buildreq"
	"github.com/gzzchh/asu-go/internal/runner"
)

type ReplayOptions struct {
	Verbose       *bool  `json:"verbose,omitempty"`
	KeepContainer *bool  `json:"keep_container,omitempty"`
	ImageTag      string `json:"image_tag,omitempty"`
	PullPolicy    string `json:"pull_policy,omitempty"`
	SkipLimits    bool   `json:"skip_limits,omitempty"`
}

func applyReplayOptions(j *runner.Job, o ReplayOptions) {
	j.Verbose = o.Verbose != nil && *o.Verbose
	j.KeepContainer = o.KeepContainer != nil && *o.KeepContainer
	j.PullPolicy = o.PullPolicy
	if o.ImageTag != "" {
		base := strings.SplitN(j.Image, "@", 2)[0]
		if i := strings.LastIndex(base, ":"); i > strings.LastIndex(base, "/") {
			base = base[:i]
		}
		j.Image = base + ":" + o.ImageTag
		j.Rolling = false
	}
}

// replayID 用标准库生成 48 位毫秒时间戳与 80 位安全随机数的 ULID。
func replayID() (string, error) {
	var data [16]byte
	n := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		data[i] = byte(n)
		n >>= 8
	}
	if _, err := rand.Read(data[6:]); err != nil {
		return "", err
	}
	x := new(big.Int).SetBytes(data[:])
	alphabet := "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var encoded [26]byte
	for i := 25; i >= 0; i-- {
		encoded[i] = alphabet[x.Uint64()&31]
		x.Rsh(x, 5)
	}
	return "rp_" + string(encoded[:]), nil
}

// Replay 从已保存请求或直接提交的请求创建独立诊断任务，不经过公开 Submit/统计。
func (q *SQLite) Replay(ctx context.Context, origin string, r buildreq.Request, o ReplayOptions) (Response, error) {
	if origin != "" {
		var raw string
		err := q.DB.QueryRowContext(ctx, `SELECT request FROM jobs WHERE request_hash=? AND (purge_at IS NULL OR purge_at>?)`, origin, time.Now().UTC()).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return failure(404, "job not found"), nil
		}
		if err != nil {
			return Response{}, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return Response{}, err
		}
	}
	r = r.Normalize()
	if err := buildreq.ValidateReplay(r, q.Options.Limits, o.SkipLimits); err != nil {
		return ValidationResponse(err), nil
	}
	if o.PullPolicy != "" && o.PullPolicy != "missing" && o.PullPolicy != "always" && o.PullPolicy != "never" {
		return failure(400, "invalid pull_policy"), nil
	}
	if o.ImageTag != "" && !validImageTag(o.ImageTag) {
		return failure(400, "invalid image_tag"), nil
	}
	source, profile, err := q.validate(ctx, r)
	if err != nil {
		var validation *buildreq.ValidationError
		if errors.As(err, &validation) {
			return ValidationResponse(err), nil
		}
		return Response{}, err
	}
	if origin == "" {
		origin, err = r.Hash()
		if err != nil {
			return Response{}, err
		}
	}
	if o.Verbose == nil {
		v := true
		o.Verbose = &v
	}
	if o.KeepContainer == nil {
		v := true
		o.KeepContainer = &v
	}
	options, err := json.Marshal(struct {
		ReplayOptions
		Packages []string `json:"build_cmd_packages"`
	}{o, selectedPackages(r, profile)})
	if err != nil {
		return Response{}, err
	}
	raw, err := r.Canonical()
	if err != nil {
		return Response{}, err
	}
	id, err := replayID()
	if err != nil {
		return Response{}, err
	}
	resolved, _ := profile["id"].(string)
	if resolved == "" {
		resolved = r.Profile
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	pending, err := q.Length(ctx)
	if err != nil {
		return Response{}, err
	}
	if pending >= q.Options.MaxPendingJobs {
		return failure(529, "server overload"), nil
	}
	now := time.Now().UTC()
	_, err = q.DB.ExecContext(ctx, `INSERT INTO jobs(request_hash,replay_of,source_id,version,target,profile,status,stage,enqueued_at,expires_at,purge_at,client,request,options) VALUES(?,?,?,?,?,?,'queued','init',?,?,?,?,?,?)`, id, origin, source.ID, r.Version, r.Target, resolved, now, now, now.Add(q.Options.Retention), r.Client, string(raw), string(options))
	if err != nil {
		return Response{}, err
	}
	logging.OrNop(q.Options.Logger).Info("诊断重放作业已入队", zap.String("component", "queue"), zap.String("request_hash", id), zap.String("job_id", id), zap.String("replay_of", origin), zap.String("request_id", logging.RequestID(ctx)), zap.String("distribution", r.Distro), zap.String("version", r.Version), zap.String("target", r.Target), zap.String("profile", resolved))
	q.notify()
	return Response{Status: 202, Body: map[string]any{"id": id, "request_hash": id, "replay_of": origin}}, nil
}

func validImageTag(tag string) bool {
	if len(tag) > 128 || strings.HasPrefix(tag, ".") || strings.HasPrefix(tag, "-") {
		return false
	}
	for _, c := range tag {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// ExecutionContext 同时用于 worker 和重启接管。持久取消标记覆盖 claim/注册间窗口。
func (q *SQLite) ExecutionContext(parent context.Context, id string) (context.Context, func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	if q.active == nil {
		q.active = make(map[string]context.CancelFunc)
	}
	q.active[id] = cancel
	var detail string
	if err := q.DB.QueryRowContext(ctx, `SELECT COALESCE(detail,'') FROM jobs WHERE request_hash=?`, id).Scan(&detail); err != nil || detail == "cancelled" {
		cancel()
	}
	return ctx, func() {
		q.mu.Lock()
		delete(q.active, id)
		q.mu.Unlock()
		cancel()
	}
}

func (q *SQLite) Cancel(ctx context.Context, id string) (Response, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var status string
	err := q.DB.QueryRowContext(ctx, `SELECT status FROM jobs WHERE request_hash=? AND (purge_at IS NULL OR purge_at>?)`, id, time.Now().UTC()).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return failure(404, "job not found"), nil
	}
	if err != nil {
		return Response{}, err
	}
	if status != "queued" && status != "running" {
		return failure(409, "job already finished"), nil
	}
	now := time.Now().UTC()
	if status == "queued" {
		_, err = q.DB.ExecContext(ctx, `UPDATE jobs SET status='failed',stage='failed',detail='cancelled',finished_at=?,expires_at=?,purge_at=? WHERE request_hash=?`, now, now.Add(q.Options.BuildFailureTTL), now.Add(q.Options.Retention), id)
	} else {
		_, err = q.DB.ExecContext(ctx, `UPDATE jobs SET detail='cancelled' WHERE request_hash=?`, id)
		if err == nil && q.active[id] != nil {
			q.active[id]()
		}
	}
	if err != nil {
		logging.OrNop(q.Options.Logger).Error("取消作业失败", zap.String("component", "queue"), zap.String("request_hash", id), zap.Error(err))
	} else {
		logging.OrNop(q.Options.Logger).Info("作业取消已请求", zap.String("component", "queue"), zap.String("request_hash", id), zap.String("job_id", id), zap.String("previous_status", status), zap.String("request_id", logging.RequestID(ctx)))
	}
	return Response{Status: 202, Body: map[string]any{"id": id, "detail": "cancelled"}}, err
}

// JobDetail 不复用公开轮询格式；诊断记录按 purge_at 而非 expires_at 可见。
type JobDetail struct {
	ID         string          `json:"id"`
	ReplayOf   *string         `json:"replay_of"`
	Version    string          `json:"version"`
	Target     string          `json:"target"`
	Profile    string          `json:"profile"`
	Status     string          `json:"status"`
	Stage      string          `json:"stage"`
	Detail     string          `json:"detail"`
	Error      string          `json:"error"`
	Request    json.RawMessage `json:"request"`
	Result     json.RawMessage `json:"result"`
	Stages     json.RawMessage `json:"stages"`
	Options    json.RawMessage `json:"options"`
	EnqueuedAt time.Time       `json:"enqueued_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	ExpiresAt  *time.Time      `json:"expires_at"`
	PurgeAt    *time.Time      `json:"purge_at"`
}

const jobColumns = `request_hash,replay_of,version,target,profile,status,COALESCE(stage,'init'),COALESCE(detail,''),COALESCE(error,''),request,COALESCE(result,'null'),COALESCE(stages,'[]'),COALESCE(options,'{}'),enqueued_at,started_at,finished_at,expires_at,purge_at`

func scanJob(row interface{ Scan(...any) error }) (JobDetail, error) {
	var j JobDetail
	var request, result, stages, options string
	err := row.Scan(&j.ID, &j.ReplayOf, &j.Version, &j.Target, &j.Profile, &j.Status, &j.Stage, &j.Detail, &j.Error, &request, &result, &stages, &options, &j.EnqueuedAt, &j.StartedAt, &j.FinishedAt, &j.ExpiresAt, &j.PurgeAt)
	j.Request, j.Result, j.Stages, j.Options = json.RawMessage(request), json.RawMessage(result), json.RawMessage(stages), json.RawMessage(options)
	return j, err
}

func (q *SQLite) Job(ctx context.Context, id string) (JobDetail, error) {
	return scanJob(q.DB.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE request_hash=? AND (purge_at IS NULL OR purge_at>?)`, id, time.Now().UTC()))
}

func (q *SQLite) LogVisible(ctx context.Context, id string) (bool, error) {
	var n int
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE request_hash=? AND (purge_at IS NULL OR purge_at>?)`, id, time.Now().UTC()).Scan(&n)
	return n == 1, err
}

type JobList struct {
	Total int         `json:"total"`
	Items []JobDetail `json:"items"`
}

func (q *SQLite) Jobs(ctx context.Context, status string, page, size int) (JobList, error) {
	list := JobList{Items: []JobDetail{}}
	where := ` WHERE (purge_at IS NULL OR purge_at>?)`
	args := []any{time.Now().UTC()}
	switch status {
	case "", "all":
	case "queued", "running", "done", "failed":
		where += ` AND status=?`
		args = append(args, status)
	case "finished":
		where += ` AND status IN ('done','failed')`
	default:
		return list, fmt.Errorf("invalid status")
	}
	if page < 1 || size < 1 || size > 200 {
		return list, fmt.Errorf("invalid pagination")
	}
	if err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs`+where, args...).Scan(&list.Total); err != nil {
		return list, err
	}
	rows, err := q.DB.QueryContext(ctx, `SELECT `+jobColumns+` FROM jobs`+where+` ORDER BY enqueued_at DESC,request_hash DESC LIMIT ? OFFSET ?`, append(args, size, (page-1)*size)...)
	if err != nil {
		return list, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return list, err
		}
		list.Items = append(list.Items, j)
	}
	return list, rows.Err()
}
