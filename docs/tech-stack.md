# 技术栈与工程约定 (Tech Stack)

本文档是 ASU-Go 技术选型的唯一事实来源。选型变更请先写 ADR（`docs/adr/`），再改本文档。

---

## 1. 总览

| 层 | 选型 | 说明 |
| --- | --- | --- |
| 语言 | Go（版本以 `go.mod` 为准） | 单模块，根目录 `go.mod` |
| HTTP 框架 | **Gin** | 路由、中间件、SSE 流式响应 |
| CLI | **Cobra** | 子命令：`serve` / `config check` / `version` / `migrate` 等 |
| 配置 | **YAML**（`gopkg.in/yaml.v3` 严格解码，键名全小写 snake_case） | **不引入 Viper**，见 §3 |
| 日志 | **Zap**（`go.uber.org/zap`） | 结构化日志；`log.format` 可选 `json` / `text` / `plain`（易读彩色 label=value，兼容 `console`） |
| 数据库 | SQLite（`modernc.org/sqlite`，纯 Go，免 CGO） | `database/sql` + 内嵌迁移（goose） |
| 容器运行时 | `Runtime` 接口 + Docker/Podman 驱动 | 未来可加 Kubernetes 驱动，见 §4 |
| 对象存储 | 本地 FS（默认）/ S3 兼容 | 经 `store` 接口抽象 |
| 前端 | **Bun + Nuxt 4（SPA 模式）** | `nuxt generate` 产物嵌入 Go 二进制 |
| 前端组件库 | **Nuxt UI v4**（Tailwind CSS v4） | 见 §5 |
| 测试 | `testing` + `testify`；前端 `vitest` | |
| Lint | `golangci-lint`；前端 `eslint`（Nuxt 官方配置） | |
| 任务入口 | `Makefile` | 统一 `make dev / build / test / lint` |

---

## 2. 仓库布局

单仓库、单 Go module，前端作为 `web/` 子项目（独立 `package.json`，由 Bun 管理）。

```text
asu-go/
├── cmd/asu/                # Cobra 入口（main.go 仅组装，不含业务）
├── internal/
│   ├── cli/                # Cobra 子命令实现
│   ├── config/             # YAML 加载 / 默认值 / 校验
│   ├── api/                # 兼容原版 ASU 的公开 API
│   ├── admin/              # 管理后台 API（/admin/api）
│   ├── metadata/           # 上游元数据
│   ├── queue/              # 任务队列
│   ├── runtime/            # 容器运行时抽象 + docker / podman / (k8s) 驱动
│   ├── stream/             # 日志广播与 RingBuffer
│   ├── store/              # 产物存储
│   └── db/                 # SQLite 访问与迁移
├── web/                    # Nuxt 项目
│   ├── app/                # pages / components / composables
│   ├── dist/               # nuxt generate 产物（git ignore，由 embed.go 嵌入）
│   └── embed.go
├── docs/                   # 设计文档、ADR、agent 配置
├── configs/asu.example.yaml
├── Makefile
└── GLOSSARY.md
```

约定：
- 业务代码一律放 `internal/`，不对外暴露包。
- `cmd/asu/main.go` 只做依赖组装并调用 `internal/cli`。
- 包之间依赖方向：`cli → api/admin → queue/metadata/store → runtime/db`，禁止反向依赖。

---

## 3. CLI 与配置

### 3.1 CLI（Cobra）

```text
asu serve [--config asu.yaml]   # 启动服务
asu source list|add|edit|enable|export|import   # 经 /admin/api 操作（需 admin.api_token）
asu replay <hash>|-f request.json   # 诊断重放，经 /admin/api
asu db migrate                  # 仅本地，要求服务未运行
asu config check                # 校验配置并打印生效配置
asu version
```

全局 flag：`--config`（默认依次查找 `./asu.yaml`、`./asu.yml`、`/etc/asu/asu.yaml`、`/etc/asu/asu.yml`）、`--log-level`。

