# SQLite 构建队列

`NewSQLite` 组装 jobs 表、ProfileProvider、Runner 与 ArtifactPublisher。`Run(ctx)` 由 serve 的 errgroup 持有：先接管 running，再按并发上限消费 queued；唤醒 channel 只是通知，SQLite 是唯一事实来源。关闭等待 Runner 清理容器并落库。

公开接口：`POST /api/v1/build`、`GET|HEAD /api/v1/build/:hash`、`GET /api/v1/stats`。队列位置从 0 开始，与原版 RQ 一致；所有任务响应含 `enqueued_at`、`request_hash` 与两种状态头。失败及成功缓存到期对客户端不可见，queued/running 不因等待超过 TTL 而重复执行。

`Publisher` 默认使用 `store.Local`，serve 将其与匿名 `/store/*path` 下载路由共用。存储目录由 `store.dir` 指定（默认 `{data_dir}/store`）；旧同哈希产物先移到临时目录，rename 发布失败恢复。Runner 额外导出 `manifest` 与 `build_cmd_packages`，恢复收尾也能构造同一结果。日志接口使用同一个 SQLiteJobStore 与 Hub。

## 与原版的有意差异

- 按设计使用规范 JSON SHA-256，不复刻 Python repr 哈希；源码别名在哈希后解析，jobs.request 保留规范化原请求，冗余 profile 保存实际构建 ID。
- 达到（而非超过）`max_pending_jobs` 即拒绝新请求，以实现严格容量上限；所有已存在任务（不仅 done）计 cache_hits。统计为 UTC 天粒度 SQLite 计数，包统计排除默认包、删除包与重放。
- 元数据目录不在请求路径强制联网刷新；profiles 支持 supported_devices 别名、逗号替换和唯一 generic 回退。差异包从实际 ImageBuilder 的 make info 推导；包统计排除默认包时使用上游 profiles，应保持镜像与上游元数据一致。
- 保留产物原有顶层元数据与所选 profile 字段，重新计算实际固件 sha256；不做原版可选签名。stdout/stderr 使用 Runtime 的合并日志尾部，不能再可靠拆分成两个流。
- `internal/pkgchanges` 全量迁移原版 `package_changes.py`（231 行版本）的 24 条规则，按版本、target、实际 profile 修正构建包集合；原版没有 distro 限制，OpenWrt / ImmortalWrt 均应用。版本前缀规则不自动扩展到更新版本，语言包规则故意使用原版字符串 `>= "24.10"` 比较，只有精确 `SNAPSHOT` 移除两个旧 kmod。

## 包修正与哈希顺序

原版 `routers/api.py:115–118` 优先从非空 `packages_versions` 取包名，`api.py:228` / `build.py:144` 计算请求哈希；`build.py:272` 在读取 `make info` 后执行包修正，随后 `build.py:287–290` 计算差异包。因此包修正不参与原版请求哈希。Go 同样保留规范化原请求的哈希和 `jobs.request`，在 `job()` 组装时用已解析的实际 profile 应用纯函数规则，再交给 Runner 依据实际 ImageBuilder 默认包计算 `diff_packages`。执行前后读取 `make info` 的位置不同，但规则不依赖其结果，实际 `build_cmd_packages` 的集合保持一致（请求包排序仍沿用 Go 规范化约定）。移除默认包必须发生在修正之后，避免为新增驱动/固件生成负包。

`packages_versions` 的非空键集合仍优先于 `packages`，包名修正不改动版本约束映射；这忠实保留原版限制：对被改名或删除的旧包指定版本可能导致 manifest 校验失败。规则新增去重、改名不去重，负包名不触发正包改名/移除。入队时也用同一纯函数生成 `options.build_cmd_packages` / `stat_packages` 快照，保证包统计使用修正后的非默认包；默认包统计仍依据上游 profiles（见上述差异）。最终公开 `build_cmd_packages` 来自 Runner 导出的实际命令。

人工 owut/LuCI 完整固件构建留待真实设备及正式 ImageBuilder 环境；本 issue 使用 Fake 和 Podman 小镜像脚本验证协议全链路。
