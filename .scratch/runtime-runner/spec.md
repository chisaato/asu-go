# Spec: runtime-runner（运行时与构建执行）

Depends on: foundation
Read first: ADR 0003, `docs/runner-and-observability.md`, `docs/tech-stack.md` §4

## 目标
`Runtime` 接口 + Docker/Podman 驱动 + Runner（执行一次构建、流式日志、取消、接管）。

## 范围
- `internal/runtime`：接口（Prepare/Start/Logs/Wait/Kill/Info，及 put/get archive 所需能力）、不透明 `Handle`、fake 实现。
- Docker 驱动（Moby client），Podman 通过兼容 socket；启动时检测引擎、版本、日志驱动可读性（`none` 拒绝启动）。
- 容器：不设 AutoRemove、无宿主 bind mount、`cap_drop=all`、`no_new_privileges`、网络 `asu-build` 自动创建、label `managed-by=asu-go`。
- 流程：create → put_archive → start（构建脚本）→ logs follow → wait → get_archive → remove。
- 构建脚本模板与 `##asu:stage=<name>` 标记解析；`verbose` 注入 `V=s`。
- `internal/stream`：RingBuffer、日志落盘、广播器；`GET /admin/api/jobs/{hash}/log?follow=1`。
- 超时、取消（Kill + 清理）；重启接管（日志重放、覆写日志文件）；Zombie Scavenger。
- 诊断：`keep_container` 失败保留容器（label 标记），`diagnostics.keep_container_ttl`（1h）与 `diagnostics.max_kept`（3），超出删最旧，由 Scavenger 回收。
- 镜像拉取策略 `pull_policy`，`rolling` 复查。

## 验收
- 单元测试用 fake Runtime 覆盖 Runner 状态流转。
- `integration` tag：用 alpine 假构建脚本在真实 Docker 与 Podman 上验证 follow、stage 标记、kill、接管（含日志不丢不重）、清理。
- 手工冒烟：真实 ImageBuilder 构建 x86_64 固件（不进 CI）。
