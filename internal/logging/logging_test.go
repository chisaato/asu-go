package logging

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gzzchh/asu-go/internal/config"
	"go.uber.org/zap"
)

func TestFormats(t *testing.T) {
	for _, format := range []string{"json", "text", "plain", "console"} {
		t.Run(format, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			original := os.Stdout
			os.Stdout = w
			defer func() {
				os.Stdout = original
				_ = r.Close()
				_ = w.Close()
			}()
			logger, err := New(config.Log{Format: format, Level: "info"})
			if err != nil {
				t.Fatal(err)
			}
			logger.Debug("不可见")
			logger.Info("测试日志", zap.String("component", "test"), zap.String("msg_with_space", "hello world"), zap.Int("code", 200))
			_ = logger.Sync()
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "测试日志") || strings.Contains(string(data), "不可见") {
				t.Fatalf("日志内容或级别不正确: %s", data)
			}
			var entry map[string]any
			if format == "json" {
				if err := json.Unmarshal(data, &entry); err != nil {
					t.Fatal(err)
				}
				if entry["component"] != "test" || entry["level"] != "info" {
					t.Fatalf("结构化字段不正确: %v", entry)
				}
			} else {
				str := string(data)
				if json.Valid(data) || !strings.Contains(str, "INFO") || !strings.Contains(str, "\x1b[") {
					t.Fatalf("%s 应使用易读彩色格式: %q", format, str)
				}
				if strings.Contains(str, `{"component"`) {
					t.Fatalf("%s 不应包含 JSON 大对象: %q", format, str)
				}
				if !strings.Contains(str, "component=test") || !strings.Contains(str, `msg_with_space="hello world"`) || !strings.Contains(str, "code=200") {
					t.Fatalf("%s 应使用 label=value 格式: %q", format, str)
				}
			}
		})
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, cfg := range []config.Log{{Format: "xml", Level: "info"}, {Format: "json", Level: "invalid"}} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("未拒绝非法日志配置: %+v", cfg)
		}
	}
}

func TestFileLogging(t *testing.T) {
	for _, format := range []string{"json", "text", "plain", "console"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "asu.log")
			for i := 0; i < 2; i++ {
				logger, err := New(config.Log{Format: format, Level: "warn", File: path})
				if err != nil {
					t.Fatal(err)
				}
				logger.Info("不可见")
				logger.Warn("警告")
				logger.Error("失败", zap.String("request_hash", "test-hash"))
				_ = logger.Sync()
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Count(text, "警告") != 2 || strings.Count(text, "失败") != 2 || !strings.Contains(text, "test-hash") || strings.Contains(text, "不可见") || strings.Contains(text, "\x1b[") || strings.Contains(text, "stacktrace") || strings.Contains(text, "testing.tRunner") {
				t.Fatalf("文件日志格式、追加、级别或堆栈不符: %q", text)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal(info, err)
			}
		})
	}
}

func TestFileRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asu.log")
	if err := os.WriteFile(path+".1", []byte("旧备份"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	file, err = openFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	for name, size := range map[string]int64{path: 0, path + ".1": maxFileSize + 1} {
		info, err := os.Stat(name)
		if err != nil || info.Size() != size || info.Mode().Perm() != 0600 {
			t.Fatal(name, info, err)
		}
	}
	if _, err := New(config.Log{Format: "json", Level: "info", File: filepath.Dir(path)}); err == nil || !strings.Contains(err.Error(), "log.file") {
		t.Fatalf("应拒绝目录路径: %v", err)
	}
}

func TestStacktraceOnlyDPanicAndAbove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asu.log")
	logger, err := New(config.Log{Format: "json", Level: "debug", File: path})
	if err != nil {
		t.Fatal(err)
	}
	logger.Error("无堆栈")
	logger.DPanic("有堆栈")
	_ = logger.Sync()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "stacktrace") || !strings.Contains(lines[1], "stacktrace") {
		t.Fatalf("堆栈级别不符: %s", data)
	}
}
