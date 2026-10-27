package sources

import "testing"

func TestDistributionPathValidation(t *testing.T) {
	for _, distro := range []string{"admin", "api", "json", "store", "healthz", "stats", "", "../x", "foo/bar", "OpenWrt", "bad.name"} {
		t.Run(distro, func(t *testing.T) {
			if err := ValidateDistribution(distro); err == nil {
				t.Fatal("应拒绝无效或保留标识")
			}
			source := NewSource()
			source.Distribution = distro
			if err := source.Validate(); err == nil {
				t.Fatal("source 未执行发行版校验")
			}
		})
	}
	for _, distro := range []string{"openwrt", "immortalwrt", "custom-distro_2"} {
		if err := ValidateDistribution(distro); err != nil {
			t.Fatal(err)
		}
	}
}
