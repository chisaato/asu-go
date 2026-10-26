# SQLite 构建队列

`NewSQLite` 组装 jobs 表、ProfileProvider、Runner 与 ArtifactPublisher。`Run(ctx)` 由 serve 的 errgroup 持有：先接管 running，再按并发上限消费 queued；唤醒 channel 只是通知，SQLite 是唯一事实来源。关闭等待 Runner 清理容器并落库。

公开接口：`POST /api/v1/build`、`GET|HEAD /api/v1/build/:hash`、`GET /api/v1/stats`。队列位置从 0 开始，与原版 RQ 一致；所有任务响应含 `enqueued_at`、`request_hash` 与两种状态头。失败及成功缓存到期对客户端不可见，queued/running 不因等待超过 TTL 而重复执行。

`Publisher` 默认使用 `store.Local`，serve 将其与匿名 `/store/*path` 下载路由共用。存储目录由 `store.dir` 指定（默认 `{data_dir}/store`）；旧同哈希产物先移到临时目录，rename 发布失败恢复。Runner 额外导出 `manifest` 与 `build_cmd_packages`，恢复收尾也能构造同一结果。日志接口使用同一个 SQLiteJobStore 与 Hub。

## 与原版的有意差异

- 按设计使用规范 JSON SHA-256，不复刻 Python repr 哈希；源码别名在哈希后解析，jobs.request 保留规范化原请求，冗余 profile 保存实际构建 ID。
- 达到（而非超过）`max_pending_jobs` 即拒绝新请求，以实现严格容量上限；所有已存在任务（不仅 done）计 cache_hits。统计为 UTC 天粒度 SQLite 计数，包统计排除默认包、删除包与重放。
- 元数据目录不在请求路径强制联网刷新；profiles 支持 supported_devices 别名、逗号替换和唯一 generic 回退。差异包从实际 ImageBuilder 的 make info 推导；包统计排除默认包时使用上游 profiles，应保持镜像与上游元数据一致。
- 保留产物原有顶层元数据与所选 profile 字段，重新计算实际固件 sha256；不做原版可选签名。stdout/stderr 使用 Runtime 的合并日志尾部，不能再可靠拆分成两个流。
- 本 issue 不迁移原版 package_changes.py 的历史版本包名修正规则；显式选包按规范化请求执行。

人工 owut/LuCI 完整固件构建留待真实设备及正式 ImageBuilder 环境；本 issue 使用 Fake 和 Podman 小镜像脚本验证协议全链路。
