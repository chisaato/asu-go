# 04: 迁移原版 package_changes 规则

**What to build:** 原版 ASU 在构建前会按目标版本对请求包名做历史修正（改名、拆分、合并、移除），保证旧设备升级时安装的包名在新版本仍然有效。build-api/02 有意未迁移这部分，需要补上，否则 owut/LuCI 升级老固件时可能因包名变化而失败。

**Blocked by:** build-api/02

**Status:** done

- [x] 对照 `/data/app-stacks/asu/asu/package_changes.py` 全量迁移规则（按 distro/version/target 条件），保持行为一致
- [x] 规则以数据驱动方式实现（便于对照与后续扩展），并明确与请求哈希的先后关系（哈希基于规范化后的原请求，还是修正后的包集合，需与 build-api/02 的约定一致并写进文档）
- [x] 表驱动测试：覆盖原版每条规则至少一个样例，以及版本边界
- [x] `docs/api-spec.md` 与 `internal/queue/README.md` 说明该行为

实现：24 条数据规则，均注释原版行号；24 个显式规则正例和逐规则条件边界，1681 个直接调用原版 Python 函数生成的黄金样例（含 92 个正例上下文及每个 profile），queue 测试覆盖哈希/原请求不变、实际 profile、差异包顺序、版本约束与修正后包统计。原版无 distro 限制，修正在哈希之后、差异包之前。`make build && make test && make lint` 全部通过。

Parent spec: `.scratch/build-api/spec.md`
