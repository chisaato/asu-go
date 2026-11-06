# ImmortalWrt 25.12.2 ImageBuilder 性能与 Makefile 命令审查

- **日期**：2026-11-06
- **定位**：早期本地探索，以及真实请求的远端隔离复现和命令级审查；各实验不混为同一基线
- **一手镜像**：`docker.io/immortalwrt/imagebuilder:mediatek-filogic-openwrt-25.12.2`
- **镜像 Digest**：`sha256:f36c2e730fe0a9cf821f95cf9c954e5990c34c13116b9efb9498cad381461130`
- **目标硬件与架构**：`mediatek/filogic` (Profile: `bananapi_bpi-r3`, Target Arch: `aarch64_cortex-a53`)
- **执行宿主**：AMD Ryzen 9 9950X, 91GB RAM, NVMe (XFS), Podman 5.8.9

---

## 最新结论：真实请求的慢点在 ABI 查询宏，而非实际解包

以下结论来自后续真实请求调查，优先于下文早期不同 profile 的探索性对照。

**2026-11-07 当前正式assets补丁补测**：同一远端宿主、原始规范化请求、固定镜像、等量冻结包种子，以AB/BA执行四轮。原版321.952/321.611秒，优化版105.317/107.735秒，平均耗时减少66.9%，速度约为原版3.02倍。实际回收的全部6个固件、manifest、profiles.json字节一致；SBOM动态字段及相应校验和清单不同。这是生产主进程Script的隔离复现，不含排队/镜像拉取，也不是完整服务端到端测速。详细控制、边界及证据见[构建重放与加速能力验证第8节](build-replay-acceleration-verification.md#8-远端真实请求实测当前补丁整体约3倍速度)，不要与下文历史原型或旧本地试次混为一组。

### 真实请求与远端复现

- 请求：`5f9a0c31c53f522b6869a3619dc8605491122f66c4ecaec428f7c492e766511f`，`cmcc_rax3000m`，最终安装 285 个包。2026-11-07 从远端原任务恢复的规范化 request 含95个包，保存的 `options.build_cmd_packages` 也为95项；已用本仓库 `Request.Hash()` 验证与原哈希一致。此前记录的“97个显式请求包”未由恢复记录证实，后续重放以完整 request 为准。
- 远端双路 Xeon Gold 6138。原始任务约 575 秒，包含镜像准备；manifest 约 249 秒，image 约 175 秒。
- 在远端独立临时容器重跑同输入，复用现有镜像，仅播种 282 个包文件、不播种索引，使用原生下载工具及生产构建网络。退出码 0，记录的总运行时间约 322 秒，manifest 约 146 秒、image 约 175 秒。
- 该次粗粒度打点中，两次 `package_install` 分别约 139 秒。**这是整个 make 目标耗时，不能解释为解包本身的耗时。**

### 命令级审查：为何一个安装目标需要两分钟

随后带诊断探针的独立试次测得如下数据。探针修改了计时和输出捕获路径，有额外开销，不能与上一次 139 秒直接相减。

| 操作 | manifest 阶段 | image 阶段 |
| --- | ---: | ---: |
| `FormatPackages` 宏展开 | 144.2267 秒 | 144.5401 秒 |
| 实际 `apk add --no-scripts` | 1.7219 秒 | 1.7654 秒 |
| `package_install` 目标合计 | 146.1308 秒 | 146.4814 秒 |

该试次的 `prepare_rootfs` 目标约 25.45 秒，`build_image` 目标约 6.60 秒。不能把前者全部归因于服务启用，也不能把后者全部视为单独的 SquashFS 压缩时间。

源码 `GetABISuffix` 对每个列表项分别执行一次 `$(APK) query --fields tags --match provides ...`，再筛选 `openwrt:abiversion`。`APK` 又经 `fakeroot` 包装。`FormatPackages` 在 `foreach` 中串行调用该宏，manifest 和 image 各展开一次。

逐项日志记录了两阶段共 264 次调用（每阶段 132 次），累计约 274.42 秒；8 次返回非空，涉及 `liblua`、`libuclient`、`libustream-openssl` 三种包名。其余查询返回空后缀，但这些查询的语义不能直接跳过；可优化的是重复初始化和重复查找的执行方式。宏总耗时还包含探针启动、结果处理等，不能与逐项时间重复加总。

### 已验证的优化线索与边界

- 诊断性查询结果缓存对照中，manifest 约 116 秒、image 约 45 秒，总计约 161.83 秒，退出码 0。它主要验证了列表内及跨阶段查询结果复用的收益，**不是已经完成并验证的全仓库批量查询实现**。
- 原始远端与该对照产物的 `sha256sums` 比对，仅 SBOM 条目不同，其余固件、manifest、profiles.json 条目相同。这不等于所有产物字节完全一致，也不替代设备升级冒烟验证。
- ABI 查询去重/缓存/批处理，以及复用已安装 rootfs，收益有重叠，不能将两个方案节省的完整安装目标耗时相加。
- 一次完整安装仍是必要工作，但本样本中实际 `apk add` 只需约 1.7 秒。应优先消除 ABI 宏的逐包重复命令，再评估是否值得改造 rootfs 复用。
- 单次查询中 fakeroot 包装自身开销的精确占比、进程等待位置仍需独立证据；不能用整机 iowait、频率快照或子进程 CPU 时间差直接认定 IPC/磁盘/CPU 根因。
- 另有独立最小用例证实 `FormatPackages` 的 `pkg_ver` 只在当前项含 `=` 时赋值、未在普通项清空，会把先前版本限制带到后续项。当前真实请求没有证实触发此问题；不应将最小用例扩大为公开 API 已可触发的结论。

### 证据位置

原始证据位于本机临时目录，可能被系统清理，不能当作仓库中永久可获取的附件：

- `/tmp/opencode/asu-real-repro/remote_rerun/`：远端驱动脚本、主进程脚本、打点 Makefile、阶段日志和系统采样。
- `/tmp/opencode/asu-real-repro/command_audit/`：`Makefile.audit`、`cmd_audit_build.log`、`audit_abi.log`、`audit_timing.txt`、`Makefile.opt`、`fast_abi.py`、`opt_build.log`、`opt_timing.txt`、`sha256sums.opt` 和 `test_bug.mk`。
- `/tmp/opencode/asu-real-repro/remote_sha256sums.txt`：历史远端校验和清单。

2026-11-07 复查时上述旧临时目录已不存在；已只读重新提取远端原任务的规范化 request、构建日志及任务元数据到 `/tmp/opencode/asu-replay-recovery-20261107/`（目录0700、请求文件0600）。新恢复数据不包含上述旧诊断试次的全部探针证据，也未重新执行构建。

业务代码和生产配置未因本研究修改。后续实现必须保持 ABI 后缀选择、版本限制、签名校验、自定义仓库隔离及 manifest/hash 兼容性。

---

以下为早期 `bananapi_bpi-r3` 本地探索记录，不能代表上述真实请求的远端耗时。

## 1. 实验性质与方法局限声明

1. **模拟环境非线上运行**：本报告测量均在基于官方容器镜像的独立临时容器中模拟运行，非 ASU-Go 线上服务进程的实时执行记录。
2. **单 Profile 单试次**：仅针对 `mediatek/filogic` 目标下的 `bananapi_bpi-r3`（默认包含 214 个 `.apk` 软件包）执行，未覆盖所有 target。
3. **组合变量对照，非独立正交 A/B**：优化试次同时调整了 `--cache-max-age 1440` 与 `REUSE_ROOTFS=1`，收益为组合效果，当前试次未做单一变量拆分隔离；且基线与优化试次启动前的本地索引初始状态存在差异，收益不可独立归因。优化对照降级为探索性对比。

---

## 2. 真实阶段日志与耗时对比

以下为两次单次连续测量的完整输出记录，原始日志保存在宿主机 `/tmp/opencode/asu-perf-audit/`。

### 2.1 原始基线试次日志（Baseline Run）

- **计时记录文件**：`/tmp/opencode/asu-perf-audit/baseline_timing.txt`
- **执行脚本**：`/tmp/opencode/asu-perf-audit/run_audit.sh`
- **各阶段原始输出**：`01_make_info.stdout`, `02_make_manifest.stdout`, `04_make_image.stdout`（退出码全为 0）

```text
INFO: exit=0, cost=0.0635s
MANIFEST: exit=0, cost=6.6975s
HASH: cost=0.0023s
IMAGE: exit=0, cost=8.9311s
TOTAL_PIPELINE: cost=15.6954s
```

*注：分阶段耗时之和为 $0.0635 + 6.6975 + 0.0023 + 8.9311 = 15.6944\text{s}$，与脚本外层实测总时长 $15.6954\text{s}$ 差异为 $0.0010\text{s}$，属于正常的 shell 调度开销。*

---

### 2.2 优化探索试次日志（Optimized Run）

- **计时记录文件**：`/tmp/opencode/asu-perf-audit/opt_timing.txt`
- **各阶段原始输出**：`opt_01_make_info.stdout`, `opt_02_make_manifest.stdout`, `opt_04_make_image.stdout`（退出码全为 0）

```text
OPT_INFO: exit=0, cost=0.0200s
OPT_MANIFEST: exit=0, cost=4.0613s
OPT_HASH: cost=0.0021s
OPT_IMAGE: exit=0, cost=4.9862s
OPT_TOTAL_PIPELINE: cost=9.0707s
```

*注：分阶段耗时之和为 $0.0200 + 4.0613 + 0.0021 + 4.9862 = 9.0696\text{s}$，与实测总时长 $9.0707\text{s}$ 差异为 $0.0011\text{s}$，属于正常的 shell 调度开销。*

---

### 2.3 两试次对比与阶段差值

| 阶段 | 基线耗时 (秒) | 优化试次耗时 (秒) | 差值 (秒) | 说明 |
| :--- | :---: | :---: | :---: | :--- |
| `make info` | 0.0635 | 0.0200 | -0.0435 | 元数据读取 |
| `make manifest` | 6.6975 | 4.0613 | -2.6362 | 包含依赖求解与解包安装（含索引状态差异） |
| `manifest hash` | 0.0023 | 0.0021 | -0.0002 | 本地文本 SHA-256 计算（输出均为 `469d33d41aeb`） |
| `make image` | 8.9311 | 4.9862 | -3.9449 | 跳过二次解包安装，直接进入固件封装 |
| **流水线总耗时** | **15.6954** | **9.0707** | **-6.6247** | **单次试次整体耗时减少 42.2%** |

*注：优化试次的具体修改逻辑属于探索性对照，未保留独立固化的全自动复现脚本，后续需在严格控制初始索引状态下进行独立正交 A/B 验证。*

---

## 3. 确凿源码结构证据：重复安装机制

通过检查镜像内 `/home/build/immortalwrt/Makefile`，证实了当前流程中存在的物理重复安装结构：

### 3.1 `make manifest` 的物理安装

在 `/home/build/immortalwrt/Makefile` 第 188 行：
```makefile
_call_manifest: FORCE
	rm -rf $(TARGET_DIR)
	mkdir -p $(TARGET_DIR) $(BIN_DIR) $(TMP_DIR) $(DL_DIR)
	$(MAKE) package_reload >/dev/null
	$(MAKE) package_install >/dev/null
ifeq ($(CONFIG_USE_APK),)
	$(OPKG) list-installed $(if $(STRIP_ABI),--strip-abi)
else
	$(APK) query --format json --fields name,version,$(if $(STRIP_ABI),tags) --installed '*' | \
		$(SCRIPT_DIR)/make-index-json.py -a $(ARCH_PACKAGES) -f apk --manifest -
endif
```
`make manifest` 并非离线模拟器，它在 `package_install` 中将全部 214 个包解压安装到了 `build_dir/.../root-mediatek`（`TARGET_DIR`）目录中，随后通过 `apk query --installed` 导出清单。

### 3.2 `make image` 的重复删除与重新安装

在 `/home/build/immortalwrt/Makefile` 第 176 行：
```makefile
_call_image: staging_dir/host/.prereq-build
	echo 'Building images for $(BOARD)$(if $($(USER_PROFILE)_NAME), - $($(USER_PROFILE)_NAME))'
	echo 'Packages: $(BUILD_PACKAGES)'
	echo
	rm -rf $(TARGET_DIR) $(TARGET_DIR_ORIG)
	mkdir -p $(TARGET_DIR) $(BIN_DIR) $(TMP_DIR) $(DL_DIR)
	$(MAKE) package_reload
	$(MAKE) package_install
	$(MAKE) -s prepare_rootfs
	$(MAKE) -s build_image
	$(MAKE) -s json_overview_image_info
	$(MAKE) -s checksum
```
`make image` 开头第一行即通过 `rm -rf $(TARGET_DIR)` 删除刚刚安装好的 rootfs，随后在 `package_install` 中重新执行一次完整的解包与安装。

---

## 4. 产物二进制一致性与实际验证命令

### 4.1 校验命令与对比方法

在宿主机上对基线试次产物与优化试次产物的 `sha256sums` 进行比对：

```bash
diff -u /tmp/opencode/asu-perf-audit/artifacts/sha256sums \
        /tmp/opencode/asu-perf-audit/artifacts_optimized/sha256sums
```

### 4.2 实际比对输出

```text
--- /tmp/opencode/asu-perf-audit/artifacts/sha256sums
+++ /tmp/opencode/asu-perf-audit/artifacts_optimized/sha256sums
@@ -6,6 +6,6 @@
 fe5ccc9cdd7459f215ef9797fd385e220d59271565d6dc0de3a25ef2f03d7645 *immortalwrt-25.12.2-469d33d41aeb-mediatek-filogic-bananapi_bpi-r3-snand-bl31-uboot.fip
 e9a4d8f711846096226d144e1bc5b338f06ce8db9122624e816abbcdeefb7a74 *immortalwrt-25.12.2-469d33d41aeb-mediatek-filogic-bananapi_bpi-r3-snand-preloader.bin
 36fe82727730b34b1eafc928947231fef360345a7364dba44b705f7043bd6e51 *immortalwrt-25.12.2-469d33d41aeb-mediatek-filogic-bananapi_bpi-r3-squashfs-sysupgrade.itb
-08f2d8f9be21baeb7e5f4cc8983e8ba1406bc32c8e8f87ce93af0283d9717801 *immortalwrt-25.12.2-469d33d41aeb-mediatek-filogic-bananapi_bpi-r3.bom.cdx.json
+927730212fdb4c0b6885beb2aa941731b093b98b1d7686be1440b750301a33da *immortalwrt-25.12.2-469d33d41aeb-mediatek-filogic-bananapi_bpi-r3.bom.cdx.json
 3e82b70b4122ecbe49446c9e52ef11cf7a484af89624fa4b6b463607ae18f207 *immortalwrt-25.12.2-469d33d41aeb-mediatek-filogic-bananapi_bpi-r3.manifest
 dd272605cf4057f675495d5dbff3c8128703aa7ae85e40230df4c20dcd39f06a *profiles.json
```

- **验证结论**：
  - `squashfs-sysupgrade.itb`（固件升级包）：`36fe8272...` 散列值完全一致；
  - `snand-bl31-uboot.fip`、`snand-preloader.bin`：散列值完全一致；
  - `manifest`、`profiles.json`：散列值完全一致；
  - 唯一差异为 `bom.cdx.json`（CycloneDX SBOM JSON 文件），经检查差异因内嵌实时生成时间戳所致。

---

## 5. 后续验证工作建议

1. **正交隔离实验**：针对 `--cache-max-age`（索引请求控制）与 `REUSE_ROOTFS`（解包安装复用）分别设计单变量测试，并在启动前统一保证相同的初始索引状态（冷索引 vs 预置索引），以精确归因各自的独立收益。
2. **线上 Runner 实测对接**：在 ASU-Go 实际调度环境中，通过真实 `PutArchive` / `GetArchive` 传输与真实 Job 链路验证耗时与稳定性。
