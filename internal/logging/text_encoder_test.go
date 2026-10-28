package logging

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gzzchh/asu-go/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type sampleObject struct {
	Name string
	Age  int
}

func (s sampleObject) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("name", s.Name)
	enc.AddInt("age", s.Age)
	return nil
}

func TestTextEncoderFieldTypes(t *testing.T) {
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

	logger, err := New(config.Log{Format: "text", Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}

	subLogger := logger.With(
		zap.String("app", "asu"),
		zap.Namespace("req"),
		zap.String("id", "req-123"),
	)

	subLogger.Info("服务请求",
		zap.Bool("active", true),
		zap.Int("status", 200),
		zap.Int64("bytes", 4096),
		zap.Float64("latency_ms", 12.34),
		zap.Duration("timeout", 5*time.Second),
		zap.String("empty_val", ""),
		zap.String("url", "https://example.com/test?a=1&b=2"),
		zap.String("quote_val", `hello "world"`),
		zap.Strings("tags", []string{"openwrt", "sysupgrade"}),
		zap.Object("user", sampleObject{Name: "alice", Age: 30}),
		zap.Error(errors.New("something went wrong")),
	)

	_ = subLogger.Sync()
	_ = w.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	line := string(data)

	// 验证上下文 namespace 与字段
	if !strings.Contains(line, "app=asu") {
		t.Fatalf("缺失上下文 app 字段: %s", line)
	}
	if !strings.Contains(line, "req.id=req-123") {
		t.Fatalf("缺失 namespace 字段: %s", line)
	}
	// 验证各基础类型与特殊字符转义
	expectedSubstrings := []string{
		"req.active=true",
		"req.status=200",
		"req.bytes=4096",
		"req.latency_ms=12.34",
		"req.timeout=5s",
		`req.empty_val=""`,
		`req.url="https://example.com/test?a=1&b=2"`,
		`req.quote_val="hello \"world\""`,
		`req.tags=["openwrt","sysupgrade"]`,
		`req.user={"age":30,"name":"alice"}`,
		`req.error="something went wrong"`,
	}
	for _, expected := range expectedSubstrings {
		if !strings.Contains(line, expected) {
			t.Errorf("未在输出中找到预期字段 %q, 实际输出: %s", expected, line)
		}
	}
}