### 3.2 配置（YAML，不用 Viper）

不引入 Viper 的原因：配置结构简单且固定；Viper 依赖树偏重、隐式合并规则易出意外。改用：

0. **键名规范**：所有 YAML 键一律**全小写 snake_case**（如 `job_timeout`、`max_concurrent_builds`、`build_ttl`），不出现大写或驼峰。每个结构体字段必须显式写 `yaml:"snake_case"` 标签（不依赖 yaml.v3 的默认小写化，否则多词字段会变成 `jobtimeout`）；测试会反射检查所有标签均匹配 `^[a-z][a-z0-9_]*$`。
1. `yaml.v3` 解码到强类型 `Config` 结构体，开启 `KnownFields(true)`，拼错的键直接报错。
2. 优先级：**命令行 flag > 环境变量 `ASU_*` > YAML 文件 > 内置默认值**。环境变量仅覆盖少量常用项（监听地址、日志级别、数据目录），在 `internal/config` 内显式映射，不做通用反射。
3. `Validate()` 集中校验，错误信息带 YAML 路径（如 `runtime.socket`）。
4. 示例配置位于 `configs/asu.example.yaml`，必须与结构体同步（有测试保证能被严格解码）。

`server.cors.public_origins` 默认 `["*"]`，为 `/api/`、`/json/`、`/store/` 提供无凭据跨域访问；可改为精确来源列表或 `[]` 关闭。`server.cors.admin_origins` 默认 `[]`（同源），仅允许精确来源且启用凭据，禁止通配符；不影响 `/admin` 静态页面。

如果将来出现多层配置合并、热加载等复杂需求，再用 ADR 评估 `koanf`（优先于 Viper）。

---

## 4. 容器运行时抽象

当前支持 Docker / Podman，未来可能支持 Kubernetes。因此 `internal/runtime` 定义与具体引擎无关的接口，**不能**让 Docker 概念（容器 ID、Hijack 连接）泄漏到调度与队列层。

```go
// Runtime 抽象一次“隔离构建执行”的生命周期。
type Runtime interface {
    // Prepare 确保构建镜像可用（拉取 / 校验）。
    Prepare(ctx context.Context, spec ImageSpec) error
    // InspectImage 读取镜像默认工作目录与用户，供构建目录与归档属主推断。
    InspectImage(ctx context.Context, image string) (ImageMetadata, error)
    // Create 分配环境，返回可持久化的不透明句柄。
    Create(ctx context.Context, spec RunSpec) (Handle, error)
    // PutArchive 在启动前注入 tar 文件流，无宿主路径依赖。
    PutArchive(ctx context.Context, h Handle, path string, archive io.Reader) error
    // Start 启动主进程。
    Start(ctx context.Context, h Handle) error
    // Logs 流式读取已解复用的标准输出与错误流（follow 长连接，非轮询、非 Hijack），直至结束或 ctx 取消。
    Logs(ctx context.Context, h Handle, opts LogsOptions) (io.ReadCloser, error)
    // Wait 阻塞到结束并返回退出信息。
    Wait(ctx context.Context, h Handle) (ExitStatus, error)
    // GetArchive 即使主进程已经退出也能取回目录 tar 流。
    GetArchive(ctx context.Context, h Handle, path string) (io.ReadCloser, error)
    // Kill 强制终止但保留日志与产物（幂等）。
    Kill(ctx context.Context, h Handle) error
    // Remove 最终清理底层资源（幂等）。
    Remove(ctx context.Context, h Handle) error
    // Inspect 返回存在性、运行状态、标签与创建/退出时间；不存在不是错误。
    Inspect(ctx context.Context, h Handle) (State, error)
    // List 只列举 managed-by=asu-go 的环境，包含已退出环境。
    List(ctx context.Context) ([]State, error)
    // Info 返回引擎名称、版本等，用于管理后台展示。
    Info(ctx context.Context) (Info, error)
    Close() error
}
```

