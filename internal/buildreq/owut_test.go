package buildreq

import (
	"encoding/json"
	"os"
	"testing"
)

func TestOWUTCapturedRequest(t *testing.T) {
	body, err := os.ReadFile("../api/testdata/owut/build-request-immortalwrt-25.12.2.json")
	if err != nil {
		t.Fatal(err)
	}
	var r Request
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	if r.DistroSpecified() {
		t.Fatal("真实 owut 请求不应显式携带 distro")
	}
	// 与 HTTP handler 一致，从 /immortalwrt 前缀补齐发行版，不修改抓包正文。
	r.Distro = "immortalwrt"
	if err := Validate(r, Limits{}); err != nil {
		t.Fatal(err)
	}
	// 当前规范 JSON 算法；独立 Python sort_keys + SHA-256 复核，与抓包恰好一致。
	const want = "ea6405314694f787c99984b33419d928da46066a2f761345a39c1a215c2b395e"
	if got := hash(t, r); got != want {
		t.Fatalf("hash=%s，期望 %s", got, want)
	}
	canonical, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	var replay Request
	if err := json.Unmarshal(canonical, &replay); err != nil {
		t.Fatal(err)
	}
	if got := hash(t, replay); got != want {
		t.Fatalf("规范化重放 hash=%s", got)
	}
}
