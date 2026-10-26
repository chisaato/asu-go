# 上游元数据

`Syncer.Run` 由 `serve` 的 errgroup 持有，启动后异步立即同步，再按 `metadata.refresh_interval` 执行。失败只告警，不终止 HTTP 服务；取消 context 会中止在途 HTTP 请求。`kv[metadata.last_sync]` 保存最近一轮结束时间 `at` 与失败计数 `failures`，不是“最近一次全量成功时间”。

`Client.Fetch` 提供带超时、User-Agent、ETag 和 Last-Modified 的通用条件请求；`Client.Get` 为版本同步保留进程内验证器。`Cache` 使用 `Fetch` 实现 profiles 与包索引的持久缓存。

## 惰性缓存与构建校验接口

- `Service.Cache = &Cache{Dir: dataDir, TTL: ttl, Client: client, Logger: logger}`。`serve` 使用 `metadata.cache_ttl`（默认 1h，必须为正），不在启动时预拉 profiles/包索引。
- 文件保存在 `{data_dir}/metadata/{sha256(upstream_url)}/{version_path}/...`；上游去掉尾部 `/`，相同规范路径的不同镜像相互隔离。每个文件的 `.meta.json` 保存 `etag`、`last_modified`、`fetched_at`，额外保存正文 `sha256`，防止中断写入导致验证器与正文错配。正文与边车分别临时文件加 rename 写入，错配时当作未缓存。
- TTL 内直接读磁盘；过期携带条件头，304 刷新时间并复用正文。HTTP/网络失败或非法 JSON 使用已有成功正文并告警；没有缓存时遵循端点原版行为。缓存写入失败告警但仍返回本次上游成功数据。缓存文件不入 SQLite；当前没有自动淘汰。
- 同一 Cache 中同一上游/路径通过 singleflight 合并请求；等待者可通过自身 context 取消等待。共享请求使用首个调用者的 context（首个调用者取消时，该批等待者都可能失败，可重试）。生产 Client 自带 30s 超时，无自管后台刷新 goroutine。
- 路径仅允许规范非空段及 ASCII 字母、数字、`._+-`，拒绝 `.`、`..`、反斜杠、百分号和 URL 查询/片段；上游地址只能来自 source 目录。上游 kernel/feed 数据派生的路径也执行相同检查。

供 build-api/02 依赖的 `metadata.ProfileProvider`（`*Service` 实现）：

```go
Profile(ctx context.Context, distro, version, target, profile string) (map[string]any, error)
Targets(ctx context.Context, distro, version string) (map[string]any, error)
Packages(ctx context.Context, distro, version, target string) (map[string]string, error)
```

`target` 为 `target/subtarget`。`Profile` 返回原版公开端点结构：顶层元数据（移除 `profiles`）+ 所选 profile + `id` + UTC 微秒精度 `build_at`，不存在返回 `*ProfileNotFoundError`（公开响应 404，仅 `detail`）。不会将 supported_devices 别名当作 profile ID；构建请求如需别名解析应自行读取/设计额外接口。`Targets` 返回已同步的 source targets；`Packages` 合并 target 索引/kmods 与 architecture 对应 feeds 包，包名去 ABI（v1 且 Packages 不可用时保留原版 v1 名称）。返回值每次新解码/读取，不共享可变映射。

source 优先匹配启用的 distro/version，Profile/Packages 无精确版本时允许同一启用分支的镜像配置（与 revision 的分支匹配思路相同）；`Targets` 必须命中精确启用版本，不返回其他版本的 targets。构建合法性校验可先用 `Targets` 校验目录版本与 target；不要单靠 Profile/Packages 请求是否成功判断版本是否已入目录。未知分支返回 400。

`GET /json/v1/*path` 统一分派 overview/latest/branches 和三类惰性端点，避免 Gin 静态与尾部通配冲突。支持 `releases/23.05.5/targets/x86/64/index.json`、`releases/23.05.5/packages/x86_64-index.json` 等多段路径。

## 包索引兼容规则

