package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chisaato/asu-go/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	for _, password := range []string{"secret", "", strings.Repeat("x", 73)} {
		cmd := NewCommand()
		cmd.SetArgs([]string{"admin", "hash-password"})
		cmd.SetIn(strings.NewReader(password + "\n"))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		err := cmd.Execute()
		if password == "" || len(password) > 72 {
			if err == nil {
				t.Fatal("非法密码被接受")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(strings.TrimSpace(out.String())), []byte(password)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResetPassword(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "asu.yaml")
	content := `server:
  listen: ":8000"
admin:
  users: null
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewCommand()
	cmd.SetArgs([]string{"--config", cfgPath, "admin", "reset-password", "alice", "--password", "newsecret123"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("执行 reset-password 失败: %v", err)
	}

	// 重新加载配置并校验
	loaded, err := config.Load(cfgPath, config.Overrides{})
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	var foundUser *config.AdminUser
	for _, u := range loaded.Admin.Users {
		if u.Name == "alice" {
			foundUser = &u
			break
		}
	}
	if foundUser == nil {
		t.Fatal("未在配置中找到添加的用户 alice")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(foundUser.PasswordHash), []byte("newsecret123")); err != nil {
		t.Fatalf("密码哈希校验失败: %v", err)
	}
}
