# 数据模型 (Data Model)

SQLite（`modernc.org/sqlite`，WAL）。只有 `asu serve` 进程打开数据库（见 ADR 0004）。迁移用 goose，文件内嵌。字段名一律小写 snake_case。

关系：`jobs.source_id → sources.id`（N:1，可空，`ON DELETE SET NULL`）。`stat_counters`、`kv` 无外键。

## sources（发行流目录，见 ADR 0002）

| 列 | 类型 | 说明 |
| --- | --- | --- |
| id | INTEGER PK | |
| distribution | TEXT | openwrt / immortalwrt / 自定义 |
| branch | TEXT | 如 23.05、24.10、SNAPSHOT；`branches.json` 用 GROUP BY 生成，不单独建表 |
| version | TEXT | UNIQUE(distribution, version) |
| channel | TEXT NULL | upcoming / stable / oldstable |
| upstream_url | TEXT | |
| image | TEXT | 镜像基名 |
| tag_template | TEXT | 如 `{target}-v{version}` |
| package_manager | TEXT | opkg / apk |
| origin | TEXT | upstream / local |
| rolling | BOOL | 滚动 tag，定期复查镜像 |
| enabled | BOOL | |
| overrides | JSON | 被本地修改过的字段名数组，同步时跳过 |
| targets | JSON | `.targets.json` 内容，同步时整块替换 |
| synced_at | TIMESTAMP | |

## jobs（任务与构建结果）

| 列 | 类型 | 说明 |
| --- | --- | --- |
| request_hash | TEXT PK | 普通任务为 64 位 hex；重放任务为 `rp_<ulid>` |
| replay_of | TEXT NULL | 重放来源的 request_hash |
| source_id | INTEGER NULL FK | → sources.id |
| version, target, profile | TEXT | 与 `request` 重复的冗余列，用于筛选与统计，加索引 |
| status | TEXT | queued / running / done / failed |
| stage | TEXT NULL | init / container_setup / validate_revision / validate_manifest / building_image |
| enqueued_at, started_at, finished_at | TIMESTAMP | |
| expires_at | TIMESTAMP | 对客户端公开可见的截止时间（TTL 规则见 build-api spec） |
| purge_at | TIMESTAMP | 管理端保留与物理删除时间（`diagnostics.retention`，默认 7d） |
| detail | TEXT | 失败原因 / `cancelled` / `interrupted` |
| error | TEXT | stderr 尾部 |
| client | TEXT NULL | |
| request | JSON | 完整规范化 BuildRequest（含 packages 数组），重放的数据来源 |
| result | JSON | images、manifest、build_cmd_packages 等，随上游 profiles.json 变化，不拆列 |
| handle | JSON | 运行时不透明句柄 `{driver, id}` |
| stages | JSON | `[{name, at}]` 阶段时间线 |
| options | JSON | 重放选项（verbose、keep_container、镜像 tag 覆盖、pull_policy） |

## stat_counters（统计计数）

PK(day, metric, dim)；`value INTEGER`。

- `metric`：`requests` / `cache_hits` / `failures` / `package`。
- `dim`：版本或分支；`metric=package` 时为包名。热门包靠这里，因为 jobs 按 TTL 回收后无法回溯 30 天。
- 重放任务不计入统计。

## kv（杂项状态）

`key TEXT PK`，`value JSON`。例如 `metadata.last_sync`。

## 刻意不建表

| 可能的表 | 处理 |
| --- | --- |
| branches | `sources.branch` 列 |
| targets / profiles | targets 放 JSON；profiles.json 与包索引走磁盘缓存 |
| metadata_cache | 缓存文件旁 `.meta.json`（ETag、Last-Modified、fetched_at） |
| job_packages | `jobs.request` JSON，用 `json_each` |
| job_stages / job_events | `jobs.stages` JSON |
| admin_users / sessions | 用户来自 YAML（bcrypt），session 用签名 cookie |
| 构建日志 | 磁盘 `.log` 文件，路径由 id 推出 |

## 诊断重放

- 入口：`POST /admin/api/jobs/{hash}/replay`、`POST /admin/api/replay`（直接提交原始 BuildRequest JSON）；CLI `asu replay <hash>`、`asu replay -f request.json`。
- 重放任务有独立 id，不参与去重、不写公开缓存、不计统计，`replay_of` 指向原任务。
- 默认诊断模式：`verbose=true`；失败时保留容器（label 标记）；可覆盖镜像 tag 或 `pull_policy=always`。
- 保留容器：`diagnostics.keep_container_ttl` 默认 1h，`diagnostics.max_kept` 默认 3，超出删最旧；由 Scavenger 回收。
- 失败记录对客户端只在 `expires_at` 前可见（沿用原版 1h），管理端保留到 `purge_at`。