`LogsOptions` 包含 `Follow`、`Since`（时间戳字符串）、`Timestamps`。`runtime.Open(driver, Options)` 通过注册表创建驱动；组装层将 `config.Runtime` 的 socket、memory_limit、cpus、pull_policy 映射至 `Options`。`Prepare` / `Info` 执行引擎版本及日志可读性检查；Podman 兼容 `/info` 缺失默认日志驱动时，仅此处查询原生只读 `/libpod/info`。

驱动：

| 驱动 | 实现方式 | 状态 |
| --- | --- | --- |
| `docker` | 官方 Moby Go client（`github.com/moby/moby/client`）连接 `docker.sock` | 首发 |
| `podman` | 复用同一 client，连接 Podman 的 Docker 兼容 API socket（`podman.sock`）；仅在兼容层不足时加差异分支 | 首发 |
| `kubernetes` | 以 Job/Pod 实现，`client-go` | 预留，仅保留接口，不写代码 |

K8S 预留要点（现在就遵守，避免日后返工）：
- 构建输入输出通过 `RunSpec` 的“挂载 / 产物回收”抽象表达，不假设宿主路径一定存在。
- `Handle` 是不透明值，由驱动序列化到 DB，重启后可用于孤儿回收。
- 日志统一为 `io.Reader` 语义，不暴露 Docker 多路复用帧格式。
- 构建命令作为容器主进程运行（而非 `exec`），Docker 用 `logs?follow=1`、K8S 用 `pods/log?follow=true`，语义一致，不需要 Hijack。

---

### 4.1 驱动模型与一期范围

`queue` / `store` / `stats` 采用 Laravel 式驱动切换（配置 `xxx.driver`）。一期：数据库只有 SQLite，`queue.driver=sqlite`、`store.driver=local`、`stats.driver=sqlite`；`redis`、`s3` 只留接口，配置后启动报错。

### 4.2 发行版 / source 目录

