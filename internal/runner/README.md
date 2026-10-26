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
- `KeepContainer=true` 仅在失败时保留资源，仍执行 Kill；创建时打 `asu.keep_container=true` label。TTL 与数量回收由 runtime-runner/03 实现。

## 日志接口

所有任务共享 `stream.NewHub(dataDir, capacity)`，容量非正值默认 5000 行；Runner 自动 Begin/Finish。Hub 无轮询或广播协程，慢订阅者丢弃最旧待发送行。磁盘 `{data_dir}/logs/{id}.log` 保留完整日志。上层在不再需要内存尾部时调用 `Hub.Forget(id)`，磁盘历史不受影响，进程退出时关闭 Hub。

将数据库/队列适配为 `stream.JobSource.State(ctx,id) (exists,finished,error)`，通过 `admin.RegisterLogs(已鉴权管理路由组, stream.Service{Hub,Jobs})` 挂载 `/admin/api/jobs/:id/log`。活动任务的 `?follow=1` 原子取得 ring 快照后续写，Runner Finish 后连接结束，客户端断开即退订；历史和不存在于 JobSource 中的任务从磁盘读取。当前 serve 使用内存 JobSource 占位，build-api/02 需要替换为真实适配器，并把同一个 Hub 注入 Runner。

集成验证：

```sh
ASU_TEST_RUNTIME_DRIVER=podman ASU_TEST_RUNTIME_SOCKET=unix:///var/run/docker.sock go test -tags integration ./internal/runner/...
```

测试只使用 alpine 中的模拟 make 脚本，不拉取真实 ImageBuilder。
