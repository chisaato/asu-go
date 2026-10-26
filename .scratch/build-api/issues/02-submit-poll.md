# 02: 提交与轮询

**What to build:** 客户端可提交构建并轮询到结果；相同请求去重；过载被拒绝。（用 fake Runtime 端到端验证。）

**Blocked by:** build-api/01, sources-metadata/03, runtime-runner/02, foundation/02

**Status:** ready-for-agent

- [ ] SQLite 队列与 Worker Pool，`queue.max_concurrent_builds`，`max_pending_jobs` 超限 529
- [ ] `POST /api/v1/build`、`GET|HEAD /api/v1/build/{hash}`：同哈希去重，202/200/500，`X-Imagebuilder-Status`、`X-Queue-Position`，404 错误体与原版一致
- [ ] 请求合法性校验接入元数据（distro/version/target/profile）
- [ ] `/api/v1/stats`（`queue_length`）；`stat_counters` 写入 requests/cache_hits/failures/package
- [ ] 端到端测试：提交、去重、排队位置、成功、失败、529
- [ ] `owut` 或 LuCI 手工冒烟一次完整构建

Parent spec: `.scratch/build-api/spec.md`
