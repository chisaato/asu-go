# APK ABI 回归夹具

`immortalwrt-25.12.2/` 内三个文件为本地现有 ImageBuilder 原始源码副本，未修改。

- 镜像：`docker.io/immortalwrt/imagebuilder:mediatek-filogic-openwrt-25.12.2`
- Image ID：`7b2e471fb55a13b1e6c711c7189848369f65b87fae837b774e07a38808429659`
- Manifest digest：`sha256:f36c2e730fe0a9cf821f95cf9c954e5990c34c13116b9efb9498cad381461130`
- 相对源码路径：`Makefile`、`include/rootfs.mk`、`rules.mk`

`fixture.mk` 使用真实 `apk`、`APK`、`FAKEROOT`、`GetABISuffix` 定义，通过 GNU make 展开命令；仅可执行文件替换为记录 argv/环境的 fake。默认回归验证补丁后缓存复用、安装及 installed 查询仍经过 fakeroot。

设 `APK_ABI_TEST_UNPATCHED=1` 仅用于红色基线：不应用补丁，跨两次 make 的正负查询执行四次，缓存断言应失败。性能断言只计调用次数，不依赖耗时阈值，不代表实际构建加速测量。
