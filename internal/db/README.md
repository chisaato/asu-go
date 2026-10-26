# SQLite 所有权与迁移

- `serve` 打开 `<data_dir>/asu.sqlite`，启动监听前执行内嵌 goose 迁移，关闭服务后释放连接与锁。
- `asu db migrate --data-dir <目录>` 使用同一入口离线迁移；服务运行时会立即返回数据库占用错误。
- 单连接、WAL、`busy_timeout=5000`、外键约束均在打开时启用。
- 使用同目录 `asu.sqlite.lock` 的非阻塞 Unix advisory `flock`；锁随文件描述符关闭或进程退出自动释放，锁文件本身保留，**不得在服务运行时删除**。
- 最小实现只保证遵循该入口的进程互斥；不阻止 sqlite 客户端等外部程序直接访问数据库。不支持 Windows 或网络文件系统，多个进程必须使用同一数据目录，不应复制或替换正在使用的数据库/锁文件。

驱动注册目前只保存描述符：queue/sqlite、store/local、stats/sqlite、runtime/docker 与 runtime/podman；不表示业务操作已经实现。完整接口与工厂由各后续 issue 在已知需求下补齐，未注册名称在配置校验时拒绝。
