package cli

import (
	"fmt"

	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var Version = "dev"

func NewCommand() *cobra.Command {
	root := &cobra.Command{Use: "asu", Short: "ASU-Go 固件升级服务", SilenceUsage: true}
	var path, listen, level, dataDir, server, token string
	root.PersistentFlags().StringVar(&server, "server", "http://127.0.0.1:8000", "管理 API 地址")
	root.PersistentFlags().StringVar(&token, "token", "", "管理 API Bearer token（也读取 ASU_ADMIN_TOKEN 或配置）")
	root.PersistentFlags().StringVar(&path, "config", "", "配置文件（默认查找 ./asu.yaml、/etc/asu/asu.yaml）")
	root.PersistentFlags().StringVar(&level, "log-level", "", "日志级别")
	root.PersistentFlags().StringVar(&listen, "listen", "", "服务监听地址")
	root.PersistentFlags().StringVar(&dataDir, "data-dir", "", "数据目录")
	load := func(cmd *cobra.Command) (config.Config, error) {
		flags := config.Overrides{}
		if cmd.Flags().Changed("listen") {
			flags.Listen = &listen
		}
		if cmd.Flags().Changed("log-level") {
			flags.LogLevel = &level
		}
		if cmd.Flags().Changed("data-dir") {
			flags.DataDir = &dataDir
		}
		return config.Load(path, flags)
	}
	serve := &cobra.Command{Use: "serve", Short: "启动服务", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := load(cmd)
			if err != nil {
				return err
			}
			return runServe(cmd.Context(), cfg)
		},
	}
	configCmd := &cobra.Command{Use: "config", Short: "配置管理"}
	configCmd.AddCommand(&cobra.Command{Use: "check", Short: "校验并打印生效配置", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := load(cmd)
			if err != nil {
				return err
			}
			encoder := yaml.NewEncoder(cmd.OutOrStdout())
			defer func() { _ = encoder.Close() }()
			return encoder.Encode(cfg)
		},
	})
	dbCmd := &cobra.Command{Use: "db", Short: "离线数据库管理（要求服务未运行）"}
	dbCmd.AddCommand(&cobra.Command{Use: "migrate", Short: "执行内嵌迁移（要求服务未运行）", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := load(cmd)
			if err != nil {
				return err
			}
			database, err := db.Open(cmd.Context(), cfg.DataDir)
			if err != nil {
				return err
			}
			defer func() { _ = database.Close() }()
			return database.Migrate(cmd.Context())
		},
	})
	root.AddCommand(newSourceCommand(load, &server, &token), serve, configCmd, dbCmd, &cobra.Command{Use: "version", Short: "打印版本", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), Version)
			return err
		},
	})
	return root
}
