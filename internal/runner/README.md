# Runner 接入约定

`Runner{Runtime, Hub, JobTimeout}.Run(ctx, Job) (Result, error)` 同步完成一次构建；由 `serve` 持有的任务协程调用，Runner 不创建脱离调用方生命周期的后台任务。超时默认 10 分钟，取消与超时均使用独立清理 context 执行 Kill/Remove。

## build-api/02 接入

- `Job.ID` 为请求哈希或重放 ID，允许 1–128 位字母、数字、`_`、`-`；`Image` 为完整镜像引用，`PullPolicy` 可覆盖驱动默认值。
- `Profile`、`Packages` 为规范化后的构建选择；差异包计算与仓库 allow-list 属于上层。`PackageVersions` 对 manifest 的包名及版本做严格校验，`VersionCode` 非空时校验 ImageBuilder revision。
- `Defaults` 注入 `asu-files/etc/uci-defaults/99-asu-defaults`；`Repositories` 为名称到 URL 的映射，`RepositoriesMode=append` 保留镜像仓库，否则替换。启动脚本检测 apk/opkg，空仓库和空公钥列表不会注入相应文件。`RepositoryKeys` 支持 PEM 与 usign 公钥。
- `Setup=true` 在 `make info` 前运行镜像的 `setup.sh`；快照所需变量由 `Env` 传入。支持 `RootFSSizeMB`、`Filesystem`、`Verbose`。
- `ArtifactDir` 必须指定，建议使用任务专属空临时目录：成功后再由存储层发布，失败后由调用方删除临时目录。Runner 拒绝 tar 路径穿越、链接及特殊文件，普通文件修正为 0644，新建目录为 0755。
- `OnHandle(ctx, handle)` 在 Create 后、输入注入前调用，用于同步持久化不透明句柄。`OnStage(ctx, Stage{Name, At})` 按日志标记顺序调用，用于更新 `jobs.stage/stages`；回调错误会终止任务并清理。回调应迅速返回并遵守 context。
- `Result` 返回 `Handle`、`Stages`、`ExitCode`、`ArtifactDir`。句柄成功时也返回，但对应资源已经删除。未获得退出码时 `ExitCode=-1`。
- 使用 `errors.As(err, *Error)` 检查 `Kind=failed|timeout|cancelled`、`ExitCode`、`Tail`；`errors.Is` 可识别 context 错误。失败文本保留最多 16 KiB 日志尾部，空间不足附加原版友好报错。
- `KeepContainer=true` 仅在普通构建失败时保留资源，仍执行 Kill；取消/超时始终删除。创建时打 `asu.keep_container=true` label，由 Scavenger 执行 TTL 与数量回收。

## 日志接口

所有任务共享 `stream.NewHub(dataDir, capacity)`，容量非正值默认 5000 行；Runner 自动 Begin/Finish。Hub 无轮询或广播协程，慢订阅者丢弃最旧待发送行。磁盘 `{data_dir}/logs/{id}.log` 保留完整日志。上层在不再需要内存尾部时调用 `Hub.Forget(id)`，磁盘历史不受影响，进程退出时关闭 Hub。

将数据库/队列适配为 `stream.JobSource.State(ctx,id) (exists,finished,error)`，通过 `admin.RegisterLogs(已鉴权管理路由组, stream.Service{Hub,Jobs})` 挂载 `/admin/api/jobs/:id/log`。活动任务的 `?follow=1` 原子取得 ring 快照后续写，Runner Finish 后连接结束，客户端断开即退订；历史和不存在于 JobSource 中的任务从磁盘读取。serve 已使用 `SQLiteJobStore`，build-api/02 应把同一个 Hub 注入 Runner。

## 接管、任务存储与回收（build-api/02、03）

