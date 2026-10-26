# 01: 来源目录与管理 CLI

**What to build:** 管理员可通过 CLI 查看、新增、编辑、启停 source，并导入导出 YAML；首次启动自带 OpenWrt 与 ImmortalWrt 默认目录。

**Blocked by:** foundation/02

**Status:** ready-for-human

- [x] 内嵌默认目录首次启动入库，镜像基名 + tag 模板（OpenWrt/ImmortalWrt 规则以实测 tag 为准）
- [x] `/admin/api/sources` CRUD，`admin.api_token` 鉴权中间件
- [x] `asu source list|add|edit|enable|export|import`，全部经 API
- [x] 本地改动记录到 `overrides`
- [x] 测试：导入导出往返一致；无 token 返回 401

Parent spec: `.scratch/sources-metadata/spec.md`

## Comments

### 2026-10-27 实现完成，待审阅

- 新增 `internal/sources`：SQLite Store、模型、内嵌 YAML 默认目录、事务初始化/导入、按 `(distribution, version)` upsert、本地字段覆盖记录、镜像模板解析。默认目录包含两发行版当前 stable、oldstable 与 SNAPSHOT 共 6 条。
- 默认版本查证：[OpenWrt .versions.json](https://downloads.openwrt.org/.versions.json) 的 stable/oldstable 为 `25.12.6` / `24.10.8`；[ImmortalWrt .versions.json](https://downloads.immortalwrt.org/.versions.json) 为 `25.12.2` / `24.10.6`。
- ImmortalWrt tag 规则查证来源：[Docker Hub tags API](https://hub.docker.com/v2/repositories/immortalwrt/imagebuilder/tags?name=mediatek-filogic&page_size=10)。响应真实存在 `mediatek-filogic-openwrt-25.12.2`、`mediatek-filogic-openwrt-25.12.1`、`mediatek-filogic-master`、`mediatek-filogic-openwrt-24.10`（以及 snapshot 别名）。因此正式版默认 `openwrt-<version>`，SNAPSHOT 默认 `master`，分支 SNAPSHOT 默认 `openwrt-<branch>`；不保证每个 target/rc 均存在，用户可覆盖 `tag_template`。代码注释同步保留来源。
- 新增 `internal/admin` 来源 CRUD / 原子导入接口，Bearer 无/错 token 为 401、未配置 token 为 503；鉴权验证器可与后续 session cookie 组合。
- CLI 完成全部所列命令并额外支持 `disable`；仅通过 HTTP，支持 `--server`、`--token`，以及 `ASU_ADMIN_TOKEN` / YAML token。使用说明见 `internal/sources/README.md`。
- 验证：在隔离 worktree 执行 `make build && make test && make lint` 全部通过（Go、Vitest、golangci-lint、ESLint）。覆盖临时 SQLite CRUD、初始化幂等、YAML 完整往返、跨库导入、upsert/回滚、overrides、401/503、管理 API CRUD、CLI httptest 与 token 优先级、ResolveImage 表驱动测试。
- 实际运行 `./bin/asu serve`，临时 data_dir 位于 `/tmp/opencode`，设置 `ASU_ADMIN_TOKEN`，真实执行 `./bin/asu source list/add/export` 成功：首次 6 条默认来源，新增本地条目后导出 7 条；服务 SIGTERM 正常退出，临时目录已清理。
- 无阻塞遗留；上游同步与镜像实际拉取仍属于后续 issue，本次没有拉取 GB 级镜像。
