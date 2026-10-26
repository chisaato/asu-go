# 0001 使用强类型 YAML 配置，不引入 Viper

Status: accepted

## 背景
配置需要强类型访问、键名统一小写 snake_case、拼写错误可被发现。

## 决策
`yaml.v3` 严格解码（`KnownFields(true)`）到结构体；每个字段显式 `yaml:"snake_case"` 标签，测试校验标签匹配 `^[a-z][a-z0-9_]*$`；优先级 flag > `ASU_*` 环境变量 > YAML > 默认值。不引入 Viper。

## 后果
环境变量只覆盖少量常用项并显式映射。若出现多层合并或热加载需求，另写 ADR 评估 koanf。