- `Resume(ctx, Job, Handle) (Result,error)` 不需要原始构建参数，仅需 ID、ArtifactDir、KeepContainer 和可选 OnStage；先 Inspect，缺失返回 `*Error{Kind:"interrupted"}`。存在则从头 `Logs(Follow:true,Timestamps:true)`，覆写磁盘日志、重建 ring，剥离运行时加的时间戳并用其恢复阶段时间；共享 Run 的 wait、取产物、清理。重复内容的合法日志行不会被去重。
- `JobStore`：`Active(ctx) ([]Task,error)` 返回 queued/running 的 ID、Handle、Status、KeepContainer；`Complete(ctx,id,Result,runErr)` 负责落库及上层产物发布。`Runner.Takeover(ctx,store,artifactRoot)` 并行恢复所有 running，产物暂存于 `{artifactRoot}/{id}`；构建失败由 Complete 记录，存储/运行时查询错误返回调用方，不把查询失败误记为 interrupted。方法阻塞到全部结束，须由 serve 的 errgroup 持有。
- `SQLiteJobStore{DB:*sql.DB}` 基于现有 jobs 表，无新迁移；提供 `SaveHandle(ctx,id,h)`（可包装为 OnHandle）、Active、Complete 和日志接口 State。**先持久化 running 记录，再调用 Run**，否则 Create 到 SaveHandle 的窗口无法受保护。句柄保存为 JSON 字符串，也接受设计文档的 `{driver,id}` 包装（id 本身是不透明 Handle）。KeepContainer 从 `jobs.options.keep_container` 恢复。
- 当前 SQLite Complete 将 Runner Result 写入 jobs.result、Stages 写入 jobs.stages，完成时写 status/detail/error/finished_at；这是队列接入前的恢复实现。build-api/02 应包装/替换 Complete，在成功时发布 ArtifactDir 并构造协议兼容的 images/manifest 等 result，不可直接把 Runner Result 当作公开 API 响应。未发布的恢复产物保留于 `{data_dir}/recovery/{id}`。普通 Run 的 OnStage、OnHandle 和 Complete 调用仍由队列负责。
- `Scavenger{Runtime,Jobs,Interval,KeepContainerTTL,MaxKept}.Run(ctx)` 启动立即 Sweep，以后按 `runtime.scavenger_interval`（默认 5m）周期执行；Sweep 可单独调用。只清理 `managed-by=asu-go` 环境；活跃任务通过 Handle 或 request_hash 匹配，删除前复查数据库，查询失败不删除。非活跃、已退出且带 keep label 的诊断环境按 FinishedAt（无值回退 CreatedAt）保留 1h，数量默认 3，超出删最旧；MaxKept=0 不保留。活跃环境永远不受保留限额影响。
- `Runtime.Inspect(ctx,h) (State,error)`：Exists=false 表示不存在；State 提供 Running、Labels、CreatedAt、FinishedAt。`Runtime.List(ctx)` 仅列本服务标签的运行中/已退出环境。驱动实现封装底层引擎数据，队列不解析 Handle。
- `Job.Rolling=true` 时 Runner 按 `RollingRecheck`（默认 6h）强制使用 always 拉取，间隔内仍使用 Job.PullPolicy/驱动默认策略。成功检查时间按镜像保存在 **Runner 实例内存**，失败不会更新时间，进程重启或新实例首次使用均重新检查；应复用同一个 Runner。serve 已映射 `runtime.rolling_recheck`，未来队列由 source.rolling 设置 Job.Rolling。
- 当前 serve 的 errgroup 已持有 Takeover 和 Scavenger；无任务时与空表兼容，但启动需要可用且日志可读的运行时。Takeover 的正常返回不会终止服务，关闭时取消在途任务并等待清理。

集成验证：

```sh
ASU_TEST_RUNTIME_DRIVER=podman ASU_TEST_RUNTIME_SOCKET=unix:///var/run/docker.sock go test -tags integration ./internal/runner/...
```

测试只使用 alpine 中的模拟 make 脚本，不拉取真实 ImageBuilder。
