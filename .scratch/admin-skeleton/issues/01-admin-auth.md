# 01: 管理端登录鉴权

**What to build:** 管理端用账号密码登录，`/admin/api/*` 受保护；公开兼容 API 保持匿名。

**Blocked by:** sources-metadata/01

**Status:** ready-for-agent

- [ ] `admin.users`（bcrypt）、登录/登出接口、签名 session cookie
- [ ] `/admin/api/*` 全量接入：session cookie 或 `admin.api_token` 二选一通过
- [ ] 测试：未登录 401；错误密码被拒；token 可用于 CLI

Parent spec: `.scratch/admin-skeleton/spec.md`
