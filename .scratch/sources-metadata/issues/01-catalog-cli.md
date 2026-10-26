# 01: 来源目录与管理 CLI

**What to build:** 管理员可通过 CLI 查看、新增、编辑、启停 source，并导入导出 YAML；首次启动自带 OpenWrt 与 ImmortalWrt 默认目录。

**Blocked by:** foundation/02

**Status:** ready-for-agent

- [ ] 内嵌默认目录首次启动入库，镜像基名 + tag 模板（OpenWrt/ImmortalWrt 规则以实测 tag 为准）
- [ ] `/admin/api/sources` CRUD，`admin.api_token` 鉴权中间件
- [ ] `asu source list|add|edit|enable|export|import`，全部经 API
- [ ] 本地改动记录到 `overrides`
- [ ] 测试：导入导出往返一致；无 token 返回 401

Parent spec: `.scratch/sources-metadata/spec.md`