优先 `index.json`，`version >= 2` 移除 `version` 后直接返回；否则取 RFC822 `Packages`，第一个非 `all` 架构、`Package` 后缀去掉 `ABIVersion`。Packages HTTP 失败保留 v1（索引也不存在则 `{}`），网络异常无缓存为 500。架构索引按 `feeds.conf` 每行第二字段依次合并 feed，同名后者覆盖前者；feeds.conf HTTP 失败返回 `{}`，不使用文件中的远端 URL。

拆分 kmod 按 `profiles.json.linux_kernel` 的 `version-release-vermagic` 合并。`PostKmodSplit` **刻意保留 Python 的版本字符串比较规则**：snapshots、major >=24、23.x 末段 `05-SNAPSHOT` 或字典序 >= `6`；因此 `23.05.10` 仍会被判为 pre-split，这是原版既有行为，未擅自修正。pre-split 不读取 profiles/kmods。

验证：`make build && make test && make lint`；可选 `ASU_METADATA_SMOKE=1 go test ./internal/metadata -run TestOpenWrtSmoke -v -count=1` 使用临时数据目录，只读真实 OpenWrt 24.10.1 x86/64 generic 与合并索引，断言含 `kmod-tun`。

## 同步规则

- 从数据库中 `origin=upstream` 的启用 source 推导支持的版本分支，与原版 `in_supported_branch/get_branch` 的版本映射对齐。版本列表去重，补主线及分支 SNAPSHOT；不自动启用全新分支。
- 每个 source 使用自己的 `upstream_url`。按发行版与 URL 分组拉取 `.versions.json`，每个版本从自己的 URL 读取 `.targets.json`。
- 同一发行版/分支分布于多个镜像站时，已有版本继续使用各自地址；新版本只从分支快照（没有时选最新启用版本）的地址发现，避免多个同步组争抢同一唯一键。
- 新行 `origin=upstream`，镜像、tag 模板和包管理器继承该发行版对应分支的内嵌默认值；自定义发行版使用其现有分支模板。既有行的 `overrides` 在写事务内重新读取并逐字段保留；`origin=local` 完全不改。
- 一个版本的 targets、branch、channel、synced_at 原子写入。targets 整块替换；版本或 targets 下载/解码失败均保留旧行，不创建不完整的新行。不自动删除历史版本。

## 与 Python 原版的有意差异

- 惰性缓存持久化到文件与边车，TTL 默认 1h，失败保留过期正文并告警；不是原版 requests-cache 的实现。额外的正文摘要用于检测磁盘损坏/非事务双文件写入。路径与 source 分支白名单比原版任意 path 更严格，非法路径返回 400；feeds.conf 空行/不完整行跳过而非抛异常。

- 公开兼容端点仍固定提供 OpenWrt 视图；ImmortalWrt 等发行版分别同步，通过管理目录访问。支持的分支来自 SQLite 目录，而非原版固定的 Python `settings.branches`；管理员可通过 source 目录调整。
- 公开版本端点只读 SQLite，不在请求路径阻塞刷新；因此内容最多落后一个刷新周期，且离线启动也能返回旧目录。保留的历史版本和启用的本地 source 也参与目录视图。
- 原版 targets 刷新失败会清成 `{}`；本实现按 ADR 0002 和 issue 要求保留旧 targets 并告警。
- `overview.server.version` 是 ASU-Go 的构建版本；其余 server 字段采用当前原版默认值。`overview.upstream_url` 取 OpenWrt 主线 source（没有时取目录首个版本）的配置地址。
- revision 按原版从 `profiles.json.version_code` 读取；未知分支返回原版 400 JSON，上游非 200 状态原样传递并使用原版错误体。网络异常或非法 profiles 对应原版未捕获异常的 500；不将错误伪装为成功 revision。

已对照：`asu/util.py:591–695`、`asu/main.py:141–197`、`asu/routers/api.py:36–70`、`asu/config.py:27–60,79–102`。兼容测试手写原版 JSON 结构，包含 release 的 package_changes / branch_off_rev、server 字段、revision 错误及 301。
