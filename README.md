# ASU-Go (Attended Sysupgrade Server in Go)

一个轻量、高性能、高度可观测且自带管理控制台的 OpenWrt / 嵌入式 Linux 固件定制构建服务端。

基于 Go 语言重写，旨在彻底替代原版 Python FastAPI + Redis-Stack + RQ 的复杂技术栈，兼容官方客户端（LuCI `luci-app-attendedsysupgrade`、`owut` CLI、`firmware-selector` 等），并解决原版构建黑盒、无实时构建日志流、缺乏任务运维后台的痛点。

---

## 核心特性

- **单二进制 All-in-One 部署**：
  - 内置调度引擎与轻量任务队列，单机运行无需部署 Redis 或 RQ Worker。
  - 内置 Web 管理控制台静态资产（通过 `embed.FS` 嵌入），开箱即用。
- **全链路实时构建可观测性**：
  - 直连 Docker / Podman Unix Socket，通过 Hijack 流式读取构建容器的标准输出与错误流。
  - 支持类似 CI/CD 的实时控制台滚动（基于 SSE / WebSocket）与分阶段构建进度展示。
- **完善的 Web 管理后台**：
  - 任务监控看板（运行中、排队中、历史任务、失败溯源）。
  - 任务生命周期控制（卡死任务强制 Kill 并级联清理底层容器）。
  - 产物与磁盘缓存管理（固件包过期清理、容量监控）。
  - 手动在线构建调试器。
- **100% 协议向后兼容**：
  - 兼容 OpenWrt 官方 ASU 核心 API（版本元数据同步、Profile 匹配、请求哈希计算、构建轮询协议）。
- **极低系统资源占用**：
  - 单进程常驻内存仅需 ~20MB，Goroutine 轻量级高并发。

---

## 技术栈

Go + Gin + Cobra + YAML 配置 + SQLite；前端 Bun + Nuxt 4 (SPA) + Nuxt UI，构建后嵌入二进制；容器运行时经 `Runtime` 接口抽象（Docker / Podman，预留 Kubernetes）。详见 [技术栈与工程约定](docs/tech-stack.md)。

---

## 目录结构规划

```text
asu-go/
├── cmd/asu/                  # Cobra 入口
├── internal/
│   ├── cli/                  # Cobra 子命令
│   ├── config/               # YAML 配置加载与校验
│   ├── api/                  # 兼容原版 ASU 的公开 API
│   ├── admin/                # 管理后台 API
│   ├── metadata/             # 上游元数据拉取与缓存
│   ├── queue/                # 任务队列
│   ├── runtime/              # 容器运行时抽象与驱动 (docker / podman / k8s 预留)
│   ├── stream/               # 日志广播与 RingBuffer
│   ├── store/                # 产物存储
│   └── db/                   # SQLite 与迁移
├── web/                      # Bun + Nuxt 项目（dist 嵌入二进制）
├── docs/                     # 设计文档 / adr / agents
├── configs/asu.example.yaml
├── AGENTS.md  GLOSSARY.md  Makefile  go.mod
└── .scratch/                 # 本地 Markdown issue tracker
```

---

## 详细技术文档

- [技术栈与工程约定 (Tech Stack)](docs/tech-stack.md)
- [系统架构设计 (Architecture)](docs/architecture.md)
- [API 兼容与扩展规范 (API Spec)](docs/api-spec.md)
- [容器执行引擎与实时可观测性 (Runner & Observability)](docs/runner-and-observability.md)
- [管理后台与任务运维规范 (Admin Dashboard)](docs/admin-dashboard.md)
