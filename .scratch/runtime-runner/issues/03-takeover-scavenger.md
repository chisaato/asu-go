# 03: 接管与回收

**What to build:** 服务重启后，运行中的构建被接管并继续收尾，日志不丢不重；孤儿容器被清理。

**Blocked by:** runtime-runner/02

**Status:** done

- [x] 重启接管：按 `Handle` 查询容器，运行中/已退出均从头重放日志并覆写日志文件，容器不存在则 `failed(interrupted)`
- [x] Zombie Scavenger：清理带 `managed-by=asu-go` 但无活跃任务的容器
- [x] `pull_policy`（missing/always/never）与 `rolling` 复查
- [x] `diagnostics.keep_container_ttl`、`diagnostics.max_kept` 的保留容器回收
- [x] 集成测试：构建中途重启服务，日志完整且无重复行

实现与接入：`internal/runner/README.md`。提供 `JobStore`、SQLite 实现及内存测试实现；serve 的 errgroup 管理接管与周期回收。队列接入前，恢复产物暂存于 `{data_dir}/recovery/{id}`；build-api/02 需要包装 Complete 发布产物并构造公开 result（不能直接暴露 Runner Result）。

验证通过：`make build && make test && make lint`；`ASU_TEST_RUNTIME_DRIVER=podman ASU_TEST_RUNTIME_SOCKET=unix:///var/run/docker.sock go test -tags integration ./internal/runner/... ./internal/runtime/...`。集成测试关闭原 Runner 的日志连接与 Hub 模拟崩溃，创建新 Runner/Hub 接管同一 Handle，验证 ring/磁盘日志完整且无重复，并验证 kill 后残留环境被 Scavenger 回收。

Parent spec: `.scratch/runtime-runner/spec.md`
