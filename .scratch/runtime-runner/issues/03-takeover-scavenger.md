# 03: 接管与回收

**What to build:** 服务重启后，运行中的构建被接管并继续收尾，日志不丢不重；孤儿容器被清理。

**Blocked by:** runtime-runner/02

**Status:** ready-for-agent

- [ ] 重启接管：按 `Handle` 查询容器，运行中/已退出均从头重放日志并覆写日志文件，容器不存在则 `failed(interrupted)`
- [ ] Zombie Scavenger：清理带 `managed-by=asu-go` 但无活跃任务的容器
- [ ] `pull_policy`（missing/always/never）与 `rolling` 复查
- [ ] `diagnostics.keep_container_ttl`、`diagnostics.max_kept` 的保留容器回收
- [ ] 集成测试：构建中途重启服务，日志完整且无重复行

Parent spec: `.scratch/runtime-runner/spec.md`
