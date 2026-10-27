# 管理后台前端规格（二期，Nuxt）

本文档是 [admin-dashboard.md](admin-dashboard.md) 的**前端实现规格**：定义页面、组件、状态、交互以及每个页面依赖的 `/admin/api` 契约。面向实现者（含 AI 编码代理）。

- 技术栈固定：Bun + Nuxt 4（SPA，`ssr: false`，`baseURL: /admin/`）+ Nuxt UI v4（Tailwind v4）+ TypeScript。工程约定见 [tech-stack.md](tech-stack.md)。**只用 Bun**，不生成 `package-lock.json` / `yarn.lock` / `pnpm-lock.yaml`。
- 位置：`web/`。已有 `app/pages/{login,index}.vue`、`app/components/JobDetail.vue`、`app/composables/useAdminApi.ts`、`app/middleware/auth.global.ts`、`app/utils/jobs.ts`、`tests/`（vitest）。**先读这些再动手，沿用其风格与封装。**
- 界面语言：简体中文；代码标识符、路径保持原文。
- 受众：仅管理员，桌面浏览器优先，窄屏不崩即可（不做移动端精修）。支持明/暗主题（跟随系统，顶栏可切换）。
- 交付：`make build` 能产出嵌入二进制的 `web/dist`；`bun run lint`、`bun run test`（vitest）通过。

## 1. 现状与范围

已实现（一期）：登录页、任务列表+详情抽屉（`index.vue`、`JobDetail.vue`）。后端已有的管理 API 见 §6.1。

二期要做的页面（全部）：

| 路径 | 页面 | 后端依赖 |
| --- | --- | --- |
| `/login` | 登录（已有，保持） | 已有 |
| `/` | 仪表盘（概览 + 运行中任务） | 部分已有，见 §6 |
| `/jobs` | 任务历史（过滤、分页、重试/重放/取消） | 已有 + 小扩展 |
| `/jobs/[id]` | 任务详情 + 实时终端 | 日志已有；SSE 为新增 |
| `/storage` | 存储管理 | 新增 |
| `/build` | 手动构建器 | 复用公开 API + 管理扩展 |
| `/sources` | 来源目录（OpenWrt/ImmortalWrt 等） | 已有 |
| `/settings` | 只读：生效配置摘要与引擎状态 | 新增 |

后端中标记“新增”的接口**尚未存在**，实现前端时按 §6 契约先写 `composables` 与 mock（`tests/` 里用 fixture），不要自行改 Go 代码；后端由另一条 lane 落地。契约有任何不合理处，在 PR/汇报里提出而不是静默改字段。

## 2. 全局布局与导航

- 左侧可折叠侧栏（Nuxt UI `UDashboardGroup` / `UDashboardSidebar` 系列）：仪表盘、任务、存储、手动构建、来源、设置；底部显示当前用户、主题切换、登出。
- 顶栏：面包屑 + 全局“连接状态”徽标（SSE/轮询是否正常）+ 实例版本。
- 全站 401 → 清本地状态跳 `/login?redirect=<原路径>`；403（CSRF）提示“会话异常，请刷新”。
- 全局 toast 用于操作结果；危险操作（取消、删除、清理）一律二次确认对话框，文案写清影响范围。
- 空态、加载态（骨架屏）、错误态（含重试按钮与 `request_id` 展示，若响应头 `X-Request-Id` 存在）每个页面都要有。
- 所有 `fetch` 经 `useAdminApi` 封装：自动带 `X-Requested-With: asu-admin`、`credentials: same-origin`、统一错误对象。

## 3. 页面规格

### 3.1 仪表盘 `/`

- 顶部指标卡（4～6 张）：并发槽位 `running/max`、排队数、今日构建数与成功率、近 24h 失败数、磁盘占用（`used / limit`，无 limit 只显示 used）、引擎状态（类型、版本、socket 延迟、活跃容器数）。
- “运行中的构建”列表：每项显示设备 profile、`distribution version target`、客户端（User-Agent 解析为 `owut/1.2` 之类，解析不了显示原文截断）、已耗时（前端每秒本地计时）、当前阶段进度条、按钮 [打开控制台] [终止]。
- 阶段进度由事件中的 `stage` 映射：`init`→`container_setup`→`manifest`→`build`→`collect`→`done`（以后端实际 `##asu:stage=` 名称为准，前端维护一份“名称→中文文案→进度百分比”映射表，未知阶段显示原名、进度保持不变）。
- “排队中”列表：位置、request_hash 短码（前 8 位）、等待时长。
- “近期完成”：最近 10 条，状态徽标（成功/失败/超时/取消），点击进详情。
- 数据来源：首屏 `GET /admin/api/overview`，之后订阅全局事件流 `GET /admin/api/events`（SSE），断线自动指数退避重连（1s→30s），重连成功后重新拉取 `overview`。
- 热门数据（可选，放底部折叠区）：热门 profile / 包 Top 10，来自 `/admin/api/overview` 的 `stats`。

