# 管理后台

- `make dev`：并行启动 Go :8000 与 Nuxt :3000，浏览器访问 `http://localhost:3000/admin/`。Nuxt 将 `/api`、`/admin/api`、`/json` 代理至 Go；退出时清理 Nuxt 进程。
- `make web`：Bun 安装依赖并生成 `web/dist`；`make build` 随后构建嵌入资源的 `bin/asu`。
- `make test` / `make lint`：同时执行 Go 与前端检查。

仅使用 Bun，提交 `bun.lock`。Nuxt UI v4 的接入依据官方安装文档：<https://ui.nuxt.com/docs/getting-started/installation/nuxt>。

`dist/placeholder.html` 是 dist 内唯一跟踪的资源，保证干净检出后无需 Bun 也可执行 `go build ./...`、`go test ./...`。`public/placeholder.html` 在生成时恢复该占位文件，避免构建删除受跟踪文件。生成产物、依赖与 Nuxt 缓存均不提交；未生成页面时 Go 提示执行 `make web`。

## 页面与认证

- `/admin/login` 使用 `admin.users` 配置的账号密码登录，cookie 会话由后端管理；其他页面由全局路由守卫检查 `/admin/api/me`。配置方法见 `docs/admin-dashboard.md` §5。
- `/admin/` 显示构建任务表，支持状态筛选、服务端分页、手动刷新和可关闭的 5 秒轮询（当前页存在排队/运行任务且页面可见时）。相对时间的提示包含本地绝对时间。
- 点击任务 ID 或行打开只读详情抽屉，展示请求、结果、错误 JSON 与阶段时间线；不提供日志、SSE 或构建操作。
- `useAdminApi` 统一携带 cookie 与 `X-Requested-With: asu-admin`，受保护接口返回 401 时跳转 `/login`（由 Nuxt 补上 `/admin/` 基础路径）；登录失败不触发重定向。
- Nuxt UI 使用简体中文 locale，明暗主题默认跟随系统。无额外状态管理或浏览器自动化依赖。
