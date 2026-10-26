package pkgchanges

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"testing"
)

func TestRules(t *testing.T) {
	for _, tc := range []struct {
		name, version, target, profile, input, want string
	}{
		{"mt7622", "23.05.5", "mediatek/mt7622", "any", "", "kmod-mt7622-firmware"},
		{"rtl8366s", "23.05-SNAPSHOT", "ath79/generic", "netgear_wndr3700", "", "kmod-switch-rtl8366s"},
		{"rtl8366rb", "23.05", "ath79/generic", "buffalo_wzr-hp-g300nh-rb", "", "kmod-switch-rtl8366rb"},
		{"auc", "24.10.0", "x86/64", "any", "auc", "owut"},
		{"archer", "24.10.0", "x86/64", "tplink_archer-c6-v2", "", "ipq-wifi-tplink_archer-c6-v2"},
		{"fitblk", "24.10-SNAPSHOT", "mediatek/filogic", "any", "", "fitblk"},
		{"kirkwood", "25.12.0", "kirkwood/generic", "checkpoint_l-50", "", "kmod-dsa-mv88e6xxx"},
		{"cortexa9", "25.12.0", "mvebu/cortexa9", "cznic_turris-omnia", "", "kmod-dsa-mv88e6xxx"},
		{"cortexa53", "25.12.0", "mvebu/cortexa53", "glinet_gl-mv1000", "", "kmod-dsa-mv88e6xxx"},
		{"cortexa72", "25.12.0", "mvebu/cortexa72", "mikrotik_rb5009", "", "kmod-dsa-mv88e6xxx"},
		{"xrx200", "25.12.0", "lantiq/xrx200", "unknown", "", "kmod-dsa-gswip"},
		{"phy22f", "25.12.0", "lantiq/xrx200", "avm_fritz7412", "", "kmod-dsa-gswip xrx200-rev1.1-phy22f-firmware xrx200-rev1.2-phy22f-firmware"},
		{"phy11g", "25.12.0", "lantiq/xrx200", "bt_homehub-v5a", "", "kmod-dsa-gswip xrx200-rev1.1-phy11g-firmware xrx200-rev1.2-phy11g-firmware"},
		{"legacy", "25.12.0", "lantiq/xrx200_legacy", "unknown", "", "kmod-dsa-gswip"},
		{"legacy22f", "25.12.0", "lantiq/xrx200_legacy", "netgear_dm200", "", "kmod-dsa-gswip xrx200-rev1.1-phy22f-firmware xrx200-rev1.2-phy22f-firmware"},
		{"legacy11g", "25.12.0", "lantiq/xrx200_legacy", "tplink_tdw8970", "", "kmod-dsa-gswip xrx200-rev1.1-phy11g-firmware xrx200-rev1.2-phy11g-firmware"},
		{"bcm53xx", "25.12.0", "bcm53xx/generic", "meraki_mr32", "", "kmod-hci-uart"},
		{"ipq40xx", "25.12.0", "ipq40xx/generic", "linksys_whw03", "", "kmod-hci-uart"},
		{"ipq807x", "25.12.0", "qualcommax/ipq807x", "zyxel_nbg7815", "", "kmod-hci-uart"},
		{"mikrotik", "25.12.0-rc1", "ath79/mikrotik", "any", "", "kmod-ag71xx-legacy"},
		{"conntrack6", "SNAPSHOT", "x86/64", "any", "kmod-nf-conntrack6", ""},
		{"crc32c", "SNAPSHOT", "x86/64", "any", "kmod-lib-crc32c", ""},
		{"language", "24.10", "x86/64", "any", "luci-i18n-opkg-zh-cn", "luci-i18n-package-manager-zh-cn"},
		{"english", "22.03.0", "x86/64", "any", "luci-i18n-base-en other-en -luci-i18n-base-en", "other-en -luci-i18n-base-en"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, distro := range []string{"openwrt", "immortalwrt", "custom"} {
				c := Context{Distro: distro, Version: tc.version, Target: tc.target, Profile: tc.profile}
				input := words(tc.input)
				before := slices.Clone(input)
				got := Apply(c, input)
				if !slices.Equal(got, words(tc.want)) {
					t.Fatalf("%s: got %v, want %s", distro, got, tc.want)
				}
				if !slices.Equal(input, before) {
					t.Fatal("修改了输入")
				}
			}
		})
	}
}

// 每条受限规则都有条件边界；全量期望另由独立 Python 黄金数据检验。
func TestRuleBoundaries(t *testing.T) {
	for i, r := range rules {
		t.Run(fmt.Sprint(i+1), func(t *testing.T) {
			c := Context{Version: "24.10.0", Target: "unknown/target", Profile: "unknown"}
			switch {
			case r.versionPrefix != "":
				c.Version = r.versionPrefix + ".0"
			case r.version != "":
				c.Version = r.version
			case r.minimum != "":
				c.Version = r.minimum
			}
			if len(r.targets) > 0 {
				c.Target = r.targets[0]
			}
			if len(r.profiles) > 0 {
				c.Profile = r.profiles[0]
			}
			if !r.matches(c) {
				t.Fatal("正例不匹配")
			}
			for _, dimension := range []string{"version", "target", "profile"} {
				other := c
				switch dimension {
				case "version":
					if r.versionPrefix == "" && r.version == "" && r.minimum == "" {
						continue
					}
					other.Version = "22.03.9"
				case "target":
					if len(r.targets) == 0 {
						continue
					}
					other.Target += "-wrong"
				case "profile":
					if len(r.profiles) == 0 {
						continue
					}
					other.Profile += "-wrong"
				}
				if r.matches(other) {
					t.Errorf("错误匹配 %s 边界: %+v", dimension, other)
				}
			}
		})
	}
}

func TestPythonGolden(t *testing.T) {
	file, err := os.Open("testdata/python-golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	for i := 0; ; i++ {
		var tc struct {
			Context        Context
			Packages, Want []string
		}
		if err := decoder.Decode(&tc); errors.Is(err, io.EOF) {
			if i != 1681 {
				t.Fatalf("黄金样例数 %d，期望 1681", i)
			}
			break
		} else if err != nil {
			t.Fatal(err)
		}
		before := slices.Clone(tc.Packages)
		got := Apply(tc.Context, tc.Packages)
		if !slices.Equal(got, tc.Want) {
			t.Errorf("case %d %+v: got %v, Python want %v", i, tc.Context, got, tc.Want)
		}
		if !slices.Equal(before, tc.Packages) {
			t.Fatalf("case %d: 修改了输入", i)
		}
	}
}