### 3.2 任务历史 `/jobs`

- 表格列：状态、request_hash（短码，点击复制完整值）、distro/version/target/profile、客户端、排队时长、构建耗时、创建时间、操作。
- 过滤：状态多选（排队/构建中/成功/失败/超时/取消）、发行版、版本、关键字（hash 前缀或 profile）、时间范围；过滤条件同步到 URL query，可分享、刷新保留。
- 分页：游标或页码，以后端契约为准（§6.2）；默认每页 25，排序按创建时间倒序，可切换。
- 行操作：查看详情、重试（仅失败/超时：重新提交同一请求，等价于对该请求体再 POST；成功后跳到新任务）、诊断重放（`replay`，弹窗可选“保留容器”）、取消（仅排队/构建中）、删除产物（仅成功）。
- 批量操作：勾选后“删除产物”，二次确认列出数量与预计释放空间。
- 列表对运行中任务自动刷新（订阅 §3.1 的事件流，更新行；失败则 5s 轮询兜底）。

### 3.3 任务详情与实时终端 `/jobs/[id]`

- 头部：状态徽标、profile、版本、耗时、客户端、操作按钮（取消/重试/重放/下载产物）。重放任务（id 形如 `rp_...`）显示“来自 <原 hash>”的链接与 `purge_at` 倒计时。
- 阶段时间线：横向步骤条，每步显示起止时间和耗时，当前步骤动画；失败时标红出错阶段。
- 终端（核心组件 `LogTerminal.vue`）：
  - 数据：运行中任务连 `GET /admin/api/jobs/{id}/stream`（SSE，§6.3）；已结束任务一次性取 `GET /admin/api/jobs/{id}/log`（纯文本）。
  - 渲染：等宽字体，ANSI 16 色 + 粗体 + 下划线；`##asu:stage=` 行不显示为普通日志，而是渲染成分隔条并驱动时间线。长日志使用虚拟滚动（目标 10 万行不卡）；单行超长折行可切换。
  - 跟随：默认自动滚动到底；用户上滚则暂停并出现“回到底部/恢复跟随”浮标，滚回底部自动恢复。
  - 工具栏：搜索（高亮并上下跳转匹配，Ctrl/⌘+F 聚焦）、换行开关、字号、复制全部、下载 `.log`、时间戳显示开关（若后端行带时间戳）。
  - SSE 细节：用原生 `EventSource` 无法带自定义头，但同源 cookie 认证可用；若需要 Bearer 则用 `fetch` + ReadableStream 解析。断线重连带 `Last-Event-ID`（=已收字节偏移或行号，见 §6.3），去重不重复追加。
- 侧栏/标签页：
  - “请求”：原始请求 JSON（可折叠、复制）。
  - “包”：默认包 / 用户新增 / 剔除 / 历史改名修正后的最终命令包（`build_cmd_packages`）。
  - “产物”：文件名、大小、SHA-256、下载链接（经 `/store/...`）；失败任务无此标签。
  - “元数据”：request_hash、image、容器 handle（仅保留容器的重放任务）、时间点、重试次数、`replay_of`。

### 3.4 存储管理 `/storage`

- 概览卡：产物总数、总占用、`data_dir` 剩余空间、孤儿目录数与大小、最近一次清理时间与结果。
- TTL 策略：只读展示当前生效的 TTL（defaults / versioned / 其他 / 失败 / purge_at），来自配置；在二期不提供在线修改，卡片内注明“修改请编辑配置文件并重启”。
- 产物表：按版本/发行版/大小/过期时间/状态排序过滤，勾选删除，单行展开看文件清单。
- 操作按钮：[立即清理过期] [清理失败残留] [清理孤儿]，均先“预演”（dry-run）弹窗显示将删除的条目数与字节数，确认后执行并显示结果。
- 镜像缓存（只读，一期未自动清理）：列出本机已拉取的 ImageBuilder 镜像（名称、大小、最近使用）；二期提供“删除镜像”操作需二次确认，**仅当后端契约提供 §6.4 的镜像接口时才渲染该卡片**，否则隐藏。

