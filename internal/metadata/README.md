# 上游元数据

`Syncer.Run` 由 `serve` 的 errgroup 持有，启动后异步立即同步，再按 `metadata.refresh_interval` 执行。失败只告警，不终止 HTTP 服务；取消 context 会中止在途 HTTP 请求。`kv[metadata.last_sync]` 保存最近一轮结束时间 `at` 与失败计数 `failures`，不是“最近一次全量成功时间”。

`Client.Fetch` 提供带超时、User-Agent、ETag 和 Last-Modified 的通用条件请求；`Client.Get` 在进程内保存成功响应，收到 304 时复用正文。失败响应不覆盖成功缓存。后续 profiles / 包索引磁盘缓存可以直接调用 `Fetch`，传入磁盘验证器；本 issue 尚不实现磁盘缓存。

## 同步规则

- 从数据库中 `origin=upstream` 的启用 source 推导支持的版本分支，与原版 `in_supported_branch/get_branch` 的版本映射对齐。版本列表去重，补主线及分支 SNAPSHOT；不自动启用全新分支。
- 每个 source 使用自己的 `upstream_url`。按发行版与 URL 分组拉取 `.versions.json`，每个版本从自己的 URL 读取 `.targets.json`。
- 同一发行版/分支分布于多个镜像站时，已有版本继续使用各自地址；新版本只从分支快照（没有时选最新启用版本）的地址发现，避免多个同步组争抢同一唯一键。
- 新行 `origin=upstream`，镜像、tag 模板和包管理器继承该发行版对应分支的内嵌默认值；自定义发行版使用其现有分支模板。既有行的 `overrides` 在写事务内重新读取并逐字段保留；`origin=local` 完全不改。
- 一个版本的 targets、branch、channel、synced_at 原子写入。targets 整块替换；版本或 targets 下载/解码失败均保留旧行，不创建不完整的新行。不自动删除历史版本。

## 与 Python 原版的有意差异

- 公开兼容端点仍固定提供 OpenWrt 视图；ImmortalWrt 等发行版分别同步，通过管理目录访问。支持的分支来自 SQLite 目录，而非原版固定的 Python `settings.branches`；管理员可通过 source 目录调整。
- 公开版本端点只读 SQLite，不在请求路径阻塞刷新；因此内容最多落后一个刷新周期，且离线启动也能返回旧目录。保留的历史版本和启用的本地 source 也参与目录视图。
- 原版 targets 刷新失败会清成 `{}`；本实现按 ADR 0002 和 issue 要求保留旧 targets 并告警。
- `overview.server.version` 是 ASU-Go 的构建版本；其余 server 字段采用当前原版默认值。`overview.upstream_url` 取 OpenWrt 主线 source（没有时取目录首个版本）的配置地址。
- revision 按原版从 `profiles.json.version_code` 读取；未知分支返回原版 400 JSON，上游非 200 状态原样传递并使用原版错误体。网络异常或非法 profiles 对应原版未捕获异常的 500；不将错误伪装为成功 revision。

已对照：`asu/util.py:591–695`、`asu/main.py:141–197`、`asu/routers/api.py:36–70`、`asu/config.py:27–60,79–102`。兼容测试手写原版 JSON 结构，包含 release 的 package_changes / branch_off_rev、server 字段、revision 错误及 301。
