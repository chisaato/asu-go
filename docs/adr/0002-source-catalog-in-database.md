# 0002 发行版 / source 目录以数据库为准

Status: accepted

## 背景
发行版 → ImageBuilder 镜像 → 版本信息若放 YAML，每次上游更新都要手写；未来还要支持自维护分支。

## 决策
SQLite 为运行时唯一事实来源；二进制内嵌默认目录并在首次启动写入；上游同步自动新增版本，不覆盖本地改动字段；自维护分支是 `origin=local` 的 source；YAML 仅用于 `asu source export/import`；一期通过 CLI（走 `/admin/api`）编辑。镜像引用 = 基名 + tag 模板（OpenWrt 与 ImmortalWrt 的差异仅在此）。

## 后果
一期只支持容器镜像形式的 ImageBuilder；“压缩包 URL 自建镜像”未决，待用户熟悉后再议。