### 3.5 手动构建器 `/build`

- 目的：管理员在线验证构建，无需 LuCI/owut。提交的就是公开 `POST /api/v1/build`（经 `/admin/api/build` 代理或直接调公开 API，见 §6.5），请求体与公开协议完全一致。
- 表单分步卡片：
  1. 发行版 → 分支/版本 → target → profile：级联选择，数据取公开元数据 `/{distro}/json/v1/...`（前缀规则见 api-spec 的“多发行版与路径前缀”）；profile 支持搜索（型号、id）。
  2. 软件包：标签输入框，输入时对 `packages` 元数据做补全；支持“`-pkg` 剔除”；显示该 profile 的默认包（灰色标签，可点击剔除）。
  3. 高级：`diff_packages`、`filesystem`、`rootfs_size_mb`、`defaults`（uci-defaults 文本框）、自定义仓库（名称+URL）、仓库公钥（多行）、`repositories_mode`。
  4. 预览：实时生成并展示请求 JSON（可复制、可从 JSON 反向导入表单），并显示将得到的 `request_hash`（若后端提供 `POST /admin/api/build/preview`，否则省略）。
- 提交后：直接跳转 `/jobs/[hash]` 并打开终端；若命中缓存（200 已完成）显示“命中缓存”提示并给出产物链接和“强制重建”（仅对失败作业有效，成功作业命中即复用，不提供强制）。
- 表单草稿存 `localStorage`，可导出/导入 JSON。

### 3.6 来源目录 `/sources`

- 表格：发行版、版本/分支、启用开关、镜像模板、上游 URL、是否滚动版本、是否被 override、最近同步状态。
- 操作：启用/停用（PATCH）、编辑（抽屉表单：镜像基名、tag 模板、上游 URL、overrides）、新增、删除（二次确认）、导入（粘贴 JSON/YAML，预览差异后确认）、[立即同步元数据]（若后端提供）。
- 表单校验：tag 模板仅允许已知占位符（`{target}` `{version}` `{version_tag}`），不合法实时提示。保存前提示“影响新构建，不影响运行中任务”。
- 保留词提示：发行版标识不得为 `admin`、`api`、`json`、`store`、`healthz` 等保留词（后端会拒绝，前端也提前提示）。

### 3.7 设置/系统 `/settings`（只读）

- 引擎：类型、socket、版本、延迟、容器数、网络名；[重新探测] 按钮。
- 生效配置摘要：listen、data_dir、并发数、队列/存储/统计驱动、日志级别/格式/文件、default_distro、CORS 来源、TTL。**不显示任何 secret/token/哈希**。
- 管理会话：当前用户、会话过期时间、登出。
- 版本信息：二进制版本、构建时间、Go 版本。

## 4. 组件清单

| 组件 | 说明 |
| --- | --- |
| `StatusBadge` | 任务状态徽标（颜色/图标统一）；一处映射，全站复用 |
| `StageTimeline` | 阶段时间线 |
| `LogTerminal` | §3.3 终端，纯展示 + 事件输入，不直接发请求 |
| `ConfirmDialog` | 危险操作确认，支持列出影响条目 |
| `HashCell` | 短码 + 复制 |
| `DurationText` | 本地计时/格式化 |
| `PackageInput` | 包标签输入 + 补全 |
| `RequestPreview` | 请求 JSON 预览/导入 |
| `EmptyState` / `ErrorState` | 统一空态与错误态 |

composables：`useAdminApi`（已有）、`useEventStream`（SSE，含重连/去重/可见性暂停）、`useJobs`、`useOverview`、`useStorage`、`useSources`、`useBuilder`。纯逻辑（阶段映射、ANSI 解析、过滤→query 序列化、请求体生成）放 `app/utils/` 并写 vitest 单测。

## 5. 非功能要求

- 性能：首屏 JS 尽量小，页面级懒加载；终端 10 万行不卡；SSE 在标签页隐藏时暂停渲染、可见后补齐。
- 可访问性：键盘可达，焦点环可见，对话框焦点陷阱，状态不只靠颜色表达。
- 安全：不用 `v-html` 渲染日志（ANSI 自行转义成安全 span）；不把 token 放 URL；不 `console.log` 响应体。
- 测试：utils 单测（阶段映射、ANSI、query 序列化、请求体）；composable 用 mock fetch；至少一个组件测试覆盖 `StatusBadge` 与 `LogTerminal` 的跟随逻辑。
- 与后端同源：开发时 `make dev` 经 Nuxt dev server 代理到 Go `:8000`，不要在代码里写死主机。

