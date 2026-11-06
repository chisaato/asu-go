package runner

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed assets/apk-abi
var patchAssets embed.FS

// 补丁仅注入构建工作目录，不进入 asu-files 或最终固件。
func apkABIInputs() (map[string]string, error) {
	const root = "assets/apk-abi"
	files := make(map[string]string)
	err := fs.WalkDir(patchAssets, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := patchAssets.ReadFile(name)
		if err != nil {
			return err
		}
		files[".asu/patches/apk-abi/"+strings.TrimPrefix(name, root+"/")] = string(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("读取 APK ABI 补丁资源: %w", err)
	}
	return files, nil
}
