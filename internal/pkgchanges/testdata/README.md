# 原版 Python 黄金样例

`python-golden.jsonl` 的 1681 个期望值由 Python 3 直接调用原版 `apply_package_changes` 生成，不依赖 Go 规则表。

- 原文件：`asu/package_changes.py`，231 行；SHA-256：`077a66549271f8fe9e07eaaacac277eb7bcf47603b9caeb0b791e2476fb75823`。
- 为避免安装原版服务端依赖，仅将 `asu.build_request.BuildRequest` 的类型注解导入替换为 `types.SimpleNamespace`，其余模块原样加载（`importlib.util.spec_from_file_location`），未修改原文件。
- 从原版 AST 遍历版本、target、profile 分支，为每个 profile 和每个 target 枚举正例（92 个上下文）；追加错误 target/profile、版本边界、空列表、预装新增包及不同 distro。期望值全部调用原函数计算。
- 版本覆盖：22.03.9、23.05、23.05-SNAPSHOT、24.09.9、24.10、24.10.0-rc1、24.10-SNAPSHOT、25.11.9、25.12、25.12.0-rc1、25.12-SNAPSHOT、26.01.0、精确 SNAPSHOT 与小写 snapshot。
- 额外样例涵盖缺失/重复 auc、已有 owut、重复旧 kmod、重复英语翻译、改名后重复、重复旧前缀（原版 `replace` 替换所有出现）、正负包名与字符串比较陷阱 `24.9 >= 24.10`。

Go 测试离线读取固定样例，不需要 Python 或原版代码目录。
