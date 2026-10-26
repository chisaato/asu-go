# 01: 产物存储与下载

**What to build:** 构建成功后产物落盘，客户端可通过 `/store` 下载。

**Blocked by:** build-api/02

**Status:** done

- [x] `store.driver=local`：`{store_dir}/{request_hash}/`，路径穿越防护
- [x] `GET|HEAD /store/{hash}/{file}`，`X-Content-Type-Options: nosniff`
- [x] 构建完成路径接入 `get_archive` 取回产物并落盘
- [x] 配置 `store.driver=s3` 时启动报错
- [x] 路径穿越测试

实现说明：`internal/store/README.md`；`Store` 提供 Publish/Open/Stat/Delete/Usage 供后续 Cleaner 使用，queue 与匿名下载共用同一个 Local 实例。原版所有扩展名均强制 octet-stream；未实现 S3，维持启动拒绝。

验证：`make build && make test && make lint`；`go test -tags integration ./internal/api/...`（Podman，包含实际产物下载）。覆盖失败恢复、并发同 hash 发布及读取、路径/链接/隐藏文件、HEAD/Range/304/404、Fake 构建后下载。

Parent spec: `.scratch/store-ttl/spec.md`
