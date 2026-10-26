# 0004 CLI 管理命令一律走 admin API；SQLite 单进程所有者

Status: accepted

## 背景
多进程同时访问 SQLite 会出现锁冲突（已有踩坑经验）。

## 决策
只有 `asu serve` 打开数据库文件；`asu source ...` 等管理命令通过 `/admin/api` 与 `admin.api_token` 操作；`config check`、`version` 不依赖数据库，`db migrate` 仅在服务未运行时使用。

## 后果
管理命令要求服务正在运行；校验逻辑集中在服务端，远程管理也可用。
