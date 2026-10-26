# 01: 管理端登录鉴权

**What to build:** 管理端用账号密码登录，`/admin/api/*` 受保护；公开兼容 API 保持匿名。

**Blocked by:** sources-metadata/01

**Status:** done

- [x] `admin.users`（bcrypt）、登录/登出接口、签名 session cookie
- [x] `/admin/api/*` 全量接入：session cookie 或 `admin.api_token` 二选一通过
- [x] 测试：未登录 401；错误密码被拒；token 可用于 CLI

## 实现与验证

- 新增 `admin.session_secret`、`admin.session_ttl`（默认 24h）与 `asu admin hash-password`；密钥为空时随机生成并警告。
- HMAC-SHA256 签名 cookie，常量时间验签；cookie 写请求必须带 `X-Requested-With: asu-admin`，Bearer 不要求该头。
- 登录失败按直连 IP + 用户名限制为每 5 分钟 5 次，内存表容量 10000，并发计数、惰性过期回收。
- 明确覆盖 sources-metadata/01 的旧行为：空 `admin.api_token` 仅禁用 token 路径，无有效凭据返回 401，不再整体 503；已有测试已同步。
- `make build && make test && make lint` 通过；实际 `serve` + curl 验证 login → me → sources → logout，登出后 me 返回 401。
- 公开兼容 API 尚未实现；测试通过占位 handler 验证 `/api/v1`、`/json/v1`、`/store` 路由不受管理认证影响，不声称已完成真实兼容 API 端到端验证。
- 签名 cookie 无服务端撤销表：登出清除当前浏览器 cookie，已复制的 cookie 在过期前仍有效；轮换 session_secret 可统一失效。

Parent spec: `.scratch/admin-skeleton/spec.md`
