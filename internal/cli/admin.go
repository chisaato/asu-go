package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chisaato/asu-go/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

func newAdminCommand(configPath *string) *cobra.Command {
	admin := &cobra.Command{Use: "admin", Short: "管理账号与凭证工具"}

	// 1. 生成密码哈希
	admin.AddCommand(&cobra.Command{
		Use:   "hash-password",
		Short: "从 stdin 读取一行密码并输出 bcrypt 哈希（终端输入会回显，请优先使用管道）",
		Args:  cobra.NoArgs,
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

	// 2. 直接从 CLI 重置或设置管理员密码
	admin.AddCommand(newResetPasswordCommand(configPath))

	return admin
}

func newResetPasswordCommand(configPath *string) *cobra.Command {
	var passwordFlag string
	cmd := &cobra.Command{
		Use:     "reset-password [username]",
		Aliases: []string{"set-password", "set-user"},
		Short:   "直接重置或添加管理账号密码，并自动安全保存到配置文件",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := "admin"
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				username = strings.TrimSpace(args[0])
			}

			pwd := passwordFlag
			if pwd == "" {
				cmd.Print("请输入新密码: ")
				reader := bufio.NewReader(io.LimitReader(cmd.InOrStdin(), 74))
				line, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("读取密码: %w", err)
				}
				pwd = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			}
			if pwd == "" {
				return errors.New("密码不得为空")
			}

			hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("生成哈希失败: %w", err)
			}

			var path string
			if configPath != nil {
				path = *configPath
			}
			targetFile, err := config.FindConfigFile(path)
			if err != nil {
				return err
			}

			if err := updateUserPasswordInFile(targetFile, username, string(hash)); err != nil {
				return fmt.Errorf("更新配置文件失败: %w", err)
			}

			cmd.Printf("已成功为管理员 [%s] 设置密码，已安全保存至配置文件 %s\n重启服务后即可登录管理后台 (/ui/)。\n", username, targetFile)
			return nil
		},
	}
	cmd.Flags().StringVar(&passwordFlag, "password", "", "直接指定新密码（留空则从标准输入交互式提示输入）")
	return cmd
}

func updateUserPasswordInFile(filePath, username, hash string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}

	if len(root.Content) == 0 {
		return errors.New("配置文件内容为空")
	}
	topMap := root.Content[0]
	if topMap.Kind != yaml.MappingNode {
		return errors.New("配置文件格式错误，顶层应为 mapping")
	}

	// 查找 admin 节点
	var adminMap *yaml.Node
	for i := 0; i < len(topMap.Content); i += 2 {
		if topMap.Content[i].Value == "admin" {
			adminMap = topMap.Content[i+1]
			break
		}
	}
	if adminMap == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "admin"}
		adminMap = &yaml.Node{Kind: yaml.MappingNode}
		topMap.Content = append(topMap.Content, keyNode, adminMap)
	}

	// 查找 users 节点
	var usersSeq *yaml.Node
	usersKeyIdx := -1
	for i := 0; i < len(adminMap.Content); i += 2 {
		if adminMap.Content[i].Value == "users" {
			usersKeyIdx = i
			usersSeq = adminMap.Content[i+1]
			break
		}
	}

	if usersSeq == nil || usersSeq.Kind != yaml.SequenceNode {
		usersSeq = &yaml.Node{Kind: yaml.SequenceNode}
		if usersKeyIdx >= 0 {
			adminMap.Content[usersKeyIdx+1] = usersSeq
		} else {
			adminMap.Content = append(adminMap.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "users"},
				usersSeq,
			)
		}
	}

	// 查找是否已存在该用户
	found := false
	for _, item := range usersSeq.Content {
		if item.Kind == yaml.MappingNode {
			var nameNode, hashNode *yaml.Node
			for j := 0; j < len(item.Content); j += 2 {
				switch item.Content[j].Value {
				case "name":
					nameNode = item.Content[j+1]
				case "password_hash":
					hashNode = item.Content[j+1]
				}
			}
			if nameNode != nil && nameNode.Value == username {
				found = true
				if hashNode != nil {
					hashNode.Value = hash
				} else {
					item.Content = append(item.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: "password_hash"},
						&yaml.Node{Kind: yaml.ScalarNode, Value: hash},
					)
				}
				break
			}
		}
	}

	if !found {
		newUser := &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "name"},
				{Kind: yaml.ScalarNode, Value: username},
				{Kind: yaml.ScalarNode, Value: "password_hash"},
				{Kind: yaml.ScalarNode, Value: hash},
			},
		}
		usersSeq.Content = append(usersSeq.Content, newUser)
	}

	tmpFile := filePath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	err = enc.Encode(&root)
	_ = enc.Close()
	_ = f.Close()
	if err != nil {
		_ = os.Remove(tmpFile)
		return err
	}
	return os.Rename(tmpFile, filePath)
}
