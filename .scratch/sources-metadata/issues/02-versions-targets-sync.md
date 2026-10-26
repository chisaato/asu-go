# 02: 版本与 target 同步

**What to build:** 后台自动刷新上游版本与 target，新版本自动成为 source，并提供 `/json/v1` 的版本类端点。

**Blocked by:** sources-metadata/01

**Status:** ready-for-agent

- [ ] `.versions.json`、`.targets.json` 按 `metadata.refresh_interval` 刷新
- [ ] 新版本自动加行；不覆盖 `overrides` 中的字段
- [ ] `/json/v1/latest.json`、`branches.json`、`overview.json`、`/api/v1/revision/...`、`/api/v1/latest`、`/api/v1/overview`
- [ ] 上游失败回退过期数据并告警；启动不强制同步成功
- [ ] 响应与原版 `/data/app-stacks/asu` 逐项对照一致

Parent spec: `.scratch/sources-metadata/spec.md`