公开兼容 API 使用发行版路径前缀镜像：`/{distro}/api/`、`/{distro}/json/`、`/{distro}/store/`；无前缀使用 `server.default_distro`（默认 `openwrt`，启动时校验已启用）。元数据服务显式接收 distro；Gin 以独立 `/:distro` 路由组镜像公开接口，逐请求校验启用状态，在 CORS 之前拒绝未知前缀。`admin/api/json/store/healthz/stats` 不可作发行版标识；管理 API 不镜像。公开 CORS 同时覆盖前缀路径。客户端配置见 [api-spec.md](api-spec.md#多发行版与路径前缀)。

运行时以 SQLite 为唯一事实来源，内嵌默认目录（OpenWrt、ImmortalWrt）首次启动写入；上游同步新增版本，不覆盖被本地修改的字段；YAML 仅用于 `asu source export/import`。source 的核心字段：`distribution`、`version`、`upstream_url`、`image`（镜像基名）、`tag_template`、`package_manager`、`origin`（`upstream`/`local`）、`rolling`、`enabled`。一期镜像只认容器镜像，ImageBuilder 压缩包自建镜像不做。

### 4.3 元数据同步

`.versions.json` / `.targets.json` 按 `metadata.refresh_interval`（默认 1h）后台刷新；`profiles.json` 与包索引惰性拉取，按 `metadata.cache_ttl`（默认 1h）复用磁盘缓存 `data/metadata/`，过期执行 ETag / `If-Modified-Since` 条件请求；每份缓存配 `.meta.json` 边车；上游失败返回过期缓存并告警；启动不强制同步成功。

### 4.5 数据模型与诊断

表结构见 [data-model.md](data-model.md)。配置项 `diagnostics.retention`（默认 7d）、`diagnostics.keep_container_ttl`（默认 1h）、`diagnostics.max_kept`（默认 3）。

### 4.4 构建并发与镜像

`queue.max_pending_jobs` 默认 200（仅计 queued，达到上限拒绝新任务，重复请求仍复用已有状态）。公开缓存期限由 `queue.build_ttl`（带 packages_versions，168h）、`queue.build_unversioned_ttl`（其余，24h）、`queue.build_defaults_ttl`（带 defaults 优先，30m）、`queue.build_failure_ttl`（失败，1h）控制；入队预写、完成后重新起算，管理保留期限使用 `diagnostics.retention`。`build` 段提供 `allow_defaults`、`max_defaults_length`（20480）、`max_custom_rootfs_size_mb`（1024）、`repository_allow_list`，默认不允许 defaults 与自定义仓库。

`queue.max_concurrent_builds` 默认 2，`job_timeout` 默认 10m，`runtime.memory_limit` / `runtime.cpus` 默认不限，`runtime.pull_policy: missing|always|never` 默认 `missing`。`rolling` source 按 `runtime.rolling_recheck`（默认 6h）强制 always 复查，检查时间保存在 Runner 实例内存，重启后首次使用重新检查。`runtime.scavenger_interval` 默认 5m，启动立即执行一次回收；活跃任务不回收，非活跃诊断环境按保留 TTL/数量清理。一期不自动清理镜像（磁盘风险：每个 target 一份，GB 级），二期在后台存储页手动清理。

`cleaner.interval` 默认 10m，启动立即清理一次；`cleaner.orphan_grace` 默认 1h，仅回收无 jobs 记录的公开 hash 目录。普通产物按 expires_at，日志、任务记录和诊断产物按 purge_at 回收；活跃任务不回收。下载每次检查 jobs 的公开期限，不依赖定时清理。手动删除保留任务记录并标记 `result.artifacts_removed=true`，公开轮询/下载按过期返回 404。

`runner.verbose` 默认 true，仅向 `make image` 添加 `V=s`（与任务显式 verbose 取或）。`runner.cache.enabled` 默认 true，`dir` 留空时延迟解析为 `{data_dir}/cache`，`max_size_gb` 默认 20、允许 0。缓存经 Runtime 的 put/get_archive 在任务私有 `WorkingDir/dl` 与共享包仓库之间复制，只保留非空普通 `.apk`/`.ipk`，索引从不跨任务共享。镜像引用去除 digest 并清洗为目录键；自定义 repositories 请求跳过缓存，失败构建不回收，缓存故障不阻断构建。Cleaner 共用 Cache 对象的进程内互斥，按播种时 touch 的文件 mtime LRU 淘汰；不支持多服务进程共享写同一目录。详见 [ADR 0005](adr/0005-package-cache.md) 与 [Runner 包缓存](runner-and-observability.md#36-跨任务包缓存)。

`runner.mirrors` 默认空列表，每项为强类型 `from` / `to` HTTP(S) URL 前缀，均要求以 `/` 结尾。在 setup 后、请求自定义仓库应用前、`make info` 前按顺序替换原始仓库 URL 前缀，明确保留用户自定义地址；不改变包缓存键，不影响 metadata 同步。USTC 示例见 `configs/asu.example.yaml`，安全边界与实测见 [Runner 仓库镜像](runner-and-observability.md#37-构建仓库镜像)。

---

## 5. 前端（`web/`）

- **Bun**：包管理与脚本运行（`bun install`、`bun run dev`），提交 `bun.lock`。
- **Nuxt 4，`ssr: false`**：纯 SPA，`nuxt generate` 输出静态文件到 `web/dist`，由 Go `//go:embed all:dist` 嵌入并挂载到 `/admin`。`app.baseURL` 设为 `/admin/`。
- **组件库：Nuxt UI v4**。理由：Nuxt 官方生态、开箱即用 Table / Badge / Modal / 表单 / Dashboard 布局，主题基于 Tailwind v4，管理后台所需组件基本齐全，无需再拼装多个库。若后续觉得过重，替代方案是 `shadcn-nuxt`（组件源码拷入项目，可控性更高）。
- **数据层**：原生 `$fetch` + `EventSource`（SSE）封装为 composables，暂不引入 Pinia / 额外状态库。
- **终端日志渲染**：`ansi_up` 将 ANSI 转 HTML，虚拟滚动处理长日志；暂不引入 xterm.js。
- **开发模式**：`bun run dev`（Nuxt :3000）通过 `nitro.devProxy` / `vite.server.proxy` 把 `/api`、`/admin/api`、`/json` 代理到 Go 服务（:8000），前后端热更新互不阻塞。
- **发布构建**：`make build` = `bun run generate` → `go build`，产物为单一二进制。

---

## 6. 后端实现约定

- **分层**：Gin handler 只做参数绑定、调用 service、序列化响应；业务逻辑不得依赖 `*gin.Context`。
- **日志**：全局使用 `*zap.Logger`（热路径可用 `SugaredLogger` 之外的强类型字段）；通过 `log.format`（`json` | `text` / `plain`，兼容 `console`）与 `log.level` 配置，`text` 面向终端与易读（输出 `label=value` 形式，避免大段 JSON 噪声），`json` 面向生产采集。字段键名同样使用 snake_case，并固定携带 `request_hash`、`component` 等上下文字段。构建日志流（`stream` 包）与应用日志是两回事，不混用。
- **应用日志级别与关联**：debug 用于引擎 API 调用、缓存命中、空转扫描；info 用于入队/出队、构建阶段及生命周期变化；warn 用于认证失败与可恢复清理/缓存异常；error 用于构建或执行失败。HTTP 日志包含 `method`、不含查询串的 `path`、模板 `route`、`status`、`latency_ms`、`client_ip`、`bytes`、`user_agent`、`request_id`；5xx/error、4xx/warn、其余/info，成功的健康检查及前端资源/debug。`X-Request-Id` 沿用或生成并回写，业务层通过标准 `context.Context` 获取；panic 同样关联此 ID。禁止记录 Authorization、Cookie、密码、token、请求体或生效配置中的秘密；查询串整体省略（包括 token 参数），不只遮挡某几个参数。SSE 在连接结束时记录。
- **请求错误与堆栈**：handler 用 `c.Error(err)` 附加原因，HTTP 摘要通过 `zap.Error` 写入 `error`；构建失败查询附带 `request_hash` 与最多 512 字符的 `detail` 摘要，不复制 stderr 或完整构建输出。普通 4xx/5xx 不自动打印堆栈；Zap 自动堆栈阈值为 DPanic，panic 的 recover 日志仍显式保留堆栈。
- **应用日志落盘**：`log.file` 默认 `""`，仅输出 stdout；设为文件路径后同时写 stdout 与文件（追加、权限 0600，自动创建父目录）。`text`/`plain` 终端保持彩色，文件副本不带 ANSI 颜色码且均为 `label=value`。仅启动时检查大小：已有文件 **大于 50MiB** 则重命名为 `<file>.1`，覆盖旧备份；运行中不轮转、不启动额外 goroutine。相对路径基于进程工作目录，打开/创建失败会报 `log.file` 启动错误。构建日志仍独立由 stream/TTL 管理。例如：

  ```yaml
  log:
    format: text
    level: info
    file: /tmp/opencode/asu.log
  ```
- **错误**：向上返回带 `%w` 的 error；对外 API 在统一中间件里映射为 ASU 兼容的错误体。
- **并发**：所有后台 goroutine 由 `serve` 命令持有的 `errgroup` 管理；使用 `context` 传播取消，优雅关闭须等待在途构建的容器被清理。
- **SSE**：使用 Gin 的 `c.Stream` / `http.Flusher`，每连接订阅 `stream` 广播器；客户端断开即退订，不得泄漏 goroutine。
- **SQLite**：开启 WAL；写操作串行化到单连接或使用 `busy_timeout`；迁移文件用 `embed` 内嵌，启动时自动执行。
- **测试**：运行时驱动用接口 + fake 实现做单元测试；真实 Docker/Podman 的集成测试用 build tag `integration`，默认不跑。
- **依赖克制**：新增第三方依赖前确认标准库或现有依赖无法满足。
