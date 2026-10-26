# 03: 取消与诊断重放

**What to build:** 管理员可取消任务，并能对已有任务或原始请求 JSON 发起诊断重放。

**Blocked by:** build-api/02, runtime-runner/03

**Status:** done

- [x] `POST /admin/api/jobs/{hash}/cancel`：对外 `failed` + `detail: cancelled`
- [x] `POST /admin/api/jobs/{hash}/replay`、`POST /admin/api/replay`、`asu replay <hash>|-f file`
- [x] 重放：独立 id `rp_<ulid>`、`replay_of`、不去重/不缓存/不计统计，默认 verbose + keep_container，可覆盖 image_tag、pull_policy
- [x] 失败记录：客户端 `expires_at` 后不可见，管理端保留到 `purge_at`（`diagnostics.retention` 默认 7d）
- [x] 测试：同哈希重放不被去重拦截且原记录不被改动

实现说明：新增已鉴权管理端 jobs 列表（status/分页）与详情、cancel/replay；CLI 沿用 source 的 HTTP 客户端及 token 优先级。普通执行与 Takeover 均支持取消，取消标记持久化，覆盖 context 注册前及执行完成竞争窗口。默认校验与公开请求相同，只有显式 `options.skip_limits` 放宽策略，仍校验模型与元数据。重放产物保存在独立 diagnostics 目录，不进入公开 store；未增加下载路由。现有 build-api/02 已在入队和完成时写入 purge_at，本次验证其失败记录保留与日志可见性。可选 CLI `--follow` 未实现。

验证：`make build && make test && make lint` 通过；`ASU_TEST_RUNTIME_DRIVER=podman ASU_TEST_RUNTIME_SOCKET=unix:///var/run/docker.sock go test -p 1 -tags integration ./internal/api ./internal/runner ./internal/runtime` 通过，包含重放失败容器保留及 Scavenger TTL 回收。首次跨包并行运行出现共享 socket 的 Scavenger 干扰，新增集成测试已限定扫描自身任务，建议真实引擎集成测试按包串行运行。

Parent spec: `.scratch/build-api/spec.md`
