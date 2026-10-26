package buildreq

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func baseRequest() Request {
	r := NewRequest()
	r.Version, r.Target, r.Profile = "24.10.0", "x86/64", "generic"
	return r
}

func hash(t *testing.T, r Request) string {
	t.Helper()
	h, err := r.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 64 || strings.Trim(h, "0123456789abcdef") != "" {
		t.Fatalf("invalid hash %q", h)
	}
	return h
}

func TestEquivalentRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b Request
	}{
		{"packages", func() Request { r := baseRequest(); r.Packages = []string{"luci", "+vim", "vim", "luci"}; return r }(), func() Request { r := baseRequest(); r.Packages = []string{"vim", "luci"}; return r }()},
		{"profile", func() Request { r := baseRequest(); r.Profile = "vendor,device"; return r }(), func() Request { r := baseRequest(); r.Profile = "vendor_device"; return r }()},
		{"map-order", func() Request {
			r := baseRequest()
			r.PackagesVersions = map[string]string{"vim": "1", "luci": "2"}
			return r
		}(), func() Request {
			r := baseRequest()
			r.PackagesVersions = map[string]string{"luci": "2", "vim": "1"}
			return r
		}()},
		{"client-excluded", baseRequest(), func() Request { r := baseRequest(); r.Client = ptr("owut/1"); return r }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if hash(t, tc.a) != hash(t, tc.b) {
				t.Fatal("equivalent requests have different hashes")
			}
		})
	}
}

func TestEveryBuildFieldChangesHash(t *testing.T) {
	changes := map[string]func(*Request){
		"distro":            func(r *Request) { r.Distro = "immortalwrt" },
		"version":           func(r *Request) { r.Version = "24.10.1" },
		"version_code":      func(r *Request) { r.VersionCode = "r123-abc" },
		"target":            func(r *Request) { r.Target = "ath79/generic" },
		"profile":           func(r *Request) { r.Profile = "device" },
		"packages":          func(r *Request) { r.Packages = []string{"luci"} },
		"packages_versions": func(r *Request) { r.PackagesVersions = map[string]string{"luci": "1"} },
		"diff_packages":     func(r *Request) { r.DiffPackages = true },
		"defaults":          func(r *Request) { r.Defaults = ptr("") },
		"rootfs_size_mb":    func(r *Request) { r.RootfsSizeMB = ptr(256) },
		"filesystem":        func(r *Request) { r.Filesystem = ptr("ext4") },
		"repositories":      func(r *Request) { r.Repositories = map[string]string{"custom": "https://repo.test/feed/a"} },
		"repositories_mode": func(r *Request) { r.RepositoriesMode = "append" },
		"repository_keys":   func(r *Request) { r.RepositoryKeys = []string{"key"} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			r := baseRequest()
			change(&r)
			if hash(t, r) == hash(t, baseRequest()) {
				t.Fatal("field not included in hash")
			}
		})
	}
}

