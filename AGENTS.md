# AGENTS.md

ASU-Go：用 Go 重写的 OpenWrt Attended Sysupgrade 服务端，单二进制，内置 Web 管理后台与实时构建日志。一期（见 `.scratch/README.md`）已实现并合并，`docs/` 与各包 `README.md` 是事实来源；真实 ImageBuilder 的 owut/LuCI 手工冒烟与 Docker 真机集成测试尚待人工。

## 必读文档

- 架构：`docs/architecture.md`
- 技术栈与工程约定（选型、目录、配置、运行时抽象）：`docs/tech-stack.md` —— **动手前先读**
- API 兼容：`docs/api-spec.md`
- 数据模型（SQLite 表与诊断重放）：`docs/data-model.md`
- 容器执行与可观测性：`docs/runner-and-observability.md`
- 管理后台：`docs/admin-dashboard.md`
- 术语：`GLOSSARY.md`；决策记录：`docs/adr/`

外部参考实现（Python 原版 ASU）：`/data/app-stacks/asu`。兼容性问题以其行为为准，不要凭记忆猜测。

## 技术栈速览

- 后端：Go + **Gin**（HTTP/SSE）+ **Cobra**（CLI）+ YAML 配置（`yaml.v3` 严格解码，**不用 Viper**，键名**全小写 snake_case**，字段必须显式 `yaml` 标签）+ **Zap** 日志（`log.format`: `json`/`text`/`plain`） + SQLite（`modernc.org/sqlite`，免 CGO）
- 容器：`internal/runtime.Runtime` 接口；Docker / Podman 首发（Moby client，Podman 走兼容 socket）；Kubernetes 仅预留接口，不要提前实现
- 前端：**Bun + Nuxt 4（SPA，`ssr: false`）+ Nuxt UI v4**，位于 `web/`，`nuxt generate` 产物经 `go:embed` 嵌入二进制，挂载于 `/admin`
- 单仓库、单 `go.mod`；业务代码全部在 `internal/`

## 常用命令

统一通过 `Makefile`：

```bash
make dev      # Go 服务 + Nuxt dev server（代理到 Go :8000）
make build    # bun run generate → go build，产出单一二进制
make test     # go test ./... ；前端 vitest
make lint     # golangci-lint ；前端 eslint
```

前端只用 Bun（`bun install` / `bun run ...`），不要生成 `package-lock.json` / `yarn.lock` / `pnpm-lock.yaml`。

## 代码约定

- Gin handler 只做绑定/调用/序列化，业务逻辑不得依赖 `*gin.Context`。
- 构建命令作为容器主进程运行，用 `logs?follow=1` 流式读取，不用 exec/Hijack、不定时轮询。
- 调度层、队列层不得出现 Docker 专有概念（容器 ID、Hijack、多路复用帧）；一律走 `Runtime` 接口与不透明 `Handle`。
- 所有后台 goroutine 由 `serve` 持有的 `errgroup` 管理，用 `context` 取消；关闭时必须清理在途构建的容器。
- 对外 API 必须保持与原版 ASU 协议兼容；扩展字段只增不改，管理接口放 `/admin/api`。
- 新增第三方依赖前先确认标准库/现有依赖无法满足；选型变更先写 ADR。
- 默认输出与文档使用简体中文；代码标识符、命令、路径保持原文。
- 注释以行内（独立行）注释为主，少用行尾注释。

## 验证

- 单元测试用 fake `Runtime`；真实 Docker/Podman 测试加 build tag `integration`，默认不跑。
- 修改配置结构体须同步 `configs/asu.example.yaml`，且保持能被严格解码。
- 涉及兼容 API 的改动，对照原版 ASU 的请求哈希与响应格式验证。

## Agent skills

### Issue tracker

Issue 与 spec 以本地 Markdown 存放于 `.scratch/<feature-slug>/`（`spec.md` + `issues/NN-slug.md`）。See `docs/agents/issue-tracker.md`.

### Triage labels

使用默认五个标签：`needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`，记录在 issue 文件的 `Status:` 行。See `docs/agents/triage-labels.md`.

### Domain docs

single-context：根目录 `GLOSSARY.md` + `docs/adr/`。See `docs/agents/domain.md`.
