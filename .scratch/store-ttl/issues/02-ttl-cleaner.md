# 02: TTL 与清理

**What to build:** 过期的产物、日志与数据库记录被自动回收，也可手动删除。

**Blocked by:** store-ttl/01, build-api/03

**Status:** ready-for-agent

- [ ] TTL 规则：带 defaults 30m、带 `packages_versions` 7d、其余 24h、失败 1h，均可配置，入队时算出 `expires_at`
- [ ] TTL Cleaner 按 `expires_at`/`purge_at` 回收产物目录、日志与记录，启动时先跑一次（可注入时钟）
- [ ] `DELETE /admin/api/store/{hash}`
- [ ] 测试：过期回收；重启后未过期产物仍可下载

Parent spec: `.scratch/store-ttl/spec.md`
