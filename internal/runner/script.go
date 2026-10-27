package runner

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func workingDir(dir string) string {
	if dir == "" {
		return "/builder"
	}
	return path.Clean(dir)
}

// Script 将所有请求值作为单个 shell 参数传递；调用方负责包选择、仓库 allow-list 等业务校验。
func Script(j Job) (string, error) {
	values := []string{j.ID, j.Image, j.Profile, j.VersionCode, j.Defaults, j.Filesystem, j.WorkingDir}
	values = append(values, j.Packages...)
	for k, v := range j.PackageVersions {
		values = append(values, k, v)
	}
	for _, v := range values {
		if strings.ContainsRune(v, 0) {
			return "", fmt.Errorf("输入包含 NUL")
		}
	}
	var b strings.Builder
	b.WriteString("set -eu\nasu_stage=''\nasu_started=$(date +%s)\nstage() {\n  asu_now=$(date +%s)\n  if [ -n \"$asu_stage\" ]; then printf '##asu:timing=%s %s\\n' \"$asu_stage\" \"$((asu_now - asu_started))\"; fi\n  asu_stage=$1; asu_started=$asu_now\n  printf '##asu:stage=%s\\n' \"$asu_stage\"\n}\ntrap 'asu_now=$(date +%s); printf \"##asu:timing=%s %s\\n\" \"$asu_stage\" \"$((asu_now - asu_started))\"' EXIT\nstage init\nstage container_setup\n")
	fmt.Fprintf(&b, "printf 'ASU image=%%s profile=%%s packages=%%s cache_packages=' %s %s %s\nfind dl -type f \\( -name '*.apk' -o -name '*.ipk' \\) 2>/dev/null | wc -l\n", quote(j.Image), quote(j.Profile), quote(strings.Join(j.Packages, " ")))
	// 用 FIFO + tee 同时实时输出和保存校验输入，避免 POSIX 管道吞掉 make 的退出码。
	b.WriteString("capture() {\n  output=$1; shift\n  mkfifo .asu-pipe\n  tee \"$output\" < .asu-pipe &\n  tee_pid=$!\n  status=0\n  \"$@\" > .asu-pipe || status=$?\n  wait \"$tee_pid\" || status=$?\n  rm .asu-pipe\n  return \"$status\"\n}\n")
	if j.Setup {
		b.WriteString("sh setup.sh\n")
	}
	if len(j.Repositories) > 0 {
		b.WriteString("if [ -f repositories ]; then repo=repositories; custom=.asu-repositories-apk; else repo=repositories.conf; custom=.asu-repositories-opkg; fi\n")
		if j.RepositoriesMode == "append" {
			b.WriteString("printf '\\n' >> \"$repo\"\ncat \"$custom\" >> \"$repo\"\n")
		} else {
			b.WriteString("cp \"$custom\" \"$repo\"\n")
		}
		b.WriteString("if [ \"$repo\" = repositories.conf ]; then\n  grep -F 'src imagebuilder file:packages' \"$repo\" >/dev/null || printf '%s\\n' 'src imagebuilder file:packages' >> \"$repo\"\n  grep -F 'option check_signature' \"$repo\" >/dev/null || printf '%s\\n' 'option check_signature' >> \"$repo\"\nfi\n")
	}
	b.WriteString("capture .asu-info make info\nstage validate_revision\n")
	if j.VersionCode != "" {
		fmt.Fprintf(&b, "revision=$(sed -n 's/.*Current Revision: \"\\(.*\\)\".*/\\1/p' .asu-info)\n[ \"$revision\" = %s ] || { echo 'Received incorrect version'; exit 1; }\n", quote(j.VersionCode))
	}
	fmt.Fprintf(&b, "requested=%s\npackages=$requested\n", quote(strings.Join(j.Packages, " ")))
	if j.DiffPackages {
		// 使用实际 ImageBuilder 的默认包，而非可能已更新的上游 profiles 缓存。
		b.WriteString("sed -n 's/^Default Packages: //p' .asu-info > .asu-defaults\n")
		fmt.Fprintf(&b, "awk -v profile=%s '$0 == profile \":\" { found=1; next } found && /^    Packages: / { sub(/^    Packages: /,\"\"); print; exit }' .asu-info >> .asu-defaults\n", quote(j.Profile))
		b.WriteString("defaults=$(tr ' ' '\\n' < .asu-defaults | LC_ALL=C sort -u)\nremove=''\nfor pkg in $defaults; do\n  case \" $requested \" in *\" $pkg \"*) ;; *) remove=\"$remove -${pkg#-}\" ;; esac\ndone\npackages=\"${remove# }${remove:+ }$requested\"\n")
	}
	common := " " + quote("PROFILE="+j.Profile) + " \"PACKAGES=$packages\""
	b.WriteString("stage validate_manifest\n")
	b.WriteString("capture .asu-manifest make manifest" + common + " STRIP_ABI=1\n")
	keys := make([]string, 0, len(j.PackageVersions))
	for k := range j.PackageVersions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "if ! grep -F -x -e %s -e %s .asu-manifest >/dev/null; then printf '%%s\\n' %s; exit 1; fi\n", quote(k+" - "+j.PackageVersions[k]), quote(k+" "+j.PackageVersions[k]), quote("Impossible package selection: "+k+" version not as requested or not in manifest"))
	}
	// 原版使用排序、去重后包名的空格连接字符串（无末尾换行）计算 sha256。
	b.WriteString("names=$(awk '{sub(/^\\+/,\"\",$1); print $1}' .asu-manifest | LC_ALL=C sort -u | tr '\\n' ' '); names=${names% }; hash=$(printf '%s' \"$names\" | sha256sum); hash=${hash%% *}\n")
	b.WriteString("stage building_image\n")
	bin := path.Join(workingDir(j.WorkingDir), j.ID)
	b.WriteString("mkdir -p " + quote(bin) + "\nmake image" + common + " \"EXTRA_IMAGE_NAME=$(printf '%.12s' \"$hash\")\" " + quote("BIN_DIR="+bin))
	if j.Defaults != "" {
		b.WriteString(" " + quote("FILES="+path.Join(workingDir(j.WorkingDir), "asu-files")))
	}
	if j.RootFSSizeMB > 0 {
		fmt.Fprintf(&b, " ROOTFS_PARTSIZE=%d", j.RootFSSizeMB)
	}
	if j.Filesystem != "" {
		b.WriteString(" " + quote("ROOTFS_FILESYSTEM="+j.Filesystem))
	}
	if j.Verbose {
		b.WriteString(" V=s")
	}
	b.WriteByte('\n')
	b.WriteString("cp .asu-manifest " + quote(bin+"/manifest") + "\n")
	b.WriteString("printf '%s\\n' \"$packages\" > " + quote(bin+"/build_cmd_packages") + "\n")
	return b.String(), nil
}
