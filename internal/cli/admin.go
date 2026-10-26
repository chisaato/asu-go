package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

func newAdminCommand() *cobra.Command {
	admin := &cobra.Command{Use: "admin", Short: "管理账号工具（无需服务或数据库）"}
	admin.AddCommand(&cobra.Command{Use: "hash-password", Short: "从 stdin 读取一行密码并输出 bcrypt 哈希（终端输入会回显，请优先使用管道）", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			line, err := bufio.NewReader(io.LimitReader(cmd.InOrStdin(), 74)).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("读取密码: %w", err)
			}
			password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if password == "" {
				return errors.New("密码不得为空")
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("生成 bcrypt 哈希: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(hash))
			return err
		},
	})
	return admin
}
