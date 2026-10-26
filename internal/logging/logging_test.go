package logging

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gzzchh/asu-go/internal/config"
	"go.uber.org/zap"
)

func TestFormats(t *testing.T) {
	for _, format := range []string{"json", "console"} {
		t.Run(format, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			original := os.Stderr
			os.Stderr = w
			defer func() {
				os.Stderr = original
				_ = r.Close()
				_ = w.Close()
			}()
			logger, err := New(config.Log{Format: format, Level: "info"})
			if err != nil {
				t.Fatal(err)
			}
			logger.Debug("不可见")
			logger.Info("测试日志", zap.String("component", "test"))
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
			} else if json.Valid(data) || !strings.Contains(string(data), "INFO") || !strings.Contains(string(data), "\x1b[") {
				t.Fatalf("console 应使用易读彩色格式: %q", data)
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
