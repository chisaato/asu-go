# Spec: sources-metadata（来源目录与元数据）

Depends on: foundation
Read first: ADR 0002, `docs/tech-stack.md` §4.2–4.3, `docs/api-spec.md`

## 目标
数据库中的 source 目录 + 上游元数据同步 + 兼容的 `/json/v1/*` 元数据端点。

## 范围
- 表：`sources`，字段见 `docs/data-model.md`（含 branch、channel、overrides JSON、targets JSON）。
- 内嵌默认目录（OpenWrt、ImmortalWrt）首次启动导入；镜像引用 = 基名 + tag 模板（OpenWrt `ghcr.io/openwrt/imagebuilder:{target}-v{version}` 规则；ImmortalWrt `docker.io/immortalwrt/imagebuilder:{target}-{version}` 规则，以实测 tag 为准）。
- 同步：`.versions.json` / `.targets.json` 按 `metadata.refresh_interval` 刷新，新版本自动加行，不覆盖本地改动；`profiles.json`、包索引惰性拉取、磁盘缓存、条件请求、上游失败回退过期缓存。
- 24.10+/23.05.6+/SNAPSHOT 的 kmod 拆分合并索引逻辑，对照原版 `asu/util.py:488-695`、`asu/main.py:98-139`。
- 端点：`/json/v1/latest.json`、`branches.json`、`overview.json`、`{path}/index.json`、`{arch}-index.json`、`targets/{target}/{profile}.json`；`/api/v1/revision/...`、`/api/v1/latest`、`/api/v1/overview`。
- `/admin/api/sources` 的 CRUD + `asu source list|add|edit|enable|export|import`（鉴权见 admin-skeleton，此处先用 `admin.api_token`）。

## 验收
- 离线启动不失败；同步失败有告警日志并使用缓存。
- 同步不覆盖被本地修改的字段（测试）。
- 端点响应与原版逐项对照一致（用 `/data/app-stacks/asu` 的 fixtures/响应）。
