# 01: 产物存储与下载

**What to build:** 构建成功后产物落盘，客户端可通过 `/store` 下载。

**Blocked by:** build-api/02

**Status:** ready-for-agent

- [ ] `store.driver=local`：`{store_dir}/{request_hash}/`，路径穿越防护
- [ ] `GET|HEAD /store/{hash}/{file}`，`X-Content-Type-Options: nosniff`
- [ ] 构建完成路径接入 `get_archive` 取回产物并落盘
- [ ] 配置 `store.driver=s3` 时启动报错
- [ ] 路径穿越测试

Parent spec: `.scratch/store-ttl/spec.md`
