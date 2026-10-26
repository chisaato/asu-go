# 03: profiles 与包索引缓存

**What to build:** 客户端可通过 `/json/v1` 取到 profile 详情与包索引，数据惰性拉取并缓存。

**Blocked by:** sources-metadata/02

**Status:** ready-for-agent

- [ ] `profiles.json`、包索引惰性拉取，磁盘缓存 `data/metadata/` 加 `.meta.json` 边车文件（ETag、Last-Modified），条件请求
- [ ] 24.10+/23.05.6+/SNAPSHOT 的 kmod 拆分合并（对照原版 `asu/util.py:488-695`）
- [ ] `{path}/index.json`、`{arch}-index.json`、`targets/{target}/{profile}.json`
- [ ] 上游失败回退过期缓存
- [ ] 测试：条件请求命中 304；kmod 合并结果与原版一致

Parent spec: `.scratch/sources-metadata/spec.md`
