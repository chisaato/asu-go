# 01: Runtime 接口与 Docker/Podman 驱动

**What to build:** 在真实 Docker 与 Podman 上，用 alpine 假构建完整跑通 create → put_archive → start → logs follow → wait → get_archive → remove。

**Blocked by:** foundation/02

**Status:** ready-for-agent

- [ ] `Runtime` 接口（含 put/get archive）、不透明 `Handle`、fake 实现
- [ ] Docker 驱动（Moby client），Podman 经兼容 socket
- [ ] 启动检测：引擎与版本、日志驱动可读（`none` 拒绝启动）
- [ ] 容器参数：无 AutoRemove、无宿主 bind mount、`cap_drop=all`、`no_new_privileges`、网络 `asu-build` 自动创建、label `managed-by=asu-go`
- [ ] `integration` tag 测试在 Docker 与 Podman 上通过

Parent spec: `.scratch/runtime-runner/spec.md`
