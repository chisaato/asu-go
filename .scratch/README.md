# .scratch

本地 Markdown issue tracker，约定见 `docs/agents/issue-tracker.md`。

## 一期 spec 与依赖

foundation → (sources-metadata ∥ runtime-runner) → build-api → store-ttl → admin-skeleton（也依赖 foundation）

## Issues（共 16 个，状态均为 ready-for-agent）

| Spec | Issues |
| --- | --- |
| foundation | 01 skeleton → 02 sqlite-drivers |
| sources-metadata | 01 catalog-cli → 02 versions-targets-sync → 03 profiles-index-cache |
| runtime-runner | 01 runtime-drivers → 02 runner-log-stream → 03 takeover-scavenger |
| build-api | 01 request-hash；02 submit-poll；03 cancel-replay |
| store-ttl | 01 store-download → 02 ttl-cleaner |
| admin-skeleton | 01 admin-auth；02 nuxt-embed-pipeline（只依赖 foundation/01，可提前）；03 task-list-page |

跨 spec 依赖写在各 issue 的 `Blocked by`。可立即开始：foundation/01。foundation/01 完成后可并行：foundation/02、build-api/01、admin-skeleton/02。
