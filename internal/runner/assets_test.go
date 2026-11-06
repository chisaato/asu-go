package runner

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPKABIInputsArchive(t *testing.T) {
	archive, err := inputs(Job{inputUID: 1234, inputGID: 5678})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{}
	if err := fs.WalkDir(patchAssets, "assets/apk-abi", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := patchAssets.ReadFile(name)
		want[".asu/patches/apk-abi/"+strings.TrimPrefix(name, "assets/apk-abi/")] = data
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"apply.py", "query.py"} {
		if len(want[".asu/patches/apk-abi/"+name]) == 0 {
			t.Fatalf("缺少生产资源 %s", name)
		}
	}
	seen := map[string]bool{}
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if seen[hdr.Name] {
			t.Fatalf("重复归档路径 %s", hdr.Name)
		}
		seen[hdr.Name] = true
		if hdr.Uid != 1234 || hdr.Gid != 5678 {
			t.Fatalf("%s 属主为 %d:%d", hdr.Name, hdr.Uid, hdr.Gid)
		}
		if hdr.Typeflag == tar.TypeDir {
			if hdr.Mode != 0o755 {
				t.Fatalf("目录 %s 权限 %o", hdr.Name, hdr.Mode)
			}
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		expected, ok := want[hdr.Name]
		if !ok || !bytes.Equal(data, expected) || hdr.Mode != 0o644 || hdr.Typeflag != tar.TypeReg {
			t.Fatalf("资源内容、路径或权限错误: %+v", hdr)
		}
		delete(want, hdr.Name)
	}
	if len(want) != 0 {
		t.Fatalf("缺少归档资源: %v", want)
	}
	for _, dir := range []string{".asu/", ".asu/patches/", ".asu/patches/apk-abi/"} {
		if !seen[dir] {
			t.Fatalf("缺少父目录 %s", dir)
		}
	}
}

func TestScriptAPKABIOrder(t *testing.T) {
	for _, mode := range []string{"append", "replace"} {
		script, err := Script(Job{Setup: true, Mirrors: []Mirror{{"https://official.example/", "https://mirror.example/"}}, Repositories: map[string]string{"custom": "https://custom.example/"}, RepositoriesMode: mode})
		if err != nil {
			t.Fatal(err)
		}
		previous := -1
		for _, marker := range []string{"sh setup.sh", "sed -i", "custom=.asu-repositories", "if [ \"$repo\" = repositories.conf ]", "python3 .asu/patches/apk-abi/apply.py", "capture .asu-info make info"} {
			index := strings.Index(script, marker)
			if index <= previous {
				t.Fatalf("%s: 执行顺序错误，标记 %q", mode, marker)
			}
			previous = index
		}
	}
}

func TestScriptAPKABIPython(t *testing.T) {
	for _, tc := range []struct {
		name   string
		python string
		code   int
	}{
		{name: "missing"},
		{name: "success", python: "#!/bin/sh\nprintf 'apply=%s\\n' \"$1\"\n"},
		{name: "failure", python: "#!/bin/sh\nexit 37\n", code: 37},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(dir, "dl"), 0o755); err != nil {
				t.Fatal(err)
			}
			// 隔离 PATH，确保不依赖宿主是否安装 Python。
			for _, name := range []string{"date", "find", "wc"} {
				tool, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(tool, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.python != "" {
				if err := os.WriteFile(filepath.Join(bin, "python3"), []byte(tc.python), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			script, err := Script(Job{})
			if err != nil {
				t.Fatal(err)
			}
			fragment, _, found := strings.Cut(script, "capture .asu-info make info")
			if !found {
				t.Fatal("缺少 make info")
			}
			cmd := exec.Command("sh", "-c", fragment+"printf 'reached-info\\n'\n")
			cmd.Dir = dir
			cmd.Env = []string{"PATH=" + bin}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err = cmd.Run()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatal(err)
				}
				code = exitErr.ExitCode()
			}
			if code != tc.code {
				t.Fatalf("退出码 %d，期望 %d: %s", code, tc.code, stderr.String())
			}
			if strings.Contains(stdout.String(), "reached-info") != (tc.code == 0) {
				t.Fatalf("apply 失败后仍执行 info 或正常流程中断: %s", stdout.String())
			}
			if tc.name == "missing" && (!strings.Contains(stderr.String(), "未找到 python3") || strings.Contains(stdout.String(), "未找到 python3")) {
				t.Fatalf("缺少 stderr 告警: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			if tc.name == "success" && !strings.Contains(stdout.String(), "apply=.asu/patches/apk-abi/apply.py") {
				t.Fatalf("未调用正确补丁入口: %s", stdout.String())
			}
		})
	}
}
