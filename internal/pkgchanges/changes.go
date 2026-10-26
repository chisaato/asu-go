// Package pkgchanges 纯函数式迁移原版 ASU 的历史包名修正规则。
package pkgchanges

import (
	"slices"
	"strings"
)

// Context 使用已解析的实际 profile。原版没有 distro 限制，所有发行版共享规则。
type Context struct {
	Distro, Version, Target, Profile string
}

type rule struct {
	versionPrefix, version, minimum string
	targets, profiles               []string
	remove, add                     []string
	oldPrefix, newPrefix            string
	translationEnglish              bool
}

func words(s string) []string { return strings.Fields(s) }

// 行号对应 asu/package_changes.py（231 行版本），规则保持原始执行顺序。
var rules = []rule{
	// 原版 L39–43：23.05 的 MT7622 固件。
	{versionPrefix: "23.05", targets: words("mediatek/mt7622"), add: words("kmod-mt7622-firmware")},
	// 原版 L45–58：RTL8366S 交换机驱动。
	{versionPrefix: "23.05", targets: words("ath79/generic"), profiles: words("buffalo_wzr-hp-g300nh-s dlink_dir-825-b1 netgear_wndr3700 netgear_wndr3700-v2 netgear_wndr3800 netgear_wndr3800ch netgear_wndrmac-v1 netgear_wndrmac-v2 trendnet_tew-673gru"), add: words("kmod-switch-rtl8366s")},
	// 原版 L60–61：RTL8366RB 交换机驱动。
	{versionPrefix: "23.05", targets: words("ath79/generic"), profiles: words("buffalo_wzr-hp-g300nh-rb"), add: words("kmod-switch-rtl8366rb")},
	// 原版 L63–66：仅 auc 存在时替换成 owut。
	{versionPrefix: "24.10", remove: words("auc"), add: words("owut")},
	// 原版 L68–69：只限制 profile，不限制 target。
	{versionPrefix: "24.10", profiles: words("tplink_archer-c6-v2"), add: words("ipq-wifi-tplink_archer-c6-v2")},
	// 原版 L71–76：MediaTek FIT 分区支持。
	{versionPrefix: "24.10", targets: words("mediatek/filogic mediatek/mt7622 mediatek/mt7623"), add: words("fitblk")},
	// 原版 L78–89：Kirkwood DSA。
	{versionPrefix: "25.12", targets: words("kirkwood/generic"), profiles: words("checkpoint_l-50 endian_4i-edge-200 linksys_e4200-v2 linksys_ea3500 linksys_ea4500"), add: words("kmod-dsa-mv88e6xxx")},
	// 原版 L90–110：mvebu/cortexa9 DSA。
	{versionPrefix: "25.12", targets: words("mvebu/cortexa9"), profiles: words("cznic_turris-omnia fortinet_fg-30e fortinet_fwf-30e fortinet_fg-50e fortinet_fg-51e fortinet_fg-52e fortinet_fwf-50e-2r fortinet_fwf-51e iij_sa-w2 linksys_wrt1200ac linksys_wrt1900acs linksys_wrt1900ac-v1 linksys_wrt1900ac-v2 linksys_wrt3200acm linksys_wrt32x marvell_a370-rd"), add: words("kmod-dsa-mv88e6xxx")},
	// 原版 L111–122：mvebu/cortexa53 DSA。
	{versionPrefix: "25.12", targets: words("mvebu/cortexa53"), profiles: words("glinet_gl-mv1000 globalscale_espressobin globalscale_espressobin-emmc globalscale_espressobin-ultra globalscale_espressobin-v7 globalscale_espressobin-v7-emmc methode_udpu"), add: words("kmod-dsa-mv88e6xxx")},
	// 原版 L123–132：mvebu/cortexa72 DSA。
	{versionPrefix: "25.12", targets: words("mvebu/cortexa72"), profiles: words("checkpoint_v-80 checkpoint_v-81 globalscale_mochabin mikrotik_rb5009 solidrun_clearfog-pro"), add: words("kmod-dsa-mv88e6xxx")},
	// 原版 L133–136：xrx200 DSA，无 profile 限制。
	{versionPrefix: "25.12", targets: words("lantiq/xrx200"), add: words("kmod-dsa-gswip")},
	// 原版 L137–146：xrx200 PHY22F 拆分固件。
	{versionPrefix: "25.12", targets: words("lantiq/xrx200"), profiles: words("arcadyan_arv7519rw22 arcadyan_vgv7510kw22-brn arcadyan_vgv7510kw22-nor avm_fritz7412 avm_fritz7430 buffalo_wbmr-300hpd"), add: words("xrx200-rev1.1-phy22f-firmware xrx200-rev1.2-phy22f-firmware")},
	// 原版 L147–172：xrx200 PHY11G 拆分固件。
	{versionPrefix: "25.12", targets: words("lantiq/xrx200"), profiles: words("tplink_vr200 tplink_vr200v arcadyan_vgv7519-brn arcadyan_vgv7519-nor arcadyan_vrv9510kwac23 avm_fritz3370-rev2-hynix avm_fritz3370-rev2-micron avm_fritz3390 avm_fritz3490 avm_fritz3490-micron avm_fritz5490 avm_fritz5490-micron avm_fritz7360sl avm_fritz7360-v2 avm_fritz7362sl avm_fritz7490 avm_fritz7490-micron bt_homehub-v5a lantiq_easy80920-nand lantiq_easy80920-nor zyxel_p-2812hnu-f1 zyxel_p-2812hnu-f3"), add: words("xrx200-rev1.1-phy11g-firmware xrx200-rev1.2-phy11g-firmware")},
	// 原版 L173–176：xrx200_legacy DSA。
	{versionPrefix: "25.12", targets: words("lantiq/xrx200_legacy"), add: words("kmod-dsa-gswip")},
	// 原版 L177–182：legacy PHY22F 拆分固件。
	{versionPrefix: "25.12", targets: words("lantiq/xrx200_legacy"), profiles: words("alphanetworks_asl56026 netgear_dm200"), add: words("xrx200-rev1.1-phy22f-firmware xrx200-rev1.2-phy22f-firmware")},
	// 原版 L183–189：legacy PHY11G 拆分固件。
	{versionPrefix: "25.12", targets: words("lantiq/xrx200_legacy"), profiles: words("tplink_tdw8970 tplink_tdw8980 arcadyan_vg3503j"), add: words("xrx200-rev1.1-phy11g-firmware xrx200-rev1.2-phy11g-firmware")},
	// 原版 L190–195：bcm53xx UART 蓝牙驱动。
	{versionPrefix: "25.12", targets: words("bcm53xx/generic"), profiles: words("meraki_mr32"), add: words("kmod-hci-uart")},
	// 原版 L196–201：ipq40xx UART 蓝牙驱动。
	{versionPrefix: "25.12", targets: words("ipq40xx/generic"), profiles: words("linksys_whw03 linksys_whw03v2"), add: words("kmod-hci-uart")},
	// 原版 L202–208：ipq807x UART 蓝牙驱动。
	{versionPrefix: "25.12", targets: words("qualcommax/ipq807x"), profiles: words("linksys_mx4200v1 linksys_mx8500 zyxel_nbg7815"), add: words("kmod-hci-uart")},
	// 原版 L209–211：ath79/mikrotik 旧版以太网驱动。
	{versionPrefix: "25.12", targets: words("ath79/mikrotik"), add: words("kmod-ag71xx-legacy")},
	// 原版 L213–215：只移除精确 SNAPSHOT 中的旧 conntrack6。
	{version: "SNAPSHOT", remove: words("kmod-nf-conntrack6")},
	// 原版 L216–217：只移除精确 SNAPSHOT 中的旧 crc32c。
	{version: "SNAPSHOT", remove: words("kmod-lib-crc32c")},
	// 原版 L8–15、L219–226：故意使用字符串 >=，并非语义版本比较。
	{minimum: "24.10", oldPrefix: "luci-i18n-opkg-", newPrefix: "luci-i18n-package-manager-"},
	// 原版 L228–231：所有版本移除英语翻译包。
	{translationEnglish: true},
}

