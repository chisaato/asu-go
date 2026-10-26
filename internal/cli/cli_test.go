package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"version"}, Version},
		{[]string{"config", "check", "--config", "../../configs/asu.example.yaml", "--listen", ":8400", "--log-level", "debug", "--data-dir", "cli_data"}, "listen: :8400"},
	} {
		t.Run(strings.Join(tc.args[:1], " "), func(t *testing.T) {
			cmd := NewCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("命令输出不符: %s", out.String())
			}
		})
	}
}
