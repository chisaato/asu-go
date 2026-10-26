# 02: 版本与 target 同步

**What to build:** 后台自动刷新上游版本与 target，新版本自动成为 source，并提供 `/json/v1` 的版本类端点。

**Blocked by:** sources-metadata/01

**Status:** done

- [x] `.versions.json`、`.targets.json` 按 `metadata.refresh_interval` 刷新
- [x] 新版本自动加行；不覆盖 `overrides` 中的字段
- [x] `/json/v1/latest.json`、`branches.json`、`overview.json`、`/api/v1/revision/...`、`/api/v1/latest`、`/api/v1/overview`
- [x] 上游失败回退过期数据并告警；启动不强制同步成功
- [x] 响应与原版 `/data/app-stacks/asu` 逐项对照一致

实现与有意差异见 `internal/metadata/README.md`。验证：`make build && make test && make lint`；独立临时 data_dir 下的伪上游 `serve + curl` 全部六类端点；上游恒定 503 时服务仍启动、目录端点返回旧数据、日志告警且 revision 传递 503；真实 `https://downloads.openwrt.org` SNAPSHOT 同步及 revision 读取。测试覆盖新增版本、overrides、local 行保护、整块 targets 替换、版本/targets 失败保留旧数据、分发行版/镜像站地址、304、超时、周期刷新/取消、兼容 JSON 和重定向。

Parent spec: `.scratch/sources-metadata/spec.md`
