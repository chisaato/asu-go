//go:build integration

package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/runtime"
	"github.com/chisaato/asu-go/internal/stream"
)

// 显式启用后才运行真实构建；两个任务串行，不能作为公平性能基准。
func TestAPKABIImageBuilderComparison(t *testing.T) {
	image := os.Getenv("ASU_TEST_APK_ABI_IMAGE")
	if image == "" {
		t.Skip("设置 ASU_TEST_APK_ABI_IMAGE 后才运行真实 APK ABI 产物对照")
	}
	socket := os.Getenv("ASU_TEST_RUNTIME_SOCKET")
	if socket == "" {
		socket = "unix:///var/run/docker.sock"
	}
	driver := os.Getenv("ASU_TEST_RUNTIME_DRIVER")
	if driver == "" {
		driver = "podman"
	}
	rt, err := runtime.Open(driver, runtime.Options{Socket: socket, PullPolicy: "never"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	const reportRoot = "/tmp/opencode/asu-apk-abi-validation"
	if err := os.MkdirAll(reportRoot, 0700); err != nil {
		t.Fatal(err)
	}
	reportDir, err := os.MkdirTemp(reportRoot, "apk-abi-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("日志与产物保留于 %s", reportDir)
	// 仅使用生产缓存过滤器共享非空 .apk/.ipk 普通文件，不共享索引。
	cache := &PackageCache{Enabled: true, Dir: filepath.Join(reportDir, "package-cache"), MaxBytes: 20 << 30}
	r := &Runner{
		Runtime: rt, Cache: cache, JobTimeout: 15 * time.Minute,
		Mirrors: []Mirror{
			{From: "https://downloads.immortalwrt.org/", To: "https://mirrors.ustc.edu.cn/immortalwrt/"},
			{From: "https://downloads.openwrt.org/", To: "https://mirrors.ustc.edu.cn/openwrt/"},
		},
	}
	// ID 也保持一致，避免仅因输出目录不同而引入额外差异。
	job := Job{ID: filepath.Base(reportDir), Image: image, Profile: "cmcc_rax3000m", Packages: []string{"base-files"}, PullPolicy: "never"}
	t.Log("Packages=base-files 会保留 ImageBuilder 镜像默认包，不是仅安装一个包")
	for _, phase := range []string{"baseline", "optimized"} {
		phaseDir := filepath.Join(reportDir, phase)
		hub, err := stream.NewHub(phaseDir, 1000)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = hub.Close() })
		r.Hub = hub
		r.BuildScript = nil
		if phase == "baseline" {
			r.BuildScript = func(j Job) (string, error) {
				script, err := Script(j)
				if err != nil {
					return "", err
				}
				const invocation = "  python3 .asu/patches/apk-abi/apply.py\n"
				if strings.Count(script, invocation) != 1 {
					return "", fmt.Errorf("生产补丁调用行数量不是 1，拒绝生成对照脚本")
				}
				return strings.Replace(script, invocation, "  : # 对照组跳过补丁\n", 1), nil
			}
		}
		job.ArtifactDir = filepath.Join(phaseDir, "artifacts")
		job.OnHandle = func(_ context.Context, handle runtime.Handle) error {
			// 只兜底清理本测试创建的句柄，不枚举或回收其他容器。
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				state, err := rt.Inspect(ctx, handle)
				if err != nil {
					t.Error(err)
					return
				}
				if state.Exists {
					if err := rt.Kill(ctx, handle); err != nil {
						t.Error(err)
					}
					if err := rt.Remove(ctx, handle); err != nil {
						t.Error(err)
					}
				}
			})
			return nil
		}
		started := time.Now()
		result, runErr := r.Run(context.Background(), job)
		t.Logf("%s: exit=%d duration=%s（不作公平性能比较）", phase, result.ExitCode, time.Since(started))
		if runErr != nil {
			t.Fatalf("%s 构建失败：%v；完整日志：%s", phase, runErr, filepath.Join(phaseDir, "logs", job.ID+".log"))
		}
		if result.ExitCode != 0 {
			t.Fatalf("%s 退出码不是 0：%d", phase, result.ExitCode)
		}
		log, err := os.ReadFile(filepath.Join(phaseDir, "logs", job.ID+".log"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(log, []byte("##asu:mirrors=2\n")) {
			t.Fatalf("%s 缺少生产镜像替换标记", phase)
		}
		const applied = "[asu apk-abi] 已应用："
		if phase == "optimized" && !bytes.Contains(log, []byte(applied)) {
			t.Fatal("optimized 缺少补丁成功应用标记，不能把跳过优化视为验证成功")
		}
		if phase == "baseline" && bytes.Contains(log, []byte("[asu apk-abi]")) {
			t.Fatal("baseline 意外运行了补丁")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		state, inspectErr := rt.Inspect(ctx, result.Handle)
		cancel()
		if inspectErr != nil || state.Exists {
			t.Fatalf("%s 容器没有完成清理：%+v，%v", phase, state, inspectErr)
		}
	}
	baseline := filepath.Join(reportDir, "baseline", "artifacts")
	optimized := filepath.Join(reportDir, "optimized", "artifacts")
	left := apkABIComparisonFiles(t, baseline)
	right := apkABIComparisonFiles(t, optimized)
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("待比较产物集合不同：baseline=%v optimized=%v", left, right)
	}
	for _, name := range left {
		apkABICompareFile(t, name, filepath.Join(baseline, name), filepath.Join(optimized, name))
	}
	t.Log("manifest、存在时的 profiles.json 及所有 sysupgrade 固件逐字节一致；SBOM 含动态字段，不要求整个 sha256sums 文件一致，也不声称全部产物一致")
}

func apkABIComparisonFiles(t *testing.T, root string) []string {
	t.Helper()
	names := []string{"manifest"}
	firmware := 0
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		base := entry.Name()
		isFirmware := strings.Contains(base, "sysupgrade.") && (strings.HasSuffix(base, ".bin") || strings.HasSuffix(base, ".itb") || strings.HasSuffix(base, ".img") || strings.HasSuffix(base, ".gz") || strings.HasSuffix(base, ".tar"))
		if base != "profiles.json" && !isFirmware {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("待比较产物不是非空普通文件：%s", name)
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		names = append(names, rel)
		if isFirmware {
			firmware++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if firmware == 0 {
		t.Fatalf("%s 没有任何 sysupgrade 固件", root)
	}
	sort.Strings(names)
	return names
}

func apkABICompareFile(t *testing.T, name, left, right string) {
	t.Helper()
	a, err := os.Open(left)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := os.Open(right)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	hash := sha256.New()
	bufA, bufB := make([]byte, 128<<10), make([]byte, 128<<10)
	var size int64
	for {
		nA, errA := io.ReadFull(a, bufA)
		nB, errB := io.ReadFull(b, bufB)
		for _, err := range []error{errA, errB} {
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				t.Fatal(err)
			}
		}
		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			t.Fatalf("%s 字节不一致（首个不同块偏移 %d）", name, size)
		}
		_, _ = hash.Write(bufA[:nA])
		size += int64(nA)
		if errA != nil || errB != nil {
			break
		}
	}
	if size == 0 {
		t.Fatalf("%s 是空文件", name)
	}
	t.Logf("逐字节一致：%s size=%d sha256=%x", name, size, hash.Sum(nil))
}
