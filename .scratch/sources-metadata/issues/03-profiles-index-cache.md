# 03: profiles 与包索引缓存

**What to build:** 客户端可通过 `/json/v1` 取到 profile 详情与包索引，数据惰性拉取并缓存。

**Blocked by:** sources-metadata/02

**Status:** done

- [x] `profiles.json`、包索引惰性拉取，磁盘缓存 `data/metadata/` 加 `.meta.json` 边车文件（ETag、Last-Modified），条件请求
- [x] 24.10+/23.05.6+/SNAPSHOT 的 kmod 拆分合并（对照原版 `asu/util.py:488-695`）
- [x] `{path}/index.json`、`{arch}-index.json`、`targets/{target}/{profile}.json`
- [x] 上游失败回退过期缓存
- [x] 测试：条件请求命中 304；kmod 合并结果与原版一致

实现说明与稳定的 `ProfileProvider` 接口见 `internal/metadata/README.md`。`metadata.cache_ttl` 默认 1h；原版 23.x 字典序比较（包括 23.05.10 判 pre-split）刻意保留。

验证：`make build && make test && make lint` 全部通过；`ASU_METADATA_SMOKE=1 go test ./internal/metadata -run TestOpenWrtSmoke -v -count=1` 通过。真实 OpenWrt 24.10.1 x86/64 generic，kernel 6.6.86-1-af351158cfb5febf5155a3aa53785982，索引 1216 包且包含 `kmod-tun=6.6.86-r1`；全部使用临时 data_dir，只读上游。

Parent spec: `.scratch/sources-metadata/spec.md`