func (r rule) matches(c Context) bool {
	return (r.versionPrefix == "" || strings.HasPrefix(c.Version, r.versionPrefix)) &&
		(r.version == "" || c.Version == r.version) &&
		(r.minimum == "" || c.Version >= r.minimum) &&
		(len(r.targets) == 0 || slices.Contains(r.targets, c.Target)) &&
		(len(r.profiles) == 0 || slices.Contains(r.profiles, c.Profile))
}

// Apply 返回独立包列表；不规范化、不排序，不更改 packages_versions 的键或版本约束。
// 与原版一样，新增去重，显式移除只删除首次出现项，改名不额外去重。
func Apply(c Context, packages []string) []string {
	packages = slices.Clone(packages)
	for _, r := range rules {
		if !r.matches(c) {
			continue
		}
		removed := false
		for _, p := range r.remove {
			if i := slices.Index(packages, p); i >= 0 {
				packages = slices.Delete(packages, i, i+1)
				removed = true
			}
		}
		if len(r.remove) == 0 || removed {
			for _, p := range r.add {
				if !slices.Contains(packages, p) {
					packages = append(packages, p)
				}
			}
		}
		for i, p := range packages {
			if r.oldPrefix != "" && strings.HasPrefix(p, r.oldPrefix) {
				packages[i] = r.newPrefix + strings.ReplaceAll(p, r.oldPrefix, "")
			}
		}
		if r.translationEnglish {
			packages = slices.DeleteFunc(packages, func(p string) bool {
				return strings.HasPrefix(p, "luci-i18n-") && strings.HasSuffix(p, "-en")
			})
		}
	}
	return packages
}
