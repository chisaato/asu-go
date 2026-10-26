# 术语表 (Glossary)

对话、命名、测试名统一使用下列术语；括号内为需避免混用的近义词。

| 术语 | 定义 |
| --- | --- |
| **构建请求 (Build Request)** | 客户端提交的 `/api/v1/build` 载荷：版本、target、profile、包列表、defaults 等。 |
| **请求哈希 (Request Hash)** | 由规范化后的构建请求计算出的标识，同时作为任务 ID 与产物目录名；相同请求复用同一结果。（避免：build id / job id） |
| **任务 (Job)** | 一个请求哈希对应的一次调度单元，有排队、运行、成功、失败、取消等状态。 |
| **队列 (Queue)** | 持有待执行任务并按并发上限分发的组件；默认内存 + SQLite 持久化。 |
| **运行时 (Runtime)** | `internal/runtime` 中的容器执行抽象；具体实现称**驱动 (Driver)**：docker / podman / kubernetes。（避免：在调度层说 “Docker”） |
| **句柄 (Handle)** | 运行时驱动返回的不透明值，用于定位一次正在执行的构建，可持久化以便重启后回收。 |
| **Runner** | 把任务交给 Runtime 执行、采集日志、回收资源的编排逻辑（`docs/runner-and-observability.md`）。 |
| **ImageBuilder 容器** | 内含 OpenWrt ImageBuilder 的构建容器，每个任务独占一个。 |
| **日志流 (Log Stream)** | 构建输出的实时行流；由 RingBuffer（内存近 N 行）与 Log Sink（落盘 `.log`）组成，经 SSE 广播。 |
| **产物 (Artifact)** | 构建输出的固件镜像与 manifest 等文件，存放于 `store/{request_hash}/`。 |
| **存储 (Store)** | 产物存储抽象，实现为本地 FS 或 S3 兼容。 |
| **元数据 (Metadata)** | 上游发布源的版本、target、`profiles.json`、包索引等缓存。 |
| **发行版 (Distribution)** | OpenWrt、ImmortalWrt 等上游分支体系。 |
| **管理后台 (Admin Dashboard)** | 内置于二进制、挂载在 `/admin` 的 Nuxt SPA。 |
| **来源 (Source)** | 数据库中的一条“发行流”记录：发行版、版本、上游 URL、ImageBuilder 镜像基名与 tag 模板、包管理器、`origin`、`rolling`。 |
| **阶段 (Stage)** | 构建脚本以 `##asu:stage=<name>` 标记输出的构建阶段，对应原版 `X-Imagebuilder-Status`。 |
| **接管 (Takeover)** | 服务重启后重新连接仍在运行/已退出的构建容器，从容器日志重放并收尾。 |
| **驱动 (Driver)** | `queue` / `store` / `stats` / `runtime` 等接口的可切换实现（如 sqlite、local；redis、s3 预留）。 |
| **重放 (Replay)** | 用已有任务或原始请求 JSON 另起一次诊断构建，独立 id（`rp_<ulid>`），不去重、不缓存、不计统计。 |
