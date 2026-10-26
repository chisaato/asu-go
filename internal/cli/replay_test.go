package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplayCLIHTTP(t *testing.T) {
	var path string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		path = r.URL.Path
		if r.Method != "POST" {
			t.Error(r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"id":"rp_test"}`))
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "asu.yaml")
	if err := os.WriteFile(configPath, []byte("admin:\n  api_token: secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(input string, args ...string) (string, error) {
		cmd := NewCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetIn(strings.NewReader(input))
		cmd.SetArgs(append([]string{"--server", server.URL, "--config", configPath, "replay"}, args...))
		err := cmd.Execute()
		return output.String(), err
	}
	output, err := run("", "abc", "--verbose=false", "--no-keep-container", "--image-tag", "debug", "--pull-policy", "always")
	if err != nil || output != "rp_test\n" || path != "/admin/api/jobs/abc/replay" {
		t.Fatal(output, path, err)
	}
	opts := body["options"].(map[string]any)
	if opts["verbose"] != false || opts["keep_container"] != false || opts["image_tag"] != "debug" || opts["pull_policy"] != "always" {
		t.Fatal(opts)
	}
	for _, file := range []string{"-", filepath.Join(t.TempDir(), "request.json")} {
		request := `{"version":"24.10.0","target":"x86/64","profile":"generic"}`
		if file != "-" {
			if err := os.WriteFile(file, []byte(request), 0600); err != nil {
				t.Fatal(err)
			}
		}
		output, err := run(request, "-f", file)
		if err != nil || output != "rp_test\n" || path != "/admin/api/replay" || body["version"] != "24.10.0" {
			t.Fatal(output, path, body, err)
		}
	}
	t.Setenv("ASU_ADMIN_TOKEN", "wrong")
	if _, err := run("", "abc"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
	if _, err := run("", "abc", "--token", "secret"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"abc", "-f", "-"}, {"-f", "-", "--token", "secret"}} {
		if _, err := run("null", args...); err == nil {
			t.Fatal(args)
		}
	}
}
