package config

import (
	"strings"
	"testing"
)

func TestMirrors(t *testing.T) {
	cfg, err := Decode(strings.NewReader("runner:\n  mirrors:\n    - from: https://downloads.openwrt.org/\n      to: https://mirrors.ustc.edu.cn/openwrt/\n"))
	if err != nil || len(cfg.Runner.Mirrors) != 1 {
		t.Fatal(cfg.Runner.Mirrors, err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"from", "to"} {
		for _, value := range []string{"ftp://example.org/", "file:///tmp/", "https://example.org", "https:///", "https://example.org/\n", ""} {
			c := Default()
			m := Mirror{From: "http://example.org/", To: "https://example.org/"}
			if key == "from" {
				m.From = value
			} else {
				m.To = value
			}
			c.Runner.Mirrors = []Mirror{m}
			if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "runner.mirrors[0]."+key) {
				t.Fatalf("未拒绝 %s=%q: %v", key, value, err)
			}
		}
	}
	for _, input := range []string{"runner:\n  mirrors:\n    - form: https://example.org/", "runner:\n  mirrors: wrong", "runner:\n  mirrors:\n    - from: []"} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Fatalf("未严格拒绝 %q", input)
		}
	}
	if len(Default().Runner.Mirrors) != 0 {
		t.Fatal("默认必须为空")
	}
}
