package queue

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gzzchh/asu-go/internal/buildreq"
	"github.com/gzzchh/asu-go/internal/runner"
	"github.com/gzzchh/asu-go/internal/store"
)

// ArtifactPublisher 是 store.Store 的发布子接口，便于注入失败测试。
type ArtifactPublisher interface {
	Publish(context.Context, string, string) error
}

func readResult(dir, profile, id string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(dir, "profiles.json"))
	if err != nil {
		return nil, errors.New("no JSON file found")
	}
	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON file: %w", err)
	}
	profiles, _ := result["profiles"].(map[string]any)
	p, ok := profiles[profile].(map[string]any)
	if !ok {
		return nil, errors.New("profile not found in JSON file")
	}
	delete(result, "profiles")
	for k, v := range p {
		result[k] = v
	}
	images, ok := result["images"].([]any)
	if !ok || len(images) == 0 {
		return nil, errors.New("no images found in JSON file")
	}
	for _, value := range images {
		image, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("invalid image in JSON file")
		}
		name, _ := image["name"].(string)
		if name == "" || name == "." || name != filepath.Base(name) || strings.ContainsAny(name, "/\\") {
			return nil, errors.New("invalid image name in JSON file")
		}
		file, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		image["sha256"] = hex.EncodeToString(hash.Sum(nil))
	}
	manifest := map[string]string{}
	file, err := os.Open(filepath.Join(dir, "manifest"))
	if err != nil {
		return nil, fmt.Errorf("no manifest file found: %w", err)
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if name, version, ok := strings.Cut(line, " - "); ok {
			manifest[name] = version
		} else if fields := strings.Fields(line); len(fields) >= 2 {
			manifest[fields[0]] = fields[1]
		}
	}
	err = scanner.Err()
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	cmd, err := os.ReadFile(filepath.Join(dir, "build_cmd_packages"))
	if err != nil {
		return nil, err
	}
	var epoch int64
	if number, ok := result["source_date_epoch"].(float64); ok {
		epoch = int64(number)
	} else {
		epoch, _ = strconv.ParseInt(fmt.Sprint(result["source_date_epoch"]), 10, 64)
	}
	result["manifest"] = manifest
	result["id"] = profile
	result["bin_dir"] = id
	result["build_cmd_packages"] = strings.Fields(string(cmd))
	result["build_at"] = time.Unix(epoch, 0).UTC().Format("2006-01-02T15:04:05.000000Z")
	result["detail"] = "done"
	return result, nil
}

// Complete 同时用于普通 worker 与 Runner.Takeover，不暴露 Runner.Result 的内部字段。
func (q *SQLite) Complete(ctx context.Context, id string, result runner.Result, runErr error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	var cancelled bool
	if err := q.DB.QueryRowContext(ctx, `SELECT COALESCE(detail,'')='cancelled' FROM jobs WHERE request_hash=?`, id).Scan(&cancelled); err != nil {
		return err
	}
	if cancelled {
		// 取消也可能落在 Runner 返回与 Complete 之间；此时失败环境可能已被保留。
		// 再次幂等清理，保证管理员取消永远不保留容器。
		var cleanupErr error
		if result.Handle != "" {
			cleanupErr = errors.Join(q.Runner.Runtime.Kill(ctx, result.Handle), q.Runner.Runtime.Remove(ctx, result.Handle))
		}
		runErr = &runner.Error{Kind: "cancelled", Cause: cleanupErr}
	}
	var request, profile, status, options string
	if err := q.DB.QueryRowContext(ctx, `SELECT request,profile,status,COALESCE(options,'{}') FROM jobs WHERE request_hash=?`, id).Scan(&request, &profile, &status, &options); err != nil {
		return err
	}
	if status != "running" {
		return nil
	}
	var r buildreq.Request
	if err := json.Unmarshal([]byte(request), &r); err != nil {
		return err
	}
	var public map[string]any
	if runErr == nil {
		public, runErr = readResult(result.ArtifactDir, profile, id)
	}
	if runErr == nil {
		if strings.HasPrefix(id, "rp_") {
			// 诊断产物单独保存，不进入公开 store 命名空间。
			runErr = (&store.Local{Dir: filepath.Join(q.Options.DataDir, "diagnostics")}).Publish(ctx, id, result.ArtifactDir)
		} else {
			runErr = q.Publisher.Publish(ctx, id, result.ArtifactDir)
		}
	}
	if result.ArtifactDir != "" && runErr != nil {
		_ = os.RemoveAll(result.ArtifactDir)
	}
	status, detail, message := "done", "done", ""
	ttl := q.ttl(r)
	if runErr != nil {
		status = "failed"
		detail = "Error while building firmware. See stdout/stderr"
		message = runErr.Error()
		ttl = q.Options.BuildFailureTTL
		var failure *runner.Error
		if errors.As(runErr, &failure) {
			message = failure.Tail
			if message == "" {
				message = runErr.Error()
			}
			if failure.Kind != "failed" {
				detail = failure.Kind
			}
		}
		for _, text := range []string{"No JSON file found", "Profile not found in JSON file", "No images found in JSON file"} {
			if strings.Contains(strings.ToLower(runErr.Error()), strings.ToLower(text)) {
				detail = text
			}
		}
		if strings.Contains(runErr.Error(), "Selected packages exceed device storage") || strings.Contains(message, "is too big") || strings.Contains(message, "out of space?") {
			detail = "Selected packages exceed device storage"
		}
	}
	data, err := json.Marshal(public)
	if err != nil {
		return err
	}
	if result.Stages == nil {
		result.Stages = []runner.Stage{}
	}
	stages, err := json.Marshal(result.Stages)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tx, err := q.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := tx.ExecContext(ctx, `UPDATE jobs SET status=?,stage=?,detail=?,error=?,result=?,stages=?,finished_at=?,expires_at=?,purge_at=? WHERE request_hash=? AND status='running'`, status, status, detail, message, string(data), string(stages), now, now.Add(ttl), now.Add(q.Options.Retention), id)
	if err != nil {
		return err
	}
	n, err := changed.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 && !strings.HasPrefix(id, "rp_") {
		if runErr != nil {
			if err = count(ctx, tx, "failures", r.Version); err != nil {
				return err
			}
		}
		// 包统计只计一次实际构建，不计重复提交或重放。
		var opts struct {
			Packages []string `json:"stat_packages"`
		}
		if err = json.Unmarshal([]byte(options), &opts); err != nil {
			return err
		}
		manifestStarted := false
		for _, s := range result.Stages {
			if s.Name == "validate_manifest" || s.Name == "building_image" {
				manifestStarted = true
			}
		}
		if manifestStarted {
			for _, p := range opts.Packages {
				if err = count(ctx, tx, "package", p); err != nil {
					return err
				}
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	q.Runner.Hub.Forget(id)
	return nil
}
