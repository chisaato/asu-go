# Spec: admin-skeleton（管理鉴权与前端骨架）

Depends on: foundation, store-ttl
Read first: `docs/admin-dashboard.md` §4, `docs/tech-stack.md` §5, ADR 0004

## 目标
管理接口鉴权，并验证 Bun + Nuxt + `go:embed` + `/admin` 挂载整条链路；不做业务页面。

## 范围
- 鉴权：`admin.users`（bcrypt 哈希）+ session cookie，用于 Web；`admin.api_token` 用于 CLI；公开兼容 API 保持匿名。
- `/admin/api/*` 全量接入鉴权中间件；登录/登出接口。
- `web/`：Bun + Nuxt 4（`ssr:false`，`app.baseURL=/admin/`）+ Nuxt UI v4；仅登录页 + 任务列表一页；`bun run generate` → `web/dist`；`web/embed.go` 嵌入；Go 接管 `/admin` 并处理 SPA 回退。
- dev：Nuxt dev server 代理到 Go :8000；`make dev`、`make build` 串起完整流程。

## 验收
- 未登录访问 `/admin/api/*` 返回 401；token 可用于 CLI。
- `make build` 产出单一二进制，访问 `/admin` 能登录并看到任务列表。

## 非目标
日志终端、SSE、阶段进度、手动构建器、存储页（二期）。
