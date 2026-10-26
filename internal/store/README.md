# 产物存储

`Store` 为 Cleaner 与下载提供稳定边界，所有操作接收 `context.Context`：

- `Publish(ctx, hash, srcDir)`：目录 rename 发布，成功后接管源目录；同哈希替换先备份，失败恢复旧目录。源与存储须在同一文件系统，否则返回错误，不降级为部分可见的复制。进程内共用一个 `Local` 实例，发布与打开/删除串行化；已打开的文件可继续读取。
- `Open(ctx, hash, file)`：返回 `File`（`io.ReadSeekCloser` + `Stat`），调用方关闭。允许安全的相对子目录；拒绝隐藏组件、反斜杠、符号链接及非普通文件。`os.Root` 保证链接/重命名不能越出目录。
- `Stat(ctx, hash, file)`：安全查询普通文件信息。
- `Delete(ctx, hash)`：幂等删除整个构建目录；不存在成功。Cleaner 从数据库选取待删除 hash，无需遍历存储。
- `Usage(ctx)`：普通文件逻辑字节数；不跟随链接、不统计隐藏内部备份。

hash 必须为 64 位小写 SHA-256，或 `rp_` 加规范的 26 位大写 ULID（首位 0–7，排除 I/L/O/U）。删除、发布也使用同一校验。

`store.dir` 留空时动态使用 `{data_dir}/store`；serve 将同一实例交给 queue 与匿名 `/store/*path`。下载保持原版固定 `application/octet-stream`（包括 JSON、文本和 manifest）、附件响应、`nosniff` 与 `{"detail":"Not found"}` 404，另支持 Range 与 If-Modified-Since。S3/未知驱动由配置校验拒绝，一期不返回 S3 重定向。

TTL 选择、数据库/日志回收由后续 Cleaner 实现；存储包不依赖队列或数据库。替换失败自动恢复不等同于断电事务：崩溃遗留的 `.replace-*` 保留供人工恢复，不主动删除潜在的唯一旧副本。
