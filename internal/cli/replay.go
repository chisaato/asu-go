package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/chisaato/asu-go/internal/config"
	"github.com/spf13/cobra"
)

func newReplayCommand(load func(*cobra.Command) (config.Config, error), server, token *string) *cobra.Command {
	var file, imageTag, pullPolicy string
	var verbose, noKeep bool
	cmd := &cobra.Command{Use: "replay [hash]", Short: "经管理 API 发起独立诊断重放", Args: cobra.MaximumNArgs(1)}
	cmd.Flags().StringVarP(&file, "file", "f", "", "原始 BuildRequest JSON 文件（- 为 stdin）")
	cmd.Flags().BoolVar(&verbose, "verbose", true, "显示完整构建日志")
	cmd.Flags().BoolVar(&noKeep, "no-keep-container", false, "失败后不保留容器")
	cmd.Flags().StringVar(&imageTag, "image-tag", "", "覆盖镜像 tag")
	cmd.Flags().StringVar(&pullPolicy, "pull-policy", "", "镜像拉取策略 missing|always|never")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if (file == "" && len(args) != 1) || (file != "" && len(args) != 0) {
			return fmt.Errorf("必须指定一个 hash 或 -f 文件")
		}
		cfg, err := load(cmd)
		if err != nil {
			return err
		}
		credential := cfg.Admin.APIToken
		if cmd.Flags().Changed("token") {
			credential = *token
		}
		client := sourceClient{server: *server, token: credential}
		body := map[string]any{}
		path := "/replay"
		if file != "" {
			reader := cmd.InOrStdin()
			if file != "-" {
				f, err := os.Open(file)
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				reader = f
			}
			decoder := json.NewDecoder(io.LimitReader(reader, 4<<20))
			if err := decoder.Decode(&body); err != nil {
				return fmt.Errorf("读取构建请求: %w", err)
			}
			if body == nil {
				return fmt.Errorf("构建请求必须为 JSON 对象")
			}
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				return fmt.Errorf("构建请求包含多余 JSON")
			}
		} else {
			path = "/jobs/" + url.PathEscape(args[0]) + "/replay"
		}
		body["options"] = map[string]any{"verbose": verbose, "keep_container": !noKeep, "image_tag": imageTag, "pull_policy": pullPolicy}
		var result struct {
			ID string `json:"id"`
		}
		if err := client.adminRequest(cmd, http.MethodPost, path, body, &result); err != nil {
			return err
		}
		if result.ID == "" {
			return fmt.Errorf("管理 API 未返回任务 id")
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), result.ID)
		return err
	}
	return cmd
}
