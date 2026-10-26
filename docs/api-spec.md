# ASU-Go API 兼容与扩展规范

本文档定义 ASU-Go 的 HTTP 接口规范，分为两大板块：
1. **客户端兼容 API**：严格遵循 OpenWrt 官方 ASU 协议，确保 LuCI、owut、Firmware Selector 无缝对接。
2. **管理与可观测性 API**：ASU-Go 新增的管理后台与流式监控接口。

---

## 1. 客户端兼容 API (OpenWrt Standard)

### 1.1 元数据与概要接口

#### `GET /json/v1/latest.json`
- **说明**：获取当前最新的稳定版与维护版本列表。
- **响应示例**：
  ```json
  {
    "latest": ["24.10.0", "23.05.5"]
  }
  ```

#### `GET /json/v1/overview.json`
- **说明**：返回系统概览，包含各分支（branches）及其所支持的目标架构（targets）和服务器配置。
- **响应示例**：
  ```json
  {
    "latest": ["24.10.0", "23.05.5"],
    "branches": {
      "24.10": {
        "name": "24.10",
        "versions": ["24.10.0"],
        "targets": ["ath79/generic", "mediatek/filogic", "x86/64"]
      }
    },
    "upstream_url": "https://downloads.openwrt.org",
    "server": {
      "version": "1.0.0",
      "allow_defaults": false,
      "max_custom_rootfs_size_mb": 0
    }
  }
  ```

#### `GET /json/v1/branches.json`
- **说明**：以数组形式返回所有支持的分支对象。

#### `GET /api/v1/revision/{version}/{target}/{subtarget}`
- **说明**：获取指定版本与目标的源码修订号（revision code）。
- **响应示例**：
  ```json
  {
    "revision": "r27429-b695d7747e"
  }
  ```

---

### 1.2 固件构建接口

#### `POST /api/v1/build`
- **说明**：提交固件构建请求。若当前请求的镜像已有缓存，直接返回 200 及元数据；若任务正在构建或排队，返回 202；若无缓存，则放入构建队列并返回 202。
- **请求体格式 (JSON)**：
  ```json
  {
    "distro": "openwrt",
    "version": "24.10.0",
    "target": "mediatek/filogic",
    "profile": "glinet_gl-mt3000",
    "packages": ["luci", "luci-app-attendedsysupgrade"],
    "packages_versions": {},
    "diff_packages": true,
    "filesystem": "squashfs",
    "rootfs_size_mb": 0,
    "defaults": "",
    "repositories": {},
    "repositories_keys": [],
    "client": "luci/24.10"
  }
  ```
- **核心计算逻辑 (Request Hash)**：
  - 基于请求的核心参数（`distro`, `version`, `version_code`, `target`, `profile`, `packages`（去 `+` 前缀、去重、排序）, `packages_versions`, `diff_packages`, `filesystem`, `defaults`, `rootfs_size_mb`, `repositories`, `repository_keys`, `repositories_mode`）转为**规范 JSON**（键排序、无多余空白、null 与空值显式区分）后计算 SHA-256，输出 64 位小写十六进制作为 `request_hash`。
  - **不复刻原版 Python 的哈希拼接**（依赖 `str(list)`/`str(dict)` 的 repr）：客户端只把 `request_hash` 当不透明标识使用，且不与原版共享缓存。
  - **历史包名修正在哈希之后执行**：`internal/pkgchanges` 全量迁移原版规则，在 queue 组装实际构建 Job 时按版本、target、已解析 profile 应用，再由 Runner 计算 `diff_packages`；不改 `jobs.request` 或请求哈希，与原版先哈希、后修正、再差异包的顺序一致。没有 distro 限制；23.05/24.10/25.12 使用原版版本前缀条件，SNAPSHOT 移除仅匹配精确值，语言包阈值保留原版字符串比较。非空 `packages_versions` 的键优先选包，但修正不改其版本约束（旧包版本约束可能无法通过 manifest 校验）。最终响应的 `build_cmd_packages` 是修正后的实际命令包列表。
  - 同哈希重复提交不入队，直接返回已有任务状态；排队数超过 `max_pending_jobs`（默认 200）返回 **529**；用户 Kill 的任务对外表现为 `failed`（`detail: cancelled`）。
  - 响应码与头沿用原版：排队/运行中 202，成功 200，失败 500；`X-Imagebuilder-Status`、`X-Queue-Position`。

#### `GET /api/v1/build/{request_hash}` (支持 HEAD)
- **说明**：轮询构建任务进度或获取已完成的构建结果。
- **响应头规范**：
  - `X-Imagebuilder-Status`: `queued` | `container_setup` | `validate_manifest` | `building_image` | `signing_images` | `done` | `failed`
  - `X-Queue-Position`: 当前在队列中的排队位置（数字文本）
- **状态响应**：
  - **排队中 (HTTP 202)**:
    ```json
    {
      "status": 202,
      "detail": "queued",
      "queue_position": 2,
      "request_hash": "a1b2c3d4..."
    }
    ```
  - **构建中 (HTTP 202)**:
    ```json
    {
      "status": 202,
      "detail": "started",
      "imagebuilder_status": "building_image",
      "request_hash": "a1b2c3d4..."
    }
    ```
  - **构建完成 (HTTP 200)**:
    ```json
    {
      "status": 200,
      "detail": "done",
      "id": "glinet_gl-mt3000",
      "bin_dir": "a1b2c3d4...",
      "images": [
        {
          "name": "openwrt-24.10.0-mediatek-filogic-glinet_gl-mt3000-squashfs-sysupgrade.bin",
          "type": "sysupgrade",
          "sha256": "..."
        }
      ],
      "manifest": {
        "base-files": "1620-r27429",
        "luci": "git-24.321.65432"
      }
    }
    ```
  - **构建失败 (HTTP 500)**:
    ```json
    {
      "status": 500,
      "detail": "Selected packages exceed device storage",
      "stderr": "..."
    }
    ```

