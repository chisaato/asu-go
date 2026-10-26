# 来源目录

SQLite 是唯一事实来源。`serve` 在 `sources` 为空时事务导入内嵌目录；启动不访问网络，不会补回单独删除的默认条目。默认版本取自 OpenWrt / ImmortalWrt 的 `.versions.json`（2026-10-27），后续发现新版本由元数据同步负责。

## CLI

所有管理命令只访问 HTTP，不打开 SQLite。`--server` 默认 `http://127.0.0.1:8000`。凭据优先级：显式 `--token`（包括空值）> `ASU_ADMIN_TOKEN` > 配置 `admin.api_token`。

```sh
export ASU_ADMIN_TOKEN='替换成安全的随机 token'
asu serve --data-dir /path/to/data
asu source list
asu source add --distribution custom --branch 25.12 --version 25.12.6 \
  --upstream-url https://downloads.openwrt.org \
  --image ghcr.io/openwrt/imagebuilder --package-manager apk
asu source edit 7 --tag-template '{target}-{version_tag}' --rolling=false
asu source disable 7
asu source enable 7
asu source export > sources.yaml
asu source import sources.yaml
asu source import - < sources.yaml
```

`add` 默认 `origin=local`、`enabled=true`、`rolling=false`、`package_manager=opkg`、`tag_template={target}-{version_tag}`。`edit` 只提交显式指定的 flag，可用 `--enabled=false` 等修改布尔字段。

导出是带显式 snake_case 键的 YAML 数组，包含完整目录字段（包括 `overrides`、`targets`、`synced_at`）。导入严格拒绝未知 YAML 键与多文档，按 `(distribution, version)` 原子 upsert，忽略外部 ID；匹配已有行时保留旧 ID、合并覆盖记录。导出后原样导入不会新增覆盖字段。修改已有行或新建本地条目时记录实际变化的可编辑字段。后续同步器必须跳过 `overrides` 中的字段；本 issue 不实现同步器。

## 管理 API

请求使用 `Authorization: Bearer TOKEN`；未提供或错误 token 返回 401。未配置 token 时管理端点返回 503，不允许空 token 放行。`admin.RequireAuth` 可组合 Bearer 与未来的 session cookie 验证器。

| 方法 | 路径 | 请求 / 响应 |
| --- | --- | --- |
| GET | `/admin/api/sources` | JSON 来源数组 |
| GET | `/admin/api/sources/:id` | JSON 单条来源 |
| POST | `/admin/api/sources` | JSON 来源对象；201 返回完整新条目 |
| PATCH | `/admin/api/sources/:id` | 部分字段对象；200 返回完整条目 |
| DELETE | `/admin/api/sources/:id` | 204；被引用的 jobs.source_id 置空 |
| POST | `/admin/api/sources/import` | JSON 来源数组；整批原子 upsert |

PATCH 允许目录身份、分支、channel、上游 URL、镜像基名、模板、包管理器、origin、rolling、enabled；禁止写入 ID、overrides、targets、synced_at 或 null。参数错误为 400、不存在为 404、重复来源为 409。

## 镜像规则

`ResolveImage(source, target)` 将 target 中 `/` 改为 `-`。模板可包含 `{target}`、`{version}`、`{version_tag}`，也可直接填固定 tag。镜像引用为 `image:展开后的模板`。

- OpenWrt：正式版和 rc 为 `v<version>`，SNAPSHOT 为 `master`，分支 SNAPSHOT 去掉 `-SNAPSHOT` 并加 `openwrt-`。与原版 `asu/util.py:get_container_version_tag` 一致。
- ImmortalWrt：正式版采用 `openwrt-<version>`，主线 `master`，分支 `openwrt-<branch>`。2026-10-27 查证 [Docker Hub API](https://hub.docker.com/v2/repositories/immortalwrt/imagebuilder/tags?name=mediatek-filogic&page_size=10)，真实标签包含 `mediatek-filogic-openwrt-25.12.2`、`mediatek-filogic-openwrt-25.12.1`、`mediatek-filogic-master`、`mediatek-filogic-openwrt-24.10`，另有 snapshot 别名。该查证不保证所有 target/rc 都存在；特殊版本应通过 `tag_template` 覆盖，实际镜像可用性由后续运行时准备阶段判断。
