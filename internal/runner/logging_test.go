package runner

import (
	"archive/tar"
	"context"
	"strings"
	"testing"

	"github.com/chisaato/asu-go/internal/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestLifecycleApplicationLogs(t *testing.T) {
	r, _, j := setupRunner(t, runtime.FakePlan{Output: "private-build-output\n##asu:stage=init\n##asu:stage=building_image\n", Archives: map[string][]byte{"/builder/job": tarData(t, "job/image.bin", "image", tar.TypeReg)}})
	core, logs := observer.New(zapcore.DebugLevel)
	r.Logger = zap.New(core)
	if _, err := r.Run(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"start", "wait", "remove"} {
		if logs.FilterField(zap.String("operation", operation)).Len() != 1 {
			t.Fatalf("缺少 %s 日志: %v", operation, logs.All())
		}
	}
	if logs.FilterMessage("构建阶段变化").Len() != 2 || logs.FilterMessage("作业开始").Len() != 1 || logs.FilterMessage("作业完成").Len() != 1 {
		t.Fatal(logs.All())
	}
	metadata := logs.FilterMessage("构建镜像元数据").All()
	if len(metadata) != 1 || metadata[0].Level != zapcore.InfoLevel || metadata[0].ContextMap()["image"] != j.Image || metadata[0].ContextMap()["working_dir"] != "/builder" || metadata[0].ContextMap()["user"] != "" {
		t.Fatal("缺少镜像元数据 info 日志", metadata)
	}
	for _, entry := range logs.All() {
		if entry.ContextMap()["component"] != "runner" || entry.ContextMap()["request_hash"] != j.ID {
			t.Fatal(entry)
		}
		for _, f := range entry.Context {
			if strings.Contains(f.String, "private-build-output") {
				t.Fatal("构建输出进入应用日志")
			}
		}
	}
}

func TestFailureDoesNotLogBuildTail(t *testing.T) {
	r, _, j := setupRunner(t, runtime.FakePlan{Output: "secret-build-tail\n", ExitCode: 2})
	core, logs := observer.New(zapcore.DebugLevel)
	r.Logger = zap.New(core)
	if _, err := r.Run(context.Background(), j); err == nil {
		t.Fatal("应失败")
	}
	entries := logs.FilterMessage("作业失败").All()
	if len(entries) != 1 || entries[0].Level != zapcore.ErrorLevel || strings.Contains(entries[0].ContextMap()["error"].(string), "secret-build-tail") {
		t.Fatal(entries)
	}
}
