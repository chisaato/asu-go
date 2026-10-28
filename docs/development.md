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
log: {format: text, level: debug}
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
2. Run kind 选 **Package**，Package path 填 `github.com/chisaato/asu-go/cmd/asu`。
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

## owut 真实抓包回归夹具

`internal/api/testdata/owut/` 来自用户提供的 `owut.pcapng`（2026-10-27，SHA-256：`87fad12673e4484c45a663781b8b68330cc5cbefd7cdcf15b7acf682f615b89a`），客户端为 `owut/2026.01.13~2526d84b-r1`。共 12 个 HTTP 事务：9 个 ImmortalWrt 24.10.6 / 25.12.2 元数据 GET、1 个构建 POST、失败后的 HEAD 与 GET。抓包没有单独的 latest、revision、profile 请求，不能将合成数据标成真实抓包；latest 回归取自真实 overview 的 latest 字段。

- `transactions.json` 保存全部事务的方法、路径、脱敏请求/响应头、响应状态及正文文件名；`body_sha256` 是裁剪前、解 chunk 后原始正文的摘要，`selection` 说明裁剪范围。重复正文共用文件，HEAD 正文为 null。
- `overview.json` 保留完整分支/版本/服务器信息，各分支 targets 仅保留 `mediatek/filogic`。`packages-*.json` 与 `target-index-*.json` 只选取少量真实包，保留原始包名、版本与架构。构建请求、queued/failed 响应正文逐字保留。
- 测试使用内存 HTTP transport 和 fake Runtime，无网络、容器或原始 pcap 文件依赖。由于抓包记录的是 ASU 输出而非下载站上游，fake upstream 仅添加 v2 index 包装与单个合成 feed，profiles 返回 404 以跳过二次 kmod 合并；不把这些包装称为真实上游数据。构建 profile 查询使用已有 fake。
- `TestOWUTCapturedRequest` 固定规范请求哈希；`TestOWUTCapturedMetadataTransactions` 重放 9 个元数据事务并验证 latest 发行版隔离；`TestOWUTCapturedBuildSubmitAndFailure` 验证前缀提交 202、保存的 distro、失败 GET/HEAD 的 500 与响应头/字段（对照原版 `return_job_v1`）。仅排队时间允许变化。

更新方法：先用 `tshark -r <新抓包> -Y http -T fields -e tcp.stream -e http.request.method -e http.request.uri -e http.response.code` 核对事务，再用 `tshark -r <新抓包> -q -z follow,tcp,raw,<stream>` 分方向重组 TCP 流，拆分 HTTP 头/体并解码 chunked（HEAD 不读取正文）。提取脚本只放 `/tmp/opencode/`，原始抓包不入库。生成 manifest 前删除 Authorization、Proxy-Authorization、Cookie、Set-Cookie 等敏感头，检查正文是否有秘密；大 JSON 只能删除不需要的条目，不得手改真实字段值。更新文件、摘要、裁剪说明和来源信息，复核哈希；若旧服务响应确有错误，应在测试注明并断言修正后的发行版隔离行为，而不是照抄错误。最后运行 `make build && make test && make lint`。
