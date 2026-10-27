# 开发与部署快速上手

## Compose 部署

```bash
cp configs/asu.compose.yaml asu.yaml        # 按需修改；asu.yaml / asu.yml 均已被 git 忽略；Compose 默认挂载 ./asu.yaml，使用 .yml 请同步修改 compose.yaml 里的挂载源
mkdir -p data
docker compose up -d --build                # Podman: podman compose up -d --build
```

- 数据目录 `./data` 是 bind mount（SQLite、日志、产物、元数据缓存）。
- 宿主引擎 socket 挂进容器，构建容器作为“兄弟容器”由宿主创建；构建容器本身不挂载任何宿主路径。
- rootless Podman：先 `systemctl --user enable --now podman.socket`，再 `export ASU_ENGINE_SOCKET=$XDG_RUNTIME_DIR/podman/podman.sock`。
- SELinux 主机：`compose.yaml` 已设置 `security_opt: label=disable`。
- 管理员账号：`docker compose run --rm asu admin hash-password` 生成 bcrypt 哈希，写入 `asu.yaml` 的 `admin.users`；`ASU_ADMIN_TOKEN` 供 CLI 使用。

## 本地开发（含 GoLand）

准备一份本地配置（仓库根目录 `asu.yaml` 或 `asu.yml`，均已被 git 忽略；本地运行两者都会被自动查找）：

```yaml
server: {listen: ":8000"}
log: {format: console, level: debug}
data_dir: data
admin:
  api_token: dev-token
  session_secret: dev-secret-change-me-dev-secret-change-me
  # users 见上文 hash-password
runtime:
  driver: docker
  socket: unix:///var/run/docker.sock   # Podman 用户改为对应 podman.sock
```

命令行：`make dev`（Go 服务 + Nuxt dev server，前端 http://localhost:3000/admin/ 代理到 Go :8000）；仅后端：`go run ./cmd/asu serve --config asu.yaml`。

GoLand：
1. Run → Edit Configurations → + → **Go Build**。
2. Run kind 选 **Package**，Package path 填 `github.com/gzzchh/asu-go/cmd/asu`。
3. Program arguments 填 `serve --config asu.yaml`；Working directory 填仓库根目录。
4. 需要的环境变量（可选）：`ASU_ADMIN_TOKEN=dev-token`。
5. 调试直接点 Debug 即可；前端另开终端 `cd web && bun run dev`。
6. 首次需要 `make web`（或 `cd web && bun install && bun run generate`），否则 `/admin` 只有占位页；纯 API 调试可忽略。

## 用 owut / LuCI 做实机测试

1. 让设备能访问到服务（例如 `http://<主机IP>:8000`，注意防火墙）。
2. 服务首次启动会拉取上游版本列表；拉取 ImageBuilder 镜像较大，首个构建会等待镜像下载（可先 `docker pull ghcr.io/openwrt/imagebuilder:<target>-v<version>`，ImmortalWrt 为 `docker.io/immortalwrt/imagebuilder:<target>-openwrt-<version>`）。
3. 在客户端把 ASU 服务器地址指向本服务（LuCI：attendedsysupgrade 设置里的服务器 URL；owut：见 `owut --help` 中的 ASU 服务器相关选项）。
4. 观察构建：`curl -H "Authorization: Bearer dev-token" http://localhost:8000/admin/api/jobs`；实时日志：`curl -N -H "Authorization: Bearer dev-token" "http://localhost:8000/admin/api/jobs/<hash>/log?follow=1"`。
5. 构建失败时用 `asu replay <hash>` 做诊断重放（默认带 `V=s` 并保留容器）。
