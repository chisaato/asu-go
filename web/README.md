# 管理后台骨架

- `make dev`：并行启动 Go :8000 与 Nuxt :3000，浏览器访问 `http://localhost:3000/admin/`。Nuxt 将 `/api`、`/admin/api`、`/json` 代理至 Go；退出时清理 Nuxt 进程。
- `make web`：Bun 安装依赖并生成 `web/dist`；`make build` 随后构建嵌入资源的 `bin/asu`。
- `make test` / `make lint`：同时执行 Go 与前端检查。

仅使用 Bun，提交 `bun.lock`。Nuxt UI v4 的接入依据官方安装文档：<https://ui.nuxt.com/docs/getting-started/installation/nuxt>。

`dist/placeholder.html` 是 dist 内唯一跟踪的资源，保证干净检出后无需 Bun 也可执行 `go build ./...`、`go test ./...`。`public/placeholder.html` 在生成时恢复该占位文件，避免构建删除受跟踪文件。生成产物、依赖与 Nuxt 缓存均不提交；未生成页面时 Go 提示执行 `make web`。本 issue 仅验证链路，没有登录业务。