func TestCanonicalAndDefaults(t *testing.T) {
	var decoded Request
	if err := json.Unmarshal([]byte(`{"version":"24.10.0","target":"x86/64","profile":"generic"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if hash(t, decoded) != hash(t, baseRequest()) {
		t.Fatal("missing fields did not receive defaults")
	}
	r := baseRequest()
	r.Defaults = ptr("中文<&>\u2028\u2029\\u2028")
	r.Client = ptr("owut")
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"client":"owut","defaults":"中文<&>` + "\u2028\u2029" + `\\u2028","diff_packages":false,"distro":"openwrt","filesystem":null,"packages":[],"packages_versions":{},"profile":"generic","repositories":{},"repositories_mode":"replace","repository_keys":[],"rootfs_size_mb":null,"target":"x86/64","version":"24.10.0","version_code":""}`
	if string(data) != want {
		t.Fatalf("canonical:\n%s\nwant:\n%s", data, want)
	}
	var replay Request
	if err := json.Unmarshal(data, &replay); err != nil || hash(t, replay) != hash(t, r) {
		t.Fatalf("canonical replay failed: %v", err)
	}
}

func TestNullAndEmpty(t *testing.T) {
	for name, change := range map[string]func(*Request){
		"defaults":     func(r *Request) { r.Defaults = ptr("") },
		"filesystem":   func(r *Request) { r.Filesystem = ptr("") },
		"rootfs":       func(r *Request) { r.RootfsSizeMB = ptr(0) },
		"packages":     func(r *Request) { r.Packages = nil },
		"versions":     func(r *Request) { r.PackagesVersions = nil },
		"repositories": func(r *Request) { r.Repositories = nil },
		"keys":         func(r *Request) { r.RepositoryKeys = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := baseRequest()
			change(&r)
			if hash(t, r) == hash(t, baseRequest()) {
				t.Fatal("null and empty were conflated")
			}
		})
	}
	a, _ := baseRequest().Canonical()
	r := baseRequest()
	r.Client = ptr("")
	b, _ := r.Canonical()
	if bytes.Equal(a, b) {
		t.Fatal("client null and empty were conflated in stored request")
	}
}

func TestNormalizeIsPure(t *testing.T) {
	r := baseRequest()
	r.Packages, r.Profile = []string{"z", "+a", "a"}, "vendor,device"
	n := r.Normalize()
	n.Repositories["extra"] = "changed"
	n.PackagesVersions["a"] = "changed"
	if strings.Join(r.Packages, ",") != "z,+a,a" || r.Profile != "vendor,device" || len(r.Repositories) != 0 || len(r.PackagesVersions) != 0 {
		t.Fatal("Normalize modified input")
	}
	if hash(t, n) != hash(t, n.Normalize()) {
		t.Fatal("Normalize not idempotent")
	}
}

func TestGoldenHashes(t *testing.T) {
	// 期望值由独立 Python json.dumps(sort_keys=True, separators=(',', ':'),
	// ensure_ascii=False) + hashlib.sha256 生成，不使用原版 repr 拼接。
	for _, tc := range []struct{ input, want string }{
		{`{"version":"24.10.0","target":"x86/64","profile":"generic"}`, "22c0e1a11e180d996f7fb7657ca18654324420bebd1bca271719155845a73ba8"},
		{`{"version":"SNAPSHOT","target":"mediatek/filogic","profile":"vendor,device","packages":["+luci","vim","luci"],"defaults":"echo 中文<&>","filesystem":"squashfs","rootfs_size_mb":256,"repositories":{"z":"https://example.org/feed/z","a":"https://example.org/feed/a"},"repository_keys":["key2","key1"],"packages_versions":{"vim":"1.2~rc1","luci":"git-2"},"diff_packages":true,"version_code":"r123-abc","repositories_mode":"append"}`, "b98b7505a1fc822c17a44c1f938dc345b3477eba2b94fc3b6fd630e1a966a2ee"},
		{`{"version":"24.10.0","target":"x86/64","profile":"generic","defaults":""}`, "b2109adfa600c984c7004f1cce7b864d4db9fc4309bc26943970f505bd16d9d6"},
	} {
		var r Request
		if err := json.Unmarshal([]byte(tc.input), &r); err != nil {
			t.Fatal(err)
		}
		if got := hash(t, r); got != tc.want {
			t.Errorf("hash = %s, want %s", got, tc.want)
		}
	}
}

func TestValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Request)
		limits Limits
		status int
	}{
		{"valid", func(*Request) {}, Limits{}, 0},
		{"defaults-disabled", func(r *Request) { r.Defaults = ptr("echo x") }, Limits{}, 400},
		{"defaults-empty", func(r *Request) { r.Defaults = ptr("") }, Limits{}, 0},
		{"defaults-enabled", func(r *Request) { r.Defaults = ptr("echo x") }, Limits{AllowDefaults: true}, 0},
		{"defaults-too-long", func(r *Request) { r.Defaults = ptr(strings.Repeat("a", 20481)) }, Limits{AllowDefaults: true}, 422},
		{"defaults-boundary", func(r *Request) { r.Defaults = ptr(strings.Repeat("中", 20480)) }, Limits{AllowDefaults: true}, 0},
		{"defaults-custom-limit", func(r *Request) { r.Defaults = ptr("xx") }, Limits{AllowDefaults: true, MaxDefaultsLength: 1}, 422},
		{"rootfs-zero", func(r *Request) { r.RootfsSizeMB = ptr(0) }, Limits{}, 422},
		{"rootfs-negative", func(r *Request) { r.RootfsSizeMB = ptr(-1) }, Limits{}, 422},
		{"rootfs-min", func(r *Request) { r.RootfsSizeMB = ptr(1) }, Limits{}, 0},
		{"rootfs-max", func(r *Request) { r.RootfsSizeMB = ptr(1024) }, Limits{}, 0},
		{"rootfs-too-large", func(r *Request) { r.RootfsSizeMB = ptr(1025) }, Limits{}, 422},
		{"rootfs-custom-limit", func(r *Request) { r.RootfsSizeMB = ptr(256) }, Limits{MaxCustomRootfsSizeMB: 128}, 422},
		{"filesystem-empty", func(r *Request) { r.Filesystem = ptr("") }, Limits{}, 422},
		{"filesystem-unknown", func(r *Request) { r.Filesystem = ptr("btrfs") }, Limits{}, 422},
		{"mode", func(r *Request) { r.RepositoriesMode = "bad" }, Limits{}, 422},
		{"distro", func(r *Request) { r.Distro = "bad/name" }, Limits{}, 422},
		{"version", func(r *Request) { r.Version = "a b" }, Limits{}, 422},
		{"revision", func(r *Request) { r.VersionCode = "x;id" }, Limits{}, 422},
		{"target", func(r *Request) { r.Target = "x/y/z" }, Limits{}, 422},
		{"profile", func(r *Request) { r.Profile = "x\n" }, Limits{}, 422},
		{"package", func(r *Request) { r.Packages = []string{"x;id"} }, Limits{}, 422},
		{"package-plus", func(r *Request) { r.Packages = []string{"+luci"} }, Limits{}, 0},
		{"package-double-plus", func(r *Request) { r.Packages = []string{"++luci"} }, Limits{}, 422},
		{"package-version-key", func(r *Request) { r.PackagesVersions = map[string]string{"a/b": "1"} }, Limits{}, 422},
		{"package-version-value", func(r *Request) { r.PackagesVersions = map[string]string{"a": "1:2"} }, Limits{}, 422},
		{"package-version-valid", func(r *Request) { r.PackagesVersions = map[string]string{"a": "1.2+3,4~rc-1"} }, Limits{}, 0},
		{"repo-name", func(r *Request) { r.Repositories["bad/name"] = "https://repo.test/feed/a" }, Limits{}, 422},
		{"repo-url", func(r *Request) { r.Repositories["a"] = "file:///etc/passwd" }, Limits{}, 422},
		{"repo-space", func(r *Request) { r.Repositories["a"] = "https://repo.test/a b" }, Limits{}, 422},
		{"repo-denied", func(r *Request) { r.Repositories["a"] = "https://repo.test/feed/a" }, Limits{}, 400},
		{"repo-allowed", func(r *Request) { r.Repositories["a"] = "https://repo.test/feed/a" }, Limits{RepositoryAllowList: []string{"https://repo.test/feed"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := baseRequest()
			tc.change(&r)
			assertStatus(t, Validate(r, tc.limits), tc.status)
		})
	}
	for _, fs := range []string{"squashfs", "ext4", "ubifs", "erofs", "jffs2", "jffs2-nand", "targz", "cpiogz"} {
		r := baseRequest()
		r.Filesystem = &fs
		assertStatus(t, Validate(r, Limits{}), 0)
	}
}

