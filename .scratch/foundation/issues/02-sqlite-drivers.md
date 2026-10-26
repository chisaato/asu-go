# 02: SQLite 与驱动骨架

**What to build:** 服务启动时自动迁移数据库；queue/store/stats 驱动按配置选择，未实现的驱动启动即报错。

**Blocked by:** foundation/01

**Status:** done

- [x] SQLite（modernc，WAL，busy_timeout）与 goose 内嵌迁移，建 `sources`、`jobs`、`stat_counters`、`kv`（见 `docs/data-model.md`）
- [x] `asu db migrate`（要求服务未运行）
- [x] queue/store/stats/runtime 驱动接口与注册；配置 `redis`/`s3` 等未实现驱动时报明确错误
- [x] 迁移与驱动选择的测试

Parent spec: `.scratch/foundation/spec.md`

## Comments

- 已实现，待人工审阅合并。`make build && make test && make lint` 全部通过（lint 0 issues）。
- 已在临时目录真实启动 `./bin/asu serve`：`/healthz` 返回 200，生成 `asu.sqlite`；运行时 `db migrate` 被锁拒绝；SIGTERM 正常退出，随后检查四张业务表、goose 版本表与 WAL，离线迁移再次执行成功。
- 测试覆盖重复迁移、列与索引、source 唯一约束、外键删除置空、锁释放、注册选择、未实现驱动在 `config check`/启动时报错，以及 `stats` 配置标签和示例严格解码。
- 锁的限制与使用方式见 `internal/db/README.md`：本地 Unix advisory flock，不防御绕过入口的外部程序，不支持网络文件系统/Windows。驱动仅注册描述符，业务接口留给后续实现，runtime 完整接口留给 runtime-runner/01。
