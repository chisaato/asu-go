# Spec: build-api（构建 API 与队列）

Depends on: sources-metadata, runtime-runner
Read first: `docs/api-spec.md`；原版 `asu/routers/api.py:78-285`、`asu/build_request.py`

## 目标
与原版 ASU 兼容的构建提交/轮询，SQLite 队列。

## 范围
- `BuildRequest` 模型与校验（字段正则、`allow_defaults`、`max_defaults_length`=20480、`max_custom_rootfs_size_mb`=1024、repositories 白名单、distro/version/target/profile 合法性）。
- 规范 JSON 请求哈希（SHA-256，64 位 hex）；包名规范化（去 `+`、去重、排序）。
- `queue.driver=sqlite`：任务表、状态机（queued → running[stage] → done | failed）、`max_concurrent_builds`、Worker Pool、`max_pending_jobs` 超限 529。
- `POST/GET/HEAD /api/v1/build[/{hash}]`：同哈希去重、202/200/500、`X-Imagebuilder-Status`、`X-Queue-Position`、响应体字段与原版一致；404 错误体格式。
- 任务取消（`/admin/api/jobs/{hash}/cancel`）：对外为 `failed` + `detail: cancelled`。
- 诊断重放：`POST /admin/api/jobs/{hash}/replay`、`POST /admin/api/replay`、`asu replay <hash>|-f file`；独立 id `rp_<ulid>`、`replay_of`、不去重/不缓存/不计统计；默认 verbose + keep_container，可覆盖 image_tag、pull_policy。
- 失败记录：对客户端 `expires_at`（1h）后不可见，管理端保留到 `purge_at`（`diagnostics.retention` 默认 7d）。
- 表结构见 `docs/data-model.md`（jobs、stat_counters）。
- `/api/v1/stats`（`queue_length`）。

## 验收
- 重放：同哈希重放不被去重拦截；原任务记录不被改动；重放不影响统计。
- 用 fake Runtime 的端到端测试覆盖：提交、去重、排队位置、成功、失败、取消、529。
- 请求/响应样例与原版对照一致（字段名、状态码、头）。
- `owut` 或 LuCI 客户端手工冒烟一次完整构建。
