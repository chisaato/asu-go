package queue

import (
	"github.com/chisaato/asu-go/internal/buildreq"
	"github.com/chisaato/asu-go/internal/pkgchanges"
)

// executionPackages 在哈希和持久化原请求后、Runner 的差异包计算前应用规则。
// 原版 routers/api.py L115–118 优先使用 packages_versions 的键；
// build.py L272 在 diff_packages（L287–290）之前修正，不改版本约束。
func executionPackages(r buildreq.Request, profile string) []string {
	return pkgchanges.Apply(pkgchanges.Context{
		Distro: r.Distro, Version: r.Version, Target: r.Target, Profile: profile,
	}, selectedPackages(r, nil))
}
