# Spec: foundation（项目骨架）

Depends on: -
Read first: `docs/tech-stack.md`, ADR 0001, ADR 0004

## 目标
可编译、可运行的空壳服务，固定工程约定，后续 spec 在其上叠加。

## 范围
- `go.mod`、`cmd/asu/main.go`、`internal/cli`（Cobra：`serve`、`config check`、`version`、`db migrate`）。
- `internal/config`：强类型 YAML 配置、全小写 snake_case 显式标签、严格解码、校验（错误带 YAML 路径）、`ASU_*` 环境变量与 flag 覆盖；`configs/asu.example.yaml`。
- Zap 日志：`log.format=json|console`、`log.level`，字段 snake_case。
- `internal/db`：SQLite（`modernc.org/sqlite`，WAL，`busy_timeout`），goose 内嵌迁移，启动自动迁移；初始迁移建 `sources`、`jobs`、`stat_counters`、`kv`（见 `docs/data-model.md`）。
- 驱动接口骨架：`queue`、`store`、`stats`、`runtime`；未实现的驱动配置启动即报错。
- `serve`：Gin 监听、`errgroup` 管理后台 goroutine、优雅关闭；`/healthz`。
- `Makefile`（`dev`/`build`/`test`/`lint`）、`golangci-lint` 配置、`.gitignore`（`web/dist`、`data/`）。

## 验收
- `asu config check` 对示例配置通过，对拼错键名/未知驱动报带路径的错误。
- 测试：所有 `yaml` 标签匹配 `^[a-z][a-z0-9_]*$`；示例配置可被严格解码。
- `make build`、`make test`、`make lint` 通过。

## 非目标
业务逻辑、前端。
