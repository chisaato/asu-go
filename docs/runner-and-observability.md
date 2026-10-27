# 容器执行引擎与实时可观测性规范 (Runner & Observability)

本文档深入解析 OpenWrt 固件容器构建执行机制，并解决原版 ASU 中**“构建无 stdout 输出、可观测性严重缺失”**的根本问题。

---

## 1. 为什么原版构建没有 stdout 输出？

### 1.1 原版调用的致命缺陷
原版 ASU（参考 `/data/app-stacks/asu/asu/util.py`）使用 Python `podman` SDK 的 `container.exec_run(...)`：
```python
# 原版调用方式
returncode, output = container.exec_run(command, demux=True, user="buildbot")
stdout: str = output[0].decode("utf-8") if output[0] else ""
```
- **同步缓冲机制**：`container.exec_run` 底层通过阻塞读取流，直到进程完全退出并关闭流后，才把整个缓冲区切片返回。
- 在 `make image` 运行的 2~10 分钟内，标准输出全部积压在容器套接字缓冲区中，宿主机主进程和外部完全无法感知进度。
- 一旦中间卡在依赖包下载或者冲突报错，外部只能干等至整体任务超时。

### 1.2 OpenWrt ImageBuilder 本身静默机制
- ImageBuilder 的 Makefile 默认行为是静默模式，很多子命令被 `@` 抑制或者未输出详细信息。
- 没有 TTY 终端时，部分工具（如 apk / wget / opkg）会关闭进度条，变成全静默下载。

---

## 2. ASU-Go 的流式可观测性技术方案

ASU-Go 采用**容器主进程直接跑构建命令 + `logs?follow=1` 长连接流式读取 + 多路复用解包 + 内存环形缓冲 + SSE 实时广播**的架构。不使用 Hijack，也不做定时轮询。

```mermaid
flowchart LR
    subgraph Container["ImageBuilder 容器"]
        IBProcess["make image (Process)"]
    end

    subgraph GoRunner["ASU-Go Runner"]
        Socket["Unix Socket (logs?follow=1 长连接)"]
        Demux["stdcopy 解路器 (8-Byte Header Demux)"]
        Tee["Tee 分流器"]
        RingBuf["RingBuffer (最新 5000 行)"]
        FileSink["磁盘日志 (request_hash.log)"]
        Broadcaster["PubSub 广播器"]
    end

    subgraph AdminUI["前端管理后台"]
        SSEStream["SSE 连接 (text/event-stream)"]
        XTerm["Web 终端高亮显示"]
    end

    IBProcess -->|Stdout / Stderr 流| Socket
    Socket --> Demux
    Demux --> Tee
    Tee --> RingBuf
    Tee --> FileSink
    Tee --> Broadcaster
    Broadcaster --> SSEStream
    SSEStream --> XTerm
```

---

## 3. 核心技术实现细节

### 3.1 容器标准输出的实时读取与解复用
**为什么不需要 Hijack**：Hijack（HTTP `Upgrade` 劫持为双向原始 TCP 连接）只有在 `attach` / `exec start` 这类需要**交互式 stdin** 的场景才必要。我们的构建不需要输入，所以改为：

1. 创建容器时直接把构建命令设为容器的主进程（`Cmd`，不再用 `exec`）。
2. 调用 `GET /containers/{id}/logs?follow=1&stdout=1&stderr=1`。这是一个普通 HTTP 响应，服务端边产生日志边以 chunked 方式推送，客户端读到 EOF 即容器退出。
3. 等价于“推送”而非“轮询”：不需要定时请求，延迟更低，也不会漏行或重复。仅当长连接意外断开时，用 `since=<上次时间戳>` 重连补读，这是兜底而非主路径。
4. 退出码通过 `wait` 接口获取；Kubernetes 的 `pods/log?follow=true` 语义相同，接口因此可以直接复用。

非 TTY 模式下，logs 响应使用 Docker/Podman 统一的 8 字节多路复用帧：
- **Byte 0**: 流类型（`0x01` = Stdout, `0x02` = Stderr）
- **Bytes 1~3**: 保留字节（全 0）
- **Bytes 4~7**: 数据块长度（大端 uint32）

该解帧由驱动内部的 `stdcopy.StdCopy` 完成，对外暴露为已解复用的 `io.ReadCloser`：
```go
// 驱动内部：follow 读取并解复用
rc, err := cli.ContainerLogs(ctx, id, container.LogsOptions{
    ShowStdout: true,
    ShowStderr: true,
    Follow:     true,
})
if err != nil {
    return err
}
defer rc.Close()

// 将 stdout/stderr 实时分流到并发安全的管道
go func() {
    _, _ = stdcopy.StdCopy(stdoutWriter, stderrWriter, rc)
}()
```

### 3.2 构建容器执行流程（取代原版 sleep + exec）
原版：容器主进程为 `sleep`，输入用 `put_archive`，所有步骤走 `exec_run`，产物用 `get_archive`，构建容器没有宿主 bind mount。ASU-Go 保留 put/get_archive 思路，仅把 exec 改为主进程：

