# 01: Runtime 接口与 Docker/Podman 驱动

**What to build:** 在真实 Docker 与 Podman 上，用 alpine 假构建完整跑通 create → put_archive → start → logs follow → wait → get_archive → remove。

**Blocked by:** foundation/02

**Status:** done（Docker 真机验证待补：本机无 dockerd）

- [x] `Runtime` 接口（含 put/get archive）、不透明 `Handle`、fake 实现
- [x] Docker 驱动（Moby client），Podman 经兼容 socket
- [x] 启动检测：引擎与版本、日志驱动可读（`none` 拒绝启动）
- [x] 容器参数：无 AutoRemove、无宿主 bind mount、`cap_drop=all`、`no_new_privileges`、网络 `asu-build` 自动创建、label `managed-by=asu-go`
- [~] `integration` tag 测试：Podman 通过；Docker 因本机无 dockerd 未验证

Parent spec: `.scratch/runtime-runner/spec.md`

## 实现与验证记录

- `make build && make test && make lint` 全部通过（含前端生成、vitest 与 eslint）。
- `Create(RunSpec) -> Handle` 与 `Start(Handle)` 分离，支持启动前注入 tar；`Kill` 保留产物，`Remove` 最终清理。接口已同步技术栈文档。
- 使用官方 `github.com/moby/moby/client v0.6.2`（`client.New` 及新版 options/result API），Podman 仅在兼容 info 缺少日志驱动时补读原生只读 info。
- Podman 5.8.9 经系统 socket 完整通过 integration：首行在后续输出前收到、stderr 解帧、退出码、退出后产物回收、时间戳重放、Kill 与重复清理、标签及网络。
- Docker 验证受环境阻塞：本机 Docker 29.9.1 仅为客户端，无 dockerd；`/var/run/docker.sock` 实际链接 Podman，不能将此结果视为 Docker 通过。
- 自建 Podman 用户 socket 已测试并关闭；该用户无 subuid/subgid，alpine 拉取报 GID 42 无法映射。未修改宿主用户映射，改用现有系统 Podman socket 验证。
- 待具备真实 Docker daemon 的环境补跑 `go test -tags integration ./internal/runtime/...`，再勾选最后一项。
