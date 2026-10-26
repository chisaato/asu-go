# 02: Runner 与日志流

**What to build:** 一次构建能被执行，阶段可识别，日志可用 `curl` 实时跟随，超时与取消能清理容器。

**Blocked by:** runtime-runner/01

**Status:** done

- [x] 构建脚本模板（`make info/manifest/image`）与 `##asu:stage=<name>` 标记解析；`verbose` 注入 `V=s`
- [x] `internal/stream`：RingBuffer、日志落盘、广播器
- [x] `GET /admin/api/jobs/{hash}/log?follow=1` 纯文本流
- [x] 超时、取消（Kill + 清理）
- [x] fake Runtime 单元测试 + alpine 集成测试覆盖阶段、kill、超时

验证：`make build && make test && make lint` 全部通过；`ASU_TEST_RUNTIME_DRIVER=podman ASU_TEST_RUNTIME_SOCKET=unix:///var/run/docker.sock go test -tags integration ./internal/runner/...` 通过。集成仅使用 alpine 模拟 make，不拉取真实 ImageBuilder。Docker 独立引擎未验证。

对接说明见 `internal/runner/README.md`；serve 暂用内存 JobSource，build-api/02 需替换为真实任务来源并共享 Hub。失败保留容器的 label/开关已实现，回收由 runtime-runner/03 负责。

Parent spec: `.scratch/runtime-runner/spec.md`