1. `create`：不设 AutoRemove，不挂载任何宿主路径；沿用原版安全设置（`cap_drop=all`、`no_new_privileges`、非特权）与独立网络 `asu-build`（不存在则自动创建）；打 label `managed-by=asu-go`、`asu.request_hash=<hash>`。
2. `put_archive`：在 `start` 之前注入 defaults 脚本（`asu-files/etc/uci-defaults/99-asu-defaults`）、repositories（apk 为 `repositories`，opkg 为 `repositories.conf`，按镜像内文件检测）、公钥。
3. `start`：主进程为构建脚本，依次执行 `make info`、`make manifest`、`make image`。构建输出目录位于容器自身可写层（原版的 tmpfs 在容器退出后 `get_archive` 读不到）。
4. `logs?follow=1`：Runner 流式读取并解析阶段标记。
5. `wait` 取退出码；`get_archive` 取回产物（容器已退出也可取）；落盘成功后删除容器。

### 3.3 阶段标记（取代 Phase Sniffing）
构建脚本在每个阶段前输出固定标记行：`##asu:stage=<name>`。Runner 解析后得到与原版 `X-Imagebuilder-Status` 一致的阶段值：`init`、`container_setup`、`validate_revision`、`validate_manifest`、`building_image`，并写入任务记录。标记行本身保留在日志中。不再用正则嗅探 ImageBuilder 的输出。

### 3.4 可观测性参数
- `verbose=true` 时向 `make image` 注入 `V=s`，展示完整打包与安装过程。
- 阶段事件（`event: stage`）与 SSE 随管理后台二期实现；一期只提供 follow 式纯文本日志 `GET /admin/api/jobs/{hash}/log?follow=1`。

### 3.5 应用日志（Zap）

应用日志与 `{data_dir}/logs/{id}.log` 的构建输出完全分离：绝不把构建输出行（包括失败时的 tail）复制到 Zap；阶段标记仅提取阶段名称。通过 `log.level` / `log.format` 控制级别与 JSON/console 格式。

- 固定使用 snake_case 字段：`component`（`http` / `queue` / `runner` / `runtime` / `scavenger` 等）、`request_hash`、`job_id`、`distribution`、`version`、`target`、`profile`；HTTP 链路额外带 `request_id`。后台任务不伪造 HTTP request_id，通过 request_hash 关联入队请求。
- 入队、出队（`queue_duration`）、开始/完成/失败（`duration`）、每次阶段变化（`stage`、`previous_stage`、`stage_duration`）均记录。超时/取消通过失败日志中的 `reason` 区分；关闭时记录 kill/remove 清理。耗时字段由 Zap duration encoder 编码，HTTP 的 `latency_ms` 固定毫秒。
- runtime 记录镜像检查/拉取、网络准备、create、put_archive、start、wait（`exit_code`）、get_archive、kill、remove；归档日志只包含 `bytes`，不包含正文。引擎 HTTP API 的方法、路径、状态和耗时使用 debug，不记录查询串、命令或环境变量。不透明句柄仅在 runtime 与 runner 的 Handle 日志中以 `container` 字段出现。
- 重启接管记录任务、句柄与 `replayed_lines`；scavenger 每轮记录 `found` / `removed`；TTL cleaner 记录删除数量、公开 store/诊断产物/日志的 `freed_bytes` 与 `usage_available`。无清理动作使用 debug，有状态变化使用 info，可恢复异常 warn、执行失败 error 并携带 error。
- HTTP 结束摘要包括 method/path/route/status/latency_ms/client_ip/bytes/user_agent/request_id；查询串整体不记录，认证头、Cookie、token 与请求体也不记录。成功健康检查及前端资源降 debug；4xx warn、5xx error（含 panic）。`X-Request-Id` 沿用或生成回写，panic 日志不记录可能含凭据的 panic 内容；SSE/follow 连接在结束时记录。
- 管理登录成功 info、失败 warn，不记录凭据；已认证管理写操作记录操作者、路由、状态与 request_id（token 认证仅以 `api_token` 标记身份）。

---

## 4. 任务取消与孤儿容器级联清理 (Anti-Zombie)

在原版 Python 实现中，如果服务发生异常崩溃或重启，正在运行的 Podman 构建容器会变成孤儿容器留在系统中，持续占用 CPU 和内存。

ASU-Go 引入三重防护：
1. **Context 级联取消**：
   每个 Runner 任务在启动时在本地嵌入式数据库中记录不透明的 `Handle`。任务被用户取消或超时退出时，`defer` 钩子自动向 Podman 发送 `Kill` 与 `Remove(force=true, volumes=true)`。
2. **重启接管 (Takeover)**：
   服务启动时，对数据库中状态为 `running` 的任务，用 `Handle` 查询容器：
   - 容器仍在运行：重新 `logs?follow=1&timestamps=1` **从头**读取，重建 RingBuffer 并覆写磁盘日志文件（容器日志是权威来源，因此不丢行、不重复），然后继续走正常的 wait / 取产物 / 清理流程。
   - 容器已退出：同样从日志补读，再收尾。
   - 容器不存在：任务标记为 `failed`（`detail: interrupted`）。
   - 前提：容器日志驱动必须可读（json-file / k8s-file / journald），启动时检查，`none` 则拒绝启动。
3. **启动自愈探测器 (Zombie Scavenger)**：
   扫描所有 label 带有 `managed-by=asu-go` 但数据库中没有对应活跃任务的容器，批量异步清理。

---

> **注（运行时抽象）**：上文的 logs 流读取 / 多路复用解包属于 Docker/Podman **驱动内部**实现细节，对外统一暴露为 `Runtime.Logs` 返回的 `io.ReadCloser`（已解复用）。调度与队列层不得依赖这些概念，以便未来接入 Kubernetes 驱动。详见 [tech-stack.md §4](tech-stack.md)。
