package runner

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

type Mirror struct {
	From, To string
}

// 镜像仅修改 setup 后的原始仓库，自定义仓库随后覆盖或追加，保留请求方的明确地址。
func mirrorScript(mirrors []Mirror) string {
	if len(mirrors) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("if [ -f repositories ]; then repo=repositories; else repo=repositories.conf; fi\n")
	pattern := strings.NewReplacer(`\`, `\\`, `.`, `\.`, `[`, `\[`, `*`, `\*`, `^`, `\^`, `$`, `\$`, `#`, `\#`)
	replacement := strings.NewReplacer(`\`, `\\`, `&`, `\&`, `#`, `\#`)
	for _, mirror := range mirrors {
		// BRE 的分组与行首/空白边界仅由代码生成，URL 本身全部按字面量匹配。
		expr := `s#\(^\|[[:space:]]\)` + pattern.Replace(mirror.From) + `#\1` + replacement.Replace(mirror.To) + `#g`
		fmt.Fprintf(&b, "sed -i %s \"$repo\"\n", quote(expr))
	}
	fmt.Fprintf(&b, "printf '##asu:mirrors=%d\\n'\n", len(mirrors))
	return b.String()
}

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
	for _, mirror := range j.Mirrors {
		values = append(values, mirror.From, mirror.To)
		if strings.ContainsAny(mirror.From+mirror.To, "\r\n") {
			return "", fmt.Errorf("镜像前缀包含换行")
		}
	}
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
	b.WriteString(mirrorScript(j.Mirrors))
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
	// manifest 会静默下载并安装全部包，且其 stdout 用于校验，不能加 V=s；
	// 改由后台心跳按 dl 目录增长情况输出下载进度（写 stderr，不污染 manifest 捕获）。
	b.WriteString("( while sleep 5; do n=$(ls dl 2>/dev/null | wc -l); s=$(du -sk dl 2>/dev/null | cut -f1); printf '[asu] 下载中: dl 共 %s 个文件, %s KiB\\n' \"$n\" \"$s\" >&2; done ) &\nheartbeat=$!\n")
	b.WriteString("capture .asu-manifest make manifest" + common + " STRIP_ABI=1 || { kill \"$heartbeat\" 2>/dev/null; exit 1; }\nkill \"$heartbeat\" 2>/dev/null || true\nwait \"$heartbeat\" 2>/dev/null || true\n")
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
