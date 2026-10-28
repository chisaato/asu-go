//go:build integration

package api

import (
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/chisaato/asu-go/internal/runner"
	"github.com/chisaato/asu-go/internal/runtime"
)

func TestPodmanBuildSubmitPoll(t *testing.T) {
	socket := os.Getenv("ASU_TEST_RUNTIME_SOCKET")
	if socket == "" {
		socket = "unix:///var/run/docker.sock"
	}
	driver := os.Getenv("ASU_TEST_RUNTIME_DRIVER")
	if driver == "" {
		driver = "podman"
	}
	rt, err := runtime.Open(driver, runtime.Options{Socket: socket})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	h := newBuildHarness(t)
	h.q.Runner.Runtime = rt
	// 只在测试中注入小脚本；生产仍使用 Runner.Script 与 ImageBuilder。
	h.q.Runner.BuildScript = func(j runner.Job) (string, error) {
		return fmt.Sprintf(`set -eu
# Alpine 测试夹具根目录默认只读；模拟 ImageBuilder 的可写工作目录。
chmod u+w .
echo '##asu:stage=container_setup'
echo '##asu:stage=validate_manifest'
echo '##asu:stage=building_image'
sleep 1
mkdir -p ./%s
printf 'firmware' > ./%s/firmware.bin
printf 'base - 1\nluci - 2\n' > ./%s/manifest
printf 'base luci\n' > ./%s/build_cmd_packages
printf '%%s' '{"source_date_epoch":1700000000,"profiles":{"generic":{"images":[{"name":"firmware.bin","type":"sysupgrade"}]}}}' > ./%s/profiles.json
`, j.ID, j.ID, j.ID, j.ID, j.ID), nil
	}
	h.start()
	w, queued := h.call("POST", "/api/v1/build", buildBody)
	if w.Code != 202 {
		t.Fatal(w.Code, queued)
	}
	id := queued["request_hash"].(string)
	result := h.poll(id, 200)
	if result["detail"] != "done" || result["bin_dir"] != id || result["id"] != "generic" {
		t.Fatal(result)
	}
	download := httptest.NewRecorder()
	h.router.ServeHTTP(download, httptest.NewRequest("GET", "/store/"+id+"/firmware.bin", nil))
	if download.Code != 200 || download.Body.String() != "firmware" {
		t.Fatal(download)
	}
}
