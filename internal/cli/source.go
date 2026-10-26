package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gzzchh/asu-go/internal/config"
	"github.com/gzzchh/asu-go/internal/sources"
	"github.com/spf13/cobra"
)

type sourceClient struct{ server, token string }

func (client sourceClient) request(cmd *cobra.Command, method, path string, input, output any) error {
	u, err := url.Parse(client.server)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("--server 必须是 HTTP(S) 地址")
	}
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(cmd.Context(), method, strings.TrimRight(client.server, "/")+"/admin/api/sources"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+client.token)
	req.Header.Set("Content-Type", "application/json")
	// 不跟随重定向，避免管理凭据被转发至其他地址。
	httpClient := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求管理 API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("管理 API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(output)
	}
	return nil
}

func newSourceCommand(load func(*cobra.Command) (config.Config, error), server, token *string) *cobra.Command {
	root := &cobra.Command{Use: "source", Short: "经管理 API 管理来源目录"}
	clientFor := func(cmd *cobra.Command) (sourceClient, error) {
		cfg, err := load(cmd)
		if err != nil {
			return sourceClient{}, err
		}
		credential := cfg.Admin.APIToken
		if cmd.Flags().Changed("token") {
			credential = *token
		}
		return sourceClient{server: *server, token: credential}, nil
	}
	for _, name := range []string{"list", "export"} {
		root.AddCommand(&cobra.Command{Use: name, Short: "列出来源（YAML）", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				client, err := clientFor(cmd)
				if err != nil {
					return err
				}
				var catalog []sources.Source
				if err := client.request(cmd, http.MethodGet, "", nil, &catalog); err != nil {
					return err
				}
				return sources.Export(cmd.OutOrStdout(), catalog)
			},
		})
	}
	for _, name := range []string{"add", "edit"} {
		values := map[string]*string{}
		bools := map[string]*bool{}
		command := &cobra.Command{Use: name, Short: "新增或编辑来源（edit 需要 ID；仅提交显式 flag）", Args: cobra.NoArgs}
		if name == "edit" {
			command.Use = "edit ID"
			command.Args = sourceIDArg
		}
		for _, field := range []string{"distribution", "branch", "version", "channel", "upstream_url", "image", "tag_template", "package_manager", "origin"} {
			values[field] = command.Flags().String(strings.ReplaceAll(field, "_", "-"), "", field)
		}
		for _, field := range []string{"rolling", "enabled"} {
			bools[field] = command.Flags().Bool(field, false, field)
		}
		command.RunE = func(cmd *cobra.Command, args []string) error {
			client, err := clientFor(cmd)
			if err != nil {
				return err
			}
			patch := map[string]any{}
			for field, value := range values {
				if cmd.Flags().Changed(strings.ReplaceAll(field, "_", "-")) {
					patch[field] = *value
				}
			}
			for field, value := range bools {
				if cmd.Flags().Changed(field) {
					patch[field] = *value
				}
			}
			method, path := http.MethodPost, ""
			if cmd.Name() == "edit" {
				method, path = http.MethodPatch, "/"+args[0]
			}
			var source sources.Source
			if err := client.request(cmd, method, path, patch, &source); err != nil {
				return err
			}
			return sources.Export(cmd.OutOrStdout(), []sources.Source{source})
		}
		root.AddCommand(command)
	}
	for _, name := range []string{"enable", "disable"} {
		root.AddCommand(&cobra.Command{Use: name + " ID", Short: "启用或停用来源", Args: sourceIDArg,
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := clientFor(cmd)
				if err != nil {
					return err
				}
				return client.request(cmd, http.MethodPatch, "/"+args[0], map[string]bool{"enabled": cmd.Name() == "enable"}, nil)
			},
		})
	}
	root.AddCommand(&cobra.Command{Use: "import [FILE|-]", Short: "导入 YAML，按发行版与版本 upsert（默认 stdin）", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientFor(cmd)
			if err != nil {
				return err
			}
			reader := cmd.InOrStdin()
			if len(args) > 0 && args[0] != "-" {
				file, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()
				reader = file
			}
			catalog, err := sources.DecodeCatalog(io.LimitReader(reader, 4<<20))
			if err != nil {
				return fmt.Errorf("导入目录: %w", err)
			}
			return client.request(cmd, http.MethodPost, "/import", catalog, nil)
		},
	})
	return root
}

func sourceIDArg(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("ID 必须为正整数")
	}
	return nil
}
