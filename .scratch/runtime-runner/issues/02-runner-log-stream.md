# 02: Runner 与日志流

**What to build:** 一次构建能被执行，阶段可识别，日志可用 `curl` 实时跟随，超时与取消能清理容器。

**Blocked by:** runtime-runner/01

**Status:** ready-for-agent

- [ ] 构建脚本模板（`make info/manifest/image`）与 `##asu:stage=<name>` 标记解析；`verbose` 注入 `V=s`
- [ ] `internal/stream`：RingBuffer、日志落盘、广播器
- [ ] `GET /admin/api/jobs/{hash}/log?follow=1` 纯文本流
- [ ] 超时、取消（Kill + 清理）
- [ ] fake Runtime 单元测试 + alpine 集成测试覆盖阶段、kill、超时

Parent spec: `.scratch/runtime-runner/spec.md`
