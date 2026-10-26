# Spec: store-ttl（产物存储与 TTL）

Depends on: build-api
Read first: `docs/architecture.md` §3.5

## 目标
本地产物存储、下载服务与过期清理。

## 范围
- `store.driver=local`：`{store_dir}/{request_hash}/`，路径安全检查（防穿越）。
- `GET/HEAD /store/{hash}/{file}`；`X-Content-Type-Options: nosniff`。
- TTL：带 defaults 30m、带 `packages_versions` 7d、其余 24h、失败记录 1h（均可配置）。
- TTL Cleaner：按完成时间 + TTL 回收产物目录、日志、数据库记录；启动时跑一次。
- 删除接口 `DELETE /admin/api/store/{hash}`。
- 配置了 `store.driver=s3` 时启动报错。

## 验收
- 路径穿越测试；TTL 过期回收测试（可注入时钟）；重启后未过期产物仍可下载。