## 6. 后端接口契约

通用：路径前缀 `/admin/api`；JSON；时间 RFC3339 UTC；错误体 `{"detail": "<中文或英文说明>"}`，状态码语义同 HTTP；cookie 认证写操作需头 `X-Requested-With: asu-admin`。

### 6.1 已有（以实际实现为准，先阅读 `internal/admin/*.go` 与 `web/app/utils/jobs.ts`）

`POST /login` `POST /logout` `GET /me`；`GET /jobs`、`GET /jobs/{id}`、`POST /jobs/{id}/cancel`、`POST /jobs/{id}/replay`、`POST /replay`、`GET /jobs/{id}/log`；`DELETE /store/{hash}`；`GET/POST /sources`、`GET/PATCH/DELETE /sources/{id}`、`POST /sources/import`。公开 `GET /api/v1/stats` 可复用。

### 6.2 任务列表扩展（新增，兼容现有）

`GET /jobs?status=a,b&distro=&version=&q=<hash前缀|profile>&from=&to=&limit=25&cursor=<opaque>&order=desc`
→ `{"items":[Job...],"next_cursor":"..."|null,"total":N}`。`Job` 在现有字段上补：`client`（User-Agent）、`queued_ms`、`duration_ms`、`stage`、`artifact_bytes`、`replay_of`、`purge_at`。

### 6.3 事件流（新增）

- `GET /events`：全局 SSE，事件类型 `job.created` `job.updated`（含 `id`,`status`,`stage`,`position`）`job.finished` `overview`（槽位/排队变化）；心跳注释行每 15s。
- `GET /jobs/{id}/stream`：单任务 SSE，事件 `log`（`data` 为一段日志文本，`id:` 为累计字节偏移）、`stage`（`{"name":"...","at":"..."}`）、`status`（终态）；结束后发送 `event: end` 并关闭。支持 `Last-Event-ID` 续传。
- 响应头 `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`X-Accel-Buffering: no`。

### 6.4 概览与存储（新增）

- `GET /overview` → `{"slots":{"running":2,"max":4},"queued":1,"active":[Job...],"queue":[Job...],"recent":[Job...],"today":{"total":48,"success":47,"failed":1},"disk":{"used_bytes":..,"limit_bytes":null,"free_bytes":..},"engine":{"type":"podman","version":"5.8","socket":"...","latency_ms":3,"containers":2},"stats":{"top_profiles":[],"top_packages":[]}}`
- `GET /storage` → `{"artifacts":{"count":..,"bytes":..},"orphans":{"count":..,"bytes":..},"ttl":{...},"last_clean":{"at":"...","removed":N,"freed_bytes":N}}`；`GET /storage/artifacts?...` 产物分页表。
- `POST /storage/clean` body `{"scope":"expired|failed|orphans","dry_run":true}` → `{"items":N,"bytes":N}`；`dry_run:false` 才真正执行。
- 镜像（可选）：`GET /images`、`DELETE /images/{name}`。

### 6.5 构建器辅助（新增，可选）

- `POST /build/preview` body 同公开构建请求 → `{"request_hash":"...","normalized":{...}}`（不入队）。
- 提交直接调用公开 `POST /{distro?}/api/v1/build`（同源，无需额外代理）。

### 6.6 设置（新增）

`GET /settings` → 只读生效配置摘要（§3.7 范围，无 secret）；`POST /engine/probe` → 引擎状态。

## 7. 验收清单

- [ ] 8 个页面均可从侧栏到达，刷新直达深链不 404（SPA fallback 已由后端处理）。
- [ ] 终端：实时追加、上滚暂停/回底恢复、搜索、下载、ANSI 色、10 万行不卡（用 mock 流验证）。
- [ ] SSE 断线重连无重复无丢行（mock 验证）。
- [ ] 危险操作均有二次确认；批量清理有 dry-run 预览。
- [ ] 过滤/分页状态在 URL 中，刷新保持。
- [ ] 无 secret 泄漏；日志渲染无 XSS（有测试）。
- [ ] `bun run lint`、`bun run test`、`make build` 通过；没有新增 lockfile。
