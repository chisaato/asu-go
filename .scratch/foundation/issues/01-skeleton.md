# 01: 空壳服务

**What to build:** 可编译运行的服务骨架：`asu serve` 启动并响应 `/healthz`，`asu config check` 校验配置，日志可在 json/console 间切换。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] Cobra 子命令 `serve`、`config check`、`version`
- [x] 强类型 YAML 配置：严格解码、全小写 snake_case 显式标签、flag/`ASU_*` 覆盖、带 YAML 路径的校验错误，`configs/asu.example.yaml`
- [x] Zap 日志（`log.format`、`log.level`）
- [x] `serve` 用 errgroup 管理后台 goroutine，优雅关闭
- [x] 测试：所有 yaml 标签匹配 `^[a-z][a-z0-9_]*$`；示例配置可被严格解码
- [x] `Makefile`（dev/build/test/lint）、golangci-lint 配置、`.gitignore`
- [x] `make build`、`make test`、`make lint` 通过

Parent spec: `.scratch/foundation/spec.md`
