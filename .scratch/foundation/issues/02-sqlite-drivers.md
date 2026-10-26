# 02: SQLite 与驱动骨架

**What to build:** 服务启动时自动迁移数据库；queue/store/stats 驱动按配置选择，未实现的驱动启动即报错。

**Blocked by:** foundation/01

**Status:** ready-for-agent

- [ ] SQLite（modernc，WAL，busy_timeout）与 goose 内嵌迁移，建 `sources`、`jobs`、`stat_counters`、`kv`（见 `docs/data-model.md`）
- [ ] `asu db migrate`（要求服务未运行）
- [ ] queue/store/stats/runtime 驱动接口与注册；配置 `redis`/`s3` 等未实现驱动时报明确错误
- [ ] 迁移与驱动选择的测试

Parent spec: `.scratch/foundation/spec.md`
