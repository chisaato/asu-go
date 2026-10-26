# 02: Nuxt 嵌入链路验证

**What to build:** 验证 Bun + Nuxt + `go:embed` + `/admin` 挂载整条链路：`make build` 产出单一二进制，访问 `/admin` 看到空白登录页。

**Blocked by:** foundation/01

**Status:** done

- [x] `web/`：Bun + Nuxt 4（`ssr:false`、`app.baseURL=/admin/`）+ Nuxt UI v4，仅一个占位页
- [x] `bun run generate` → `web/dist` → `web/embed.go` 嵌入；Go 接管 `/admin` 并处理 SPA 回退
- [x] `make dev`（Nuxt dev 代理到 Go :8000）与 `make build` 串起完整流程
- [x] 仅使用 Bun，无其他包管理器的锁文件

Parent spec: `.scratch/admin-skeleton/spec.md`

## Comments

### 实现与验证

- 完成：Nuxt UI v4 官方安装方式、Tailwind v4、Nuxt 官方 ESLint 与 Vitest 冒烟测试。
- `web/dist/placeholder.html` 保证未 generate 时 Go 可编译；`web/public/placeholder.html` 在 generate 时重新复制占位文件，避免构建删除受跟踪文件。
- Gin 仅处理未匹配的后台 GET/HEAD 请求，不占用 `/admin/api` 路由空间；显式 API 路由仍优先，未知 API 保持 404。
- 未执行 Bun 构建时，`go build ./...`、`go test ./...` 通过。
- `make build && make test && make lint` 通过。
- 启动 `./bin/asu serve` 后 curl 验证 `/admin/` 包含 Nuxt 脚本与页面标题，`/admin/login` 返回相同 SPA，`/admin/api/x` 返回 404；服务已关闭。
- 本 issue 不实现登录业务；验收项已全部完成。
