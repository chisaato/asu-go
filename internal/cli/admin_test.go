package cli

import (
	"bytes"
	"strings"
	"testing"

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
