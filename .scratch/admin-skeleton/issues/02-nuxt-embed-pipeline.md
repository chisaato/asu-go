# 02: Nuxt 嵌入链路验证

**What to build:** 验证 Bun + Nuxt + `go:embed` + `/admin` 挂载整条链路：`make build` 产出单一二进制，访问 `/admin` 看到空白登录页。

**Blocked by:** foundation/01

**Status:** ready-for-agent

- [ ] `web/`：Bun + Nuxt 4（`ssr:false`、`app.baseURL=/admin/`）+ Nuxt UI v4，仅一个占位页
- [ ] `bun run generate` → `web/dist` → `web/embed.go` 嵌入；Go 接管 `/admin` 并处理 SPA 回退
- [ ] `make dev`（Nuxt dev 代理到 Go :8000）与 `make build` 串起完整流程
- [ ] 仅使用 Bun，无其他包管理器的锁文件

Parent spec: `.scratch/admin-skeleton/spec.md`
