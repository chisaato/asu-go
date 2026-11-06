package runner

import (
	"os/exec"
	"testing"
)

func TestAPKABI(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 不可用，跳过 APK ABI 资源回归测试")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("GNU make 不可用，跳过 APK ABI 资源回归测试")
	}
	cmd := exec.Command(python, "-B", "-m", "unittest", "discover", "-s", "testdata/apk-abi", "-p", "test_*.py", "-v")
	output, err := cmd.CombinedOutput()
	t.Logf("Python APK ABI 回归测试：\n%s", output)
	if err != nil {
		t.Fatalf("Python APK ABI 回归测试失败：%v", err)
	}
}