func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	if want == 0 {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Status != want {
		t.Fatalf("error = %v, want status %d", err, want)
	}
}

func TestJSONModelErrors(t *testing.T) {
	for _, input := range []string{
		`null`, `{}`, `{"version":"1","target":"x/y"}`,
		`{"version":null,"target":"x/y","profile":"a"}`,
		`{"version":1,"target":"x/y","profile":"a"}`,
		`{"version":"1","target":"x/y","profile":"a","packages":null}`,
		`{"version":"1","target":"x/y","profile":"a","packages":[null]}`,
		`{"version":"1","target":"x/y","profile":"a","packages_versions":{"a":null}}`,
		`{"version":"1","target":"x/y","profile":"a","diff_packages":null}`,
		`{"version":"1","target":"x/y","profile":"a","rootfs_size_mb":1.5}`,
	} {
		t.Run(input, func(t *testing.T) {
			var r Request
			assertStatus(t, json.Unmarshal([]byte(input), &r), 422)
		})
	}
}

func TestRepositoryAllowList(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://repo.test/feed/a", true},
		{"https://REPO.test/feed/a", true},
		{"https://repo.test:8443/feed/a", true},
		{"https://repo.test/feed/a?x=1#y", true},
		// 与原版一致：端口、userinfo 不参与匹配，路径不做 dot-segment 清理。
		{"https://user:pass@repo.test/feed/a", true},
		{"https://repo.test/feed/../outside", true},
		{"https://repo.test/feed", false},
		{"https://repo.test/feeds/a", false},
		{"http://repo.test/feed/a", false},
		{"https://sub.repo.test/feed/a", false},
		{"https://repo.test.evil/feed/a", false},
		{"https://repo.test@evil.test/feed/a", false},
		{"https://repo.test%40evil.test/feed/a", false},
		{"https://repo.test/feed%2Fa", false},
		{"https://repo.test/other/../feed/a", false},
		{"https://repo.test/feed\\a", false},
		{"not a url", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			if got := RepositoryAllowed(tc.url, []string{"https://repo.test/feed/"}); got != tc.want {
				t.Fatalf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
	if RepositoryAllowed("https://repo.test/feed/a", nil) || RepositoryAllowed("https://repo.test/feed/a", []string{"%"}) {
		t.Fatal("empty or invalid allow list allowed repository")
	}
	if RepositoryAllowed("https://repo.test/%E4%B8%AD/a", []string{"https://repo.test/中"}) {
		t.Fatal("raw and percent-encoded URL paths must remain distinct")
	}
}

func TestMetadataHook(t *testing.T) {
	r := baseRequest()
	r.Profile = "vendor,device"
	called := false
	sentinel := errors.New("metadata rejected")
	err := Validate(r, Limits{}, nil, func(n Request) error {
		called = true
		if n.Profile != "vendor_device" {
			t.Fatal("callback received unnormalized request")
		}
		return sentinel
	})
	if !called || !errors.Is(err, sentinel) {
		t.Fatal("metadata callback error lost")
	}
}
