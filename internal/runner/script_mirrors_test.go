package runner

import (
	"archive/tar"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chisaato/asu-go/internal/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRunnerMirrors(t *testing.T) {
	r, rt, j := setupRunner(t, runtime.FakePlan{Output: "##asu:mirrors=1\n", Archives: map[string][]byte{"/builder/job": tarData(t, "job/firmware.bin", "image", tar.TypeReg)}})
	r.Mirrors = []Mirror{{"https://official.example/", "https://mirror.example/"}}
	core, logs := observer.New(zap.InfoLevel)
	r.Logger = zap.New(core)
	if _, err := r.Run(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rt.spec.Command[2], "https://mirror.example/") {
		t.Fatal("Runner 未传递镜像配置")
	}
	entries := logs.FilterMessage("已应用 1 条镜像替换").All()
	if len(entries) != 1 || entries[0].ContextMap()["component"] != "runner" || entries[0].ContextMap()["mirror_count"] != int64(1) {
		t.Fatal("未记录镜像应用日志", entries)
	}
}

func TestMirrorScript(t *testing.T) {
	for _, repo := range []string{"repositories", "repositories.conf"} {
		t.Run(repo, func(t *testing.T) {
			dir := t.TempDir()
			from := "https://official.example/a.&'|#[x]$^*/"
			to := "https://mirror.example/b.&'|#$(touch injected)/"
			original := from + "packages.adb\nsrc/gz base " + from + "base\n#" + from + "comment\nhttps://other.example/" + from + "nested\n"
			if err := os.WriteFile(filepath.Join(dir, repo), []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-eu", "-c", mirrorScript([]Mirror{{From: from, To: to}}))
			cmd.Dir = dir
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			got, err := os.ReadFile(filepath.Join(dir, repo))
			if err != nil {
				t.Fatal(err)
			}
			want := to + "packages.adb\nsrc/gz base " + to + "base\n#" + from + "comment\nhttps://other.example/" + from + "nested\n"
			if string(got) != want {
				t.Fatalf("got %q, want %q", got, want)
			}
			if _, err := os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
				t.Fatal("shell 注入")
			}
		})
	}
}

func TestScriptMirrorsOrder(t *testing.T) {
	for _, mode := range []string{"append", "replace"} {
		j := Job{Setup: true, Mirrors: []Mirror{{"https://official.example/", "https://mirror.example/"}}, Repositories: map[string]string{"custom": "https://official.example/custom/"}, RepositoriesMode: mode}
		script, err := Script(j)
		if err != nil {
			t.Fatal(err)
		}
		setup, mirror, custom, info := strings.Index(script, "sh setup.sh"), strings.Index(script, "sed -i"), strings.Index(script, "custom=.asu-repositories"), strings.Index(script, "capture .asu-info make info")
		if setup >= mirror || mirror >= custom || custom >= info {
			t.Fatal("setup、镜像、用户仓库、make info 的顺序错误")
		}
	}
	script, err := Script(Job{})
	if err != nil || strings.Contains(script, "sed -i") || strings.Contains(script, "##asu:mirrors=") {
		t.Fatal("空列表不应生成替换", err)
	}
}

func TestMirrorsPreserveCustomRepositories(t *testing.T) {
	for _, repo := range []string{"repositories", "repositories.conf"} {
		for _, mode := range []string{"append", "replace"} {
			dir := t.TempDir()
			prefix := ""
			custom := ".asu-repositories-apk"
			if repo == "repositories.conf" {
				prefix, custom = "src/gz base ", ".asu-repositories-opkg"
			}
			for file, value := range map[string]string{repo: prefix + "https://official.example/original/\n", custom: prefix + "https://official.example/custom/\n"} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(value), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(dir, "dl"), 0o700); err != nil {
				t.Fatal(err)
			}
			script, err := Script(Job{Mirrors: []Mirror{{"https://official.example/", "https://mirror.example/"}}, Repositories: map[string]string{"base": "https://official.example/custom/"}, RepositoriesMode: mode})
			if err != nil {
				t.Fatal(err)
			}
			fragment, _, _ := strings.Cut(script, "capture .asu-info make info")
			cmd := exec.Command("sh", "-c", fragment)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			got, err := os.ReadFile(filepath.Join(dir, repo))
			if err != nil || !strings.Contains(string(got), "https://official.example/custom/") || strings.Contains(string(got), "https://mirror.example/custom/") {
				t.Fatalf("%s %s 改写了自定义仓库: %s, %v", repo, mode, got, err)
			}
			if strings.Contains(string(got), "https://mirror.example/original/") != (mode == "append") {
				t.Fatalf("原始仓库替换错误: %s", got)
			}
		}
	}
}
