# 0003 构建作为容器主进程，用 logs follow 读取，不用 exec + Hijack

Status: accepted

## 背景
原版用 `sleep` 主进程 + `exec_run`，输出被缓冲到进程结束；流式读取 exec 输出需要 Hijack 连接。

## 决策
创建容器 → `put_archive` 注入输入 → `start`（主进程是构建脚本）→ `logs?follow=1` 流式读取 → `wait` → `get_archive` 取产物 → 删除容器。不设 AutoRemove。阶段由脚本输出的 `##asu:stage=<name>` 标记确定。重启时从容器日志重放以实现接管。

## 后果
构建输出放容器可写层而非 tmpfs；日志驱动必须可读；该流程与 Kubernetes 的 `pods/log?follow=true` 语义一致。
