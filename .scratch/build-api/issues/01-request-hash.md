# 01: 请求模型与规范哈希

**What to build:** 构建请求能被校验并得到稳定的请求哈希。

**Blocked by:** foundation/01

**Status:** ready-for-agent

- [ ] `BuildRequest` 模型与字段校验（对照原版 `asu/build_request.py`、`allow_defaults`、`max_defaults_length`=20480、`max_custom_rootfs_size_mb`=1024、repositories 白名单）
- [ ] 规范 JSON → SHA-256（64 位 hex）；包名去 `+`、去重、排序；null 与空值显式区分
- [ ] 表驱动单元测试覆盖等价请求得到相同哈希、任一字段变化得到不同哈希

Parent spec: `.scratch/build-api/spec.md`