---

### 1.3 产物下载接口

#### `GET /store/{request_hash}/{file_name}`
- **说明**：下载编译好的固件、sha256sums、manifest 或 profiles.json。
- **响应头**：
  - `Content-Type: application/octet-stream`
  - `X-Content-Type-Options: nosniff`

---

## 2. ASU-Go 增强管理与实时可观测性 API

### 2.1 实时日志流推送 (SSE)

#### `GET /api/v1/build/{request_hash}/stream`
- **协议**：Server-Sent Events (`text/event-stream`)
- **说明**：建立长连接，实时接收构建容器内部产生的每一行终端输出。若任务已完成，推流发送完已有日志后主动发送 `event: end`。
- **事件格式**：
  ```text
  event: stage
  data: {"stage":"validate_manifest","timestamp":1730000001}

  event: log
  data: {"stream":"stdout","line":"Collecting package metadata..."}

  event: log
  data: {"stream":"stderr","line":"WARNING: Some optional package ignored"}

  event: progress
  data: {"percent":65,"step":"Compiling rootfs"}

  event: finish
  data: {"status":"success","duration_ms":42500}
  ```

---

### 2.2 任务监控与运维管理接口 (`/admin/api`)

#### `GET /admin/api/jobs`、`GET /admin/api/jobs/{id}`
- **列表参数**：`status=all|queued|running|done|failed|finished`，`page` 默认 1，`page_size` 默认 20、最大 200；响应 `{total,items}`，`finished` 包括 done 与 failed。
- **详情**：返回 `id`、`replay_of`、规范化 `request`、`result`、`stages`、`options`、`error`、任务状态和时间戳，不复用公开轮询响应体；按 `purge_at` 而非 `expires_at` 保留。

#### `POST /admin/api/jobs/{id}/cancel`
- **行为**：queued 立即转为 failed；running 取消执行 context 并等待 Runner 完成 Kill/Remove。返回 202，任务公开轮询最终为 HTTP 500、`detail: cancelled`，不新增状态值。已结束返回 409，不存在返回 404。重启接管的任务同样支持取消，取消始终清理容器。

#### `GET /admin/api/tasks`
- **查询参数**：
  - `status`: `all` | `queued` | `running` | `finished` | `failed`
  - `page`: 页码（从 1 开始，默认 1）
  - `page_size`: 每页条数（默认 20）
- **响应示例**：
  ```json
  {
    "total": 128,
    "items": [
      {
        "request_hash": "a1b2c3d4...",
        "version": "24.10.0",
        "target": "mediatek/filogic",
        "profile": "glinet_gl-mt3000",
        "status": "running",
        "stage": "building_image",
        "queued_at": "2026-10-27T02:00:00Z",
        "started_at": "2026-10-27T02:00:02Z",
        "duration_seconds": 45,
        "client": "luci/24.10"
      }
    ]
  }
  ```

#### `GET /admin/api/tasks/{request_hash}`
- **说明**：获取单次构建任务完整详情，包含所有选包列表、执行命令参数、阶段耗时切片和完整 stdout/stderr。

#### `POST /admin/api/tasks/{request_hash}/cancel`
- **说明**：主动终止执行中或排队中的任务，底层立即向容器发送 SIGKILL，并级联清理临时文件与容器实例。

#### `POST /admin/api/tasks/{request_hash}/retry`
- **说明**：清理失败状态并重新将该 Hash 入队进行构建。

#### `POST /admin/api/jobs/{request_hash}/replay`、`POST /admin/api/replay`
- **说明**：诊断重放。前者复用库中已有任务的 `request`；后者直接提交原始 BuildRequest JSON。请求体可选 `options`：`verbose`（默认 true）、`keep_container`（默认 true）、`image_tag`、`pull_policy`。
- **校验**：默认与公开提交相同；仅管理端 `options.skip_limits=true` 可显式绕过 defaults、大小与仓库白名单策略限制，仍保留字段类型/格式与发行流、target、profile 校验。`options` 与原始 BuildRequest 字段在同一 JSON 对象内；从已有任务重放时可提交 `{ "options": {} }` 或空请求体。
- **行为**：创建独立 id（`rp_<ulid>`）的任务，`replay_of` 指向原哈希；不去重、不写公开缓存、不计统计。返回新任务 id，之后用 `GET /admin/api/jobs/{id}`、`.../log?follow=1` 观察。
- **隔离**：直接提交时 `replay_of` 为请求规范哈希；已有任务重放时为来源任务 id。公开 GET/HEAD 始终返回 404；成功诊断产物位于独立 `diagnostics/{id}`，不进入公开 `/store` 命名空间。

#### `DELETE /admin/api/store/{request_hash}`
- **说明**：手动从磁盘删除指定 Hash 的固件产物与日志。

#### `GET /admin/api/system/status`
- **说明**：获取服务器系统健康状况。
- **响应示例**：
  ```json
  {
    "uptime_seconds": 86400,
    "runner": {
      "engine": "podman",
      "engine_version": "5.8.0",
      "socket_path": "/run/user/1000/podman/podman.sock",
      "active_containers": 2,
      "max_concurrency": 4
    },
    "storage": {
      "path": "/data/asu-store",
      "total_bytes": 107374182400,
      "used_bytes": 12884901888,
      "cached_images_count": 34
    }
  }
  ```
