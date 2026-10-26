# 03: 取消与诊断重放

**What to build:** 管理员可取消任务，并能对已有任务或原始请求 JSON 发起诊断重放。

**Blocked by:** build-api/02, runtime-runner/03

**Status:** ready-for-agent

- [ ] `POST /admin/api/jobs/{hash}/cancel`：对外 `failed` + `detail: cancelled`
- [ ] `POST /admin/api/jobs/{hash}/replay`、`POST /admin/api/replay`、`asu replay <hash>|-f file`
- [ ] 重放：独立 id `rp_<ulid>`、`replay_of`、不去重/不缓存/不计统计，默认 verbose + keep_container，可覆盖 image_tag、pull_policy
- [ ] 失败记录：客户端 `expires_at` 后不可见，管理端保留到 `purge_at`（`diagnostics.retention` 默认 7d）
- [ ] 测试：同哈希重放不被去重拦截且原记录不被改动

Parent spec: `.scratch/build-api/spec.md`
