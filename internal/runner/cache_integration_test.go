//go:build integration

package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/runtime"
	"github.com/chisaato/asu-go/internal/stream"
)

// 全新容器只播种包，热构建前后逐包比较 mtime/大小，不能靠静默 wget 的日志判断下载量。
func TestPodmanPackageOnlyCache(t *testing.T) {
	socket := os.Getenv("ASU_TEST_RUNTIME_SOCKET")
	if socket == "" {
		socket = "unix:///var/run/docker.sock"
	}
	rt, err := runtime.Open("podman", runtime.Options{Socket: socket, PullPolicy: "never"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close() }()
	for _, image := range []string{"ghcr.io/openwrt/imagebuilder:mediatek-filogic-v25.12.6", "docker.io/immortalwrt/imagebuilder:mediatek-filogic-openwrt-25.12.2"} {
		t.Run(image, func(t *testing.T) {
			cache := &PackageCache{Enabled: true, Dir: t.TempDir(), MaxBytes: 20 << 30}
			hub, err := stream.NewHub(t.TempDir(), 10000)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = hub.Close() }()
			r := &Runner{Runtime: rt, Hub: hub, Cache: cache, Verbose: true, JobTimeout: 15 * time.Minute}
			r.BuildScript = func(j Job) (string, error) {
				s, err := Script(j)
				if err != nil {
					return "", err
				}
				// 预置包没有索引；make manifest 运行前便取证，避免把 manifest 的下载误算成命中。
				before := "find dl -type f -name '*.apk' -exec stat -c '%n %Y %s' {} \\; | LC_ALL=C sort > .cache-before\nfind dl -type f ! -name '*.apk' ! -name '*.ipk' > .cache-index-before\n"
				after := "find dl -type f -name '*.apk' -exec stat -c '%n %Y %s' {} \\; | LC_ALL=C sort > .cache-after\nfind dl -type f -name 'APKINDEX*' > .cache-index-after\ncp .cache-before .cache-after .cache-index-before .cache-index-after " + quote(filepath.Join(j.WorkingDir, j.ID)) + "\n"
				return before + s + after, nil
			}
			for _, phase := range []string{"cold", "hot"} {
				j := Job{ID: "package-cache-" + phase, Image: image, Profile: "abt_asr3000", Packages: []string{"htop"}, PullPolicy: "never", ArtifactDir: t.TempDir()}
				var runErr error
				for attempt := 0; attempt < 3; attempt++ {
					_, runErr = r.Run(context.Background(), j)
					if runErr == nil {
						break
					}
					t.Logf("%s 第 %d 次失败（网络可重试）: %v", phase, attempt+1, runErr)
				}
				if runErr != nil {
					t.Fatal(runErr)
				}
				read := func(name string) string {
					t.Helper()
					b, err := os.ReadFile(filepath.Join(j.ArtifactDir, name))
					if err != nil {
						t.Fatal(err)
					}
					return string(b)
				}
				if read(".cache-index-before") != "" {
					t.Fatal("播种包含非包文件")
				}
				if read(".cache-index-after") == "" {
					t.Fatal("未独立拉取索引")
				}
				if phase == "hot" {
					before, after := read(".cache-before"), read(".cache-after")
					if before == "" || before != after {
						t.Fatal("仅包缓存未全部复用，停止：包名、mtime 或大小改变")
					}
					t.Logf("新容器仅播种包、重新拉取索引，%d 个 apk 的 mtime/大小完全不变", strings.Count(before, "\n"))
				}
				entries, err := os.ReadDir(filepath.Join(cache.Dir, cacheKey(image)))
				if err != nil || len(entries) == 0 {
					t.Fatal("未回收包", err)
				}
				for _, e := range entries {
					if !packageName(e.Name()) {
						t.Fatal("缓存混入索引", e.Name())
					}
				}
				t.Logf("%s 完成，共享仓库 %d 个包", phase, len(entries))
			}
		})
	}
}
