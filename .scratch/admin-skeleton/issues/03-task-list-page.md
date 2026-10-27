# 03: 登录页与任务列表

**What to build:** 管理员在 `/admin` 登录后能看到任务列表（状态、阶段、时间）。

**Blocked by:** admin-skeleton/01, admin-skeleton/02, store-ttl/01

**Status:** done

- [x] 登录页对接 admin-auth
- [x] 任务列表页对接 `/admin/api` 任务查询接口（分页/状态筛选）
- [x] 未登录自动跳转登录页
- [x] 不做日志终端、SSE、手动构建器（二期）

## 实现与验证

- `/admin/login`：用户名密码表单，401/429 中文提示；全局守卫保护其他页面，提供登出。
- `/admin/`：Nuxt UI Table、状态筛选、每页 20 条、手动刷新、可关闭的 5 秒活跃任务轮询；详情抽屉只读展示 JSON 与阶段时间线。
- `make build && make test && make lint` 通过；前端 10 个单元测试覆盖 API 的 CSRF/401 行为与时间、耗时、JSON 格式化。
- 实际启动 `bin/asu serve`（临时数据目录、独立账号与会话密钥、Podman socket），curl 验证 `/admin/`、SPA 回退、入口引用的 JS/CSS 均为 200；匿名 `/me` 为 401，cookie 登录后 `/me` 与分页任务列表为 200。
- OpenChamber 当前客户端仅支持展示页面，不支持驱动浏览器，未完成浏览器交互验证；未新增浏览器测试依赖。

Parent spec: `.scratch/admin-skeleton/spec.md`
