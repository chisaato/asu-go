package buildreq

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits 由调用层配置注入；零值长度限制使用原版默认值。
type Limits struct {
	AllowDefaults         bool     `json:"allow_defaults"`
	MaxDefaultsLength     int      `json:"max_defaults_length"`
	MaxCustomRootfsSizeMB int      `json:"max_custom_rootfs_size_mb"`
	RepositoryAllowList   []string `json:"repository_allow_list"`
}

// ValidationError 区分原版模型校验（422）与请求策略校验（400）。
type ValidationError struct {
	Status int
	Field  string
	Detail string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Detail) }

func invalid(field, detail string) error {
	return &ValidationError{Status: 422, Field: field, Detail: detail}
}

func denied(field, detail string) error {
	return &ValidationError{Status: 400, Field: field, Detail: detail}
}

// MetadataValidator 预留发行版、版本、target、profile 合法性校验点。
// 回调收到规范化副本；元数据查询与 profile 别名解析由后续 service 负责。
type MetadataValidator func(Request) error

var (
	stringPattern   = regexp.MustCompile(`^[\p{L}\p{N}_.,-]*$`)
	targetPattern   = regexp.MustCompile(`^[\p{L}\p{N}_]*/[\p{L}\p{N}_]*$`)
	versionPattern  = regexp.MustCompile(`^[\p{L}\p{N}_+.,~-]*$`)
	repoNamePattern = regexp.MustCompile(`^[\p{L}\p{N}_.-]+$`)
)

// Validate 先执行模型校验（422），再执行服务端策略（400），最后调用元数据校验。
func Validate(r Request, limits Limits, metadata ...MetadataValidator) error {
	return validate(r, limits, false, metadata...)
}

// ValidateReplay 仅供已鉴权管理端显式绕过策略限制，仍验证字段与元数据。
func ValidateReplay(r Request, limits Limits, skipLimits bool) error {
	return validate(r, limits, skipLimits)
}

func validate(r Request, limits Limits, skipLimits bool, metadata ...MetadataValidator) error {
	if limits.MaxDefaultsLength == 0 {
		limits.MaxDefaultsLength = 20480
	}
	if limits.MaxCustomRootfsSizeMB == 0 {
		limits.MaxCustomRootfsSizeMB = 1024
	}
	for _, field := range []struct{ name, value string }{
		{"distro", r.Distro}, {"version", r.Version}, {"version_code", r.VersionCode}, {"profile", r.Profile},
	} {
		if !utf8.ValidString(field.value) || !stringPattern.MatchString(field.value) {
			return invalid(field.name, "String should match pattern ^[\\w.,-]*$")
		}
	}
	if !targetPattern.MatchString(r.Target) {
		return invalid("target", "String should match pattern ^[\\w]*/[\\w]*$")
	}
	if r.Packages == nil || r.PackagesVersions == nil || r.Repositories == nil || r.RepositoryKeys == nil {
		return invalid("request", "Collections cannot be null; use NewRequest for defaults")
	}
	for _, pkg := range r.Packages {
		// 原版哈希允许 + 前缀，但模型正则遗漏了它；按协议要求仅放行一个前缀。
		if !stringPattern.MatchString(strings.TrimPrefix(pkg, "+")) {
			return invalid("packages", "Invalid package name")
		}
	}
	for _, pkg := range sortedKeys(r.PackagesVersions) {
		if !stringPattern.MatchString(pkg) || !versionPattern.MatchString(r.PackagesVersions[pkg]) {
			return invalid("packages_versions", "Invalid package name or version")
		}
	}
	if !skipLimits && r.Defaults != nil && utf8.RuneCountInString(*r.Defaults) > limits.MaxDefaultsLength {
		return invalid("defaults", "String exceeds max_defaults_length")
	}
	if r.RootfsSizeMB != nil && (*r.RootfsSizeMB < 1 || (!skipLimits && *r.RootfsSizeMB > limits.MaxCustomRootfsSizeMB)) {
		return invalid("rootfs_size_mb", "Size outside allowed range")
	}
	if r.Filesystem != nil && !slices.Contains([]string{"squashfs", "ext4", "ubifs", "erofs", "jffs2", "jffs2-nand", "targz", "cpiogz"}, *r.Filesystem) {
		return invalid("filesystem", "Unsupported filesystem")
	}
	if r.RepositoriesMode != "append" && r.RepositoriesMode != "replace" {
		return invalid("repositories_mode", "Expected append or replace")
	}
	for _, name := range sortedKeys(r.Repositories) {
		repo := r.Repositories[name]
		if !repoNamePattern.MatchString(name) || !validRepoURL(repo) {
			return invalid("repositories", "Invalid repository name or URL")
		}
	}
	if !skipLimits && r.Defaults != nil && *r.Defaults != "" && !limits.AllowDefaults {
		return denied("defaults", "Handling `defaults` not enabled on server")
	}
	for _, name := range sortedKeys(r.Repositories) {
		if !skipLimits && !RepositoryAllowed(r.Repositories[name], limits.RepositoryAllowList) {
			return denied("repositories", "Repository not allowed: "+r.Repositories[name])
		}
	}
	for _, check := range metadata {
		if check != nil {
			if err := check(r.Normalize()); err != nil {
				return err
			}
		}
	}
	return nil
}

func validRepoURL(repo string) bool {
	return (strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://")) &&
		len(repo) > len(strings.SplitN(repo, "://", 2)[0])+3 &&
		!strings.ContainsFunc(repo, unicode.IsSpace)
}

// RepositoryAllowed 对照原版 repositories.py：scheme、hostname 精确比较，
// 原始 URL path 按目录前缀比较；不解码 %xx，不清理 ..，不比较端口。
// path 前缀必须含末尾 /，因此 /feed 不匹配 /feeds 或 /feed 本身。
func RepositoryAllowed(repo string, allowList []string) bool {
	parsed, err := url.Parse(repo)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	for _, allowed := range allowList {
		base, err := url.Parse(allowed)
		if err != nil || base.Hostname() == "" {
			continue
		}
		if parsed.Scheme == base.Scheme && strings.EqualFold(parsed.Hostname(), base.Hostname()) &&
			strings.HasPrefix(rawPath(repo), strings.TrimRight(rawPath(allowed), "/")+"/") {
			return true
		}
	}
	return false
}

func rawPath(raw string) string {
	// URL.RawPath 可能省略合法的百分号编码，不能用于原始路径比较。
	_, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return ""
	}
	i := strings.IndexAny(rest, "/?#")
	if i < 0 || rest[i] != '/' {
		return ""
	}
	path := rest[i:]
	if end := strings.IndexAny(path, "?#"); end >= 0 {
		path = path[:end]
	}
	return path
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
