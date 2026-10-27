# 02: TTL 与清理

**What to build:** 过期的产物、日志与数据库记录被自动回收，也可手动删除。

**Blocked by:** store-ttl/01, build-api/03

**Status:** done

- [x] TTL 规则：带 defaults 30m、带 `packages_versions` 7d、其余 24h、失败 1h，均可配置，入队时算出 `expires_at`
- [x] TTL Cleaner 按 `expires_at`/`purge_at` 回收产物目录、日志与记录，启动时先跑一次（可注入时钟）
- [x] `DELETE /admin/api/store/{hash}`
- [x] 测试：过期回收；重启后未过期产物仍可下载

实现说明：Cleaner 位于 `internal/queue/cleaner.go`，共用队列锁防止重新入队与旧产物删除竞争。下载路由每次查询 jobs，过期无需等待清理即可 GET/HEAD 404；手动删除保留记录、标记 `result.artifacts_removed=true` 并使公开轮询 404。普通产物按 expires_at，日志/记录/重放与 keep_container 产物按 purge_at；内部临时目录不自动删除。边界、活跃任务、孤儿宽限、失败重试、DELETE 与重启下载均有测试。

验证：`make build && make test && make lint`。

Parent spec: `.scratch/store-ttl/spec.md`
