# 02: 提交与轮询

**What to build:** 客户端可提交构建并轮询到结果；相同请求去重；过载被拒绝。（用 fake Runtime 端到端验证。）

**Blocked by:** build-api/01, sources-metadata/03, runtime-runner/02, foundation/02

**Status:** done（owut/LuCI 手工冒烟待人工）

- [x] SQLite 队列与 Worker Pool，`queue.max_concurrent_builds`，`max_pending_jobs` 超限 529
- [x] `POST /api/v1/build`、`GET|HEAD /api/v1/build/{hash}`：同哈希去重，202/200/500，`X-Imagebuilder-Status`、`X-Queue-Position`，404 错误体与原版一致
- [x] 请求合法性校验接入元数据（distro/version/target/profile）
- [x] `/api/v1/stats`（`queue_length`）；`stat_counters` 写入 requests/cache_hits/failures/package
- [x] 端到端测试：提交、去重、排队位置、成功、失败、529
- [ ] `owut` 或 LuCI 手工冒烟一次完整构建

Parent spec: `.scratch/build-api/spec.md`

实现与兼容差异见 `internal/queue/README.md`。覆盖并发去重、worker 并发上限、queued 重启恢复、running 接管发布、阶段/日志、TTL 到期重建与重放隔离；另有 `integration` 标签的 Podman 小镜像 POST→轮询→done 全链路测试。

人工冒烟未执行：需要真实 owut/LuCI 客户端、设备及正式 ImageBuilder；自动测试不冒充此验收项。下载路由及 TTL 物理清理由 store-ttl 后续 issue 接续。

验证通过：`make build && make test && make lint`（含前端）；`ASU_TEST_RUNTIME_DRIVER=podman ASU_TEST_RUNTIME_SOCKET=unix:///var/run/docker.sock go test -tags integration ./internal/api/... ./internal/runner/... ./internal/runtime/...`。未 push、未合并。
