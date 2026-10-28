//go:build integration

package runner

import (
	"os/exec"
	"strings"
	"testing"
)

// 真实镜像仅用于显式运行的 Podman 冒烟，不影响默认单元测试。
func TestMirrorsPodman(t *testing.T) {
	for _, image := range []string{"docker.io/immortalwrt/imagebuilder:mediatek-filogic-openwrt-25.12.2", "ghcr.io/openwrt/imagebuilder:mediatek-filogic-v25.12.6"} {
		t.Run(image, func(t *testing.T) {
			fragment := mirrorScript([]Mirror{{"https://downloads.immortalwrt.org/", "https://mirrors.ustc.edu.cn/immortalwrt/"}, {"https://downloads.openwrt.org/", "https://mirrors.ustc.edu.cn/openwrt/"}})
			command := "set -eu\nif [ -f setup.sh ]; then sh setup.sh; fi\n" + fragment + "cat \"$repo\"\nmake info\nmake manifest PROFILE=bananapi_bpi-r3 PACKAGES=base-files STRIP_ABI=1\n"
			cmd := exec.Command("podman", "run", "--rm", "--pull=never", "--network=host", "--entrypoint=sh", image, "-c", command)
			output, err := cmd.CombinedOutput()
			t.Log(string(output))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "https://mirrors.ustc.edu.cn/") {
				t.Fatal("未应用镜像")
			}
		})
	}
}
