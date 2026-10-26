# 01: 请求模型与规范哈希

**What to build:** 构建请求能被校验并得到稳定的请求哈希。

**Blocked by:** foundation/01

**Status:** ready-for-human

- [x] `BuildRequest` 模型与字段校验（对照原版 `asu/build_request.py`、`allow_defaults`、`max_defaults_length`=20480、`max_custom_rootfs_size_mb`=1024、repositories 白名单）
- [x] 规范 JSON → SHA-256（64 位 hex）；包名去 `+`、去重、排序；null 与空值显式区分
- [x] 表驱动单元测试覆盖等价请求得到相同哈希、任一字段变化得到不同哈希

Parent spec: `.scratch/build-api/spec.md`

## Comments

- 已实现 `internal/buildreq`：`Request`、`NewRequest`、`Limits`、`Validate`、`Normalize`、`Canonical`、`Hash`；没有接入 Gin、数据库或配置包。
- JSON 解码应用原版默认值，并将必填、类型、非空字段错误归类为 422；策略错误为 400。程序内构造使用 `NewRequest`，可空标量以指针区分 null 与空值。
- `Canonical` 保存含 `client` 的完整规范请求；`Hash` 仅使用 API 规范列出的 14 个构建字段，明确排除 `client`。规范 JSON 递归键排序、无空白、UTF-8、无 HTML 转义，三个黄金样例由独立 Python JSON/SHA-256 生成。
- 按任务要求允许 packages 的单个 `+` 前缀（原版模型正则未允许，但原版规范化代码明确处理该前缀）。packages_versions 保留为独立输入字段，不以其键覆盖 packages。
- repositories 精确复现原版 scheme + hostname + 原始 path 目录前缀规则（不比较端口、不清理路径中的 `..`）；测试明确锁定这些兼容行为，不额外收紧协议。
- `MetadataValidator` 预留元数据合法性回调；profile 别名解析与 source/target/version 查询留给后续 service。
- 验证：`go test ./internal/buildreq/... && make lint` 通过。
