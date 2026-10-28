# ASU-Go 🚀

用 Go 重写的 OpenWrt **Attended Sysupgrade** 服务端：单二进制、自带管理后台、构建日志实时可见。

兼容官方客户端（LuCI `luci-app-attendedsysupgrade`、`owut`、firmware-selector），同时服务 OpenWrt 与 ImmortalWrt。

## ✨ 为什么选它

| | 原版 ASU | ASU-Go |
| --- | --- | --- |
| 部署 | FastAPI + Redis Stack + RQ Worker，至少 3 个容器 | 📦 单二进制，无 Redis |
| 构建过程 | 黑盒，只能干等 | 📡 SSE 实时日志，分阶段进度 |
| 运维 | 只读统计页 | 🖥️ 内置后台：任务看板、取消、重试、缓存清理 |
| 协议 | — | ✅ 公开 API 与请求哈希保持兼容 |
| 资源 | Python 常驻，内存偏大 | 🪶 Go 单进程，轻量 |

- 🔌 **运行时抽象**：Docker / Podman 开箱即用，Kubernetes 接口预留
- 💾 **SQLite 持久化**：免 CGO，重启后可接管在途构建，容器自动清理
- ⚡ **包文件缓存**与**包仓库镜像替换**，加速构建、适配国内网络
- 🌐 **多发行版**：`/openwrt`、`/immortalwrt` 前缀各自独立

## 🏁 Quick Start

**推荐使用 Docker Compose 部署**（Podman 亦可）。

```bash
cp configs/asu.compose.yaml asu.yaml   # 按需修改
mkdir -p data
docker compose up -d --build          # Podman: podman compose up -d --build
```

- 公开 API：`http://localhost:8000`
- 管理后台：`http://localhost:8000/admin`
- 客户端接入：`owut check --server http://host:8000`（ImmortalWrt 用 `http://host:8000/immortalwrt`）

`main` 分支每次提交会发布 `ghcr.io/chisaato/asu-go:dev` 镜像。管理员账号、rootless Podman、SELinux、反向代理等细节见 [部署文档](docs/development.md#compose-部署)。

## 📚 文档

| 主题 | 文档 |
| --- | --- |
| 部署与本地开发 | [docs/development.md](docs/development.md) |
| 系统架构 | [docs/architecture.md](docs/architecture.md) |
| API 兼容与扩展 | [docs/api-spec.md](docs/api-spec.md) |
| 容器执行与可观测性 | [docs/runner-and-observability.md](docs/runner-and-observability.md) |
| 管理后台 | [docs/admin-dashboard.md](docs/admin-dashboard.md) |
| 数据模型 | [docs/data-model.md](docs/data-model.md) |
| 技术栈与工程约定 | [docs/tech-stack.md](docs/tech-stack.md) |
| 术语 / 决策记录 | [GLOSSARY.md](GLOSSARY.md) · [docs/adr/](docs/adr/) |
| 配置示例 | [configs/asu.example.yaml](configs/asu.example.yaml) |

## 🛠️ 技术栈

Go · Gin · Cobra · YAML 配置 · Zap · SQLite ｜ Bun · Nuxt 4 · Nuxt UI（构建后 `go:embed` 嵌入二进制）

## 📄 许可证

[MIT](LICENSE)

## 🤝 参与开发

`make dev` / `make build` / `make test` / `make lint`，约定见 [AGENTS.md](AGENTS.md) 与 [docs/tech-stack.md](docs/tech-stack.md)。
