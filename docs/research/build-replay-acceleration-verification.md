# 构建重放与加速能力验证

日期：2026-11-07。本文先核查当前工作区源码与保留日志，随后经用户授权在远端独立容器完成真实请求的 APK ABI 补丁对照。未部署或修改生产服务；包含尚未提交的补丁。第8节为本次实测，其余方法建议不代表都已执行。

## 1. 结论

- 现有 `asu replay` 适合作为真实请求的诊断入口：每次新建 `rp_<ulid>`，绕过公开任务/结果去重，不必删除生产固件缓存。
- **重放不是环境快照，也不是全缓存关闭。** 它仍使用当前服务、source、镜像策略、包缓存、镜像站和内嵌补丁；在生产实例执行仍争用生产资源。
- 包缓存冷/热对照与 ABI 补丁开/关对照是两个实验。不能把“未打补丁且冷包缓存”对比“打补丁且热包缓存”，将全部差值归因于补丁。
- 早期本地集成测试曾被 USTC 索引下载失败阻断；后续远端真实请求对照已成功：两次原版平均321.78秒，两次优化版平均106.53秒，流水线耗时减少66.9%，速度约为原版3.02倍。四轮固件、manifest、profiles.json字节一致，详见第8节；不是完整服务端到端测速，也未覆盖其他设备/版本。

## 2. 重放究竟绕过什么

### 2.1 公开提交可能根本不构建

公开 `POST /api/v1/build` 对规范化请求计算 SHA-256；`client` 不参与哈希，换客户端名称不是强制重建手段。`Submit` 入队前与事务内两次查重：有效的 queued/running/done 任务可直接复用；queued/running 不因公开 TTL 到期而重复执行。

来源：`internal/buildreq/request.go:132-154`、`internal/queue/sqlite.go:106-126,172-202,313-340`。

公开 API 没有 `force`/`no_cache` 重建选项。失败或过期后可重新入队；管理员也可删除非活跃任务产物使其失效，但这会撤销公开可见性、删除产物及日志，不应为了测试加速去破坏生产缓存。活跃任务删除返回 409。

来源：`internal/queue/cleaner.go:213-235`。

### 2.2 诊断重放新建独立任务，但共享运行资源

`Replay` 读取保存的请求或直接接收请求 JSON，重新校验并创建新 ID：`rp_` 加 26 字符 ULID，共 29 字符，不调用公开 `Submit` 去重。已有记录须仍在管理保留期内（`purge_at`），只有历史 hash、没有请求记录时不能凭 hash 恢复请求。

来源：`internal/queue/diagnostics.go:44-146`。

诊断产物发布到 `{data_dir}/diagnostics/{id}`，不覆盖原任务；公开查询和下载屏蔽 `rp_`，重放不计公开请求/缓存命中及构建完成的包/失败统计。**这只是任务身份、公开结果和这些业务统计的隔离**，不代表队列、Runtime、网络、共享包缓存或主机资源隔离。

来源：`internal/queue/artifacts.go:149-155,211-225`、`internal/queue/sqlite.go:313-316,382`、`internal/queue/cleaner.go:238-245`。

默认 `verbose=true`、`keep_container=true`。后者不是成功也永久保留：成功或取消/超时会清理，普通失败时才可能保留，再由回收器管理。测速建议显式 `--no-keep-container`，但仍需隔离测试目录、控制并发与资源。

来源：`internal/queue/diagnostics.go:103-110`、`internal/runner/runner.go:184-208`。

## 3. 可用入口及缺口

源码：`internal/cli/replay.go:15-76`、`internal/admin/jobs.go:25-77`。

```bash
# 以下仅展示语法；执行会向指定实例提交真实任务。
# 管理凭据使用受保护配置或 ASU_ADMIN_TOKEN，不把 token 写进命令历史。
asu replay <历史任务ID> --server http://127.0.0.1:8888 \
  --pull-policy never --no-keep-container --verbose=true

asu replay -f /受保护目录/request.json --server http://127.0.0.1:8888 \
  --pull-policy never --no-keep-container --verbose=true
```

`-f -` 支持 stdin。CLI 返回新任务 ID 后即退出，不会等待完成，也没有 `--follow`；返回 ID 不等于构建成功。`--image-tag` 只覆盖 tag，不能当作镜像 digest 参数。

管理接口：

- `POST /admin/api/jobs/{id}/replay`：请求体可带 `options`，使用原任务保存的 request。
- `POST /admin/api/replay`：BuildRequest 字段与 `options` 同级，不是 `{"request": ...}` 包装。
- `GET /admin/api/jobs/{id}`：观察 status、request、options、result、stages、queued_ms、duration_ms。
- `GET /admin/api/jobs/{id}/log?follow=1`：跟随日志。
- `POST /admin/api/jobs/{id}/cancel`：取消任务。

重放选项没有包缓存开关、仓库镜像覆盖或 ABI 补丁开关。因此，**对同一个服务反复调用 replay，只能验证当前配置下的重复运行，不能自动形成补丁开/关 A/B。** 包缓存与 mirrors 需测试实例配置控制；已有 Runner 测试则使用受信任 `BuildScript` 接缝，仅跳过唯一补丁调用行，保持其余脚本相同。

来源：`internal/queue/diagnostics.go:21-40`、`internal/runner/apk_abi_integration_test.go:55-86`。

## 4. “同请求”不等于“同环境”

保存的是规范化的 BuildRequest，不是历史运行环境。重放保留包、版本约束、defaults、rootfs、仓库及公钥等请求字段，但还会重新校验当前元数据、解析 profile、读取当前 source，并使用当前二进制的执行包规则。

特别注意：重放入队会保存 `build_cmd_packages`，但 worker 构造 Job 后又调用 `executionPackages(r, profile)` 覆盖 `j.Packages`；不能将该历史 options 字段当作逐字重放命令。`DiffPackages` 还在容器脚本中结合镜像信息处理。

来源：`internal/queue/diagnostics.go:79-118`、`internal/queue/sqlite.go:446-487`、`internal/queue/package_changes.go:8-15`、`internal/runner/script.go`。

测量应记录：

- 完整请求的受保护副本与内容摘要、最终传入构建的包列表、manifest。
- 当前 source、实际镜像引用和解析后的不可变身份、服务版本/补丁资源摘要。
- mirrors、签名配置、仓库索引/包内容摘要，以及是否生成本地签名密钥。
- 包种子文件集合、内容哈希及播种数量/字节；主机、Runtime、并发和资源限制。

`--pull-policy never` 避免常规缺失镜像自动拉取，但不是所有情况下的绝对保护：rolling source 需要复查时，Runner 会将策略改成 `always`。基准应使用明确非 rolling、固定身份的测试 source/Job，并核对实际镜像；不要以换镜像 tag 作为开关补丁的方法。

来源：`internal/runner/runner.go:112-145`。本地签名密钥生成路径：`internal/runner/testdata/apk-abi/immortalwrt-25.12.2/Makefile:343-349`。

### 4.1 原真实请求与安全导出

研究记录中的原任务为 `5f9a0c31c53f522b6869a3619dc8605491122f66c4ecaec428f7c492e766511f`，ImmortalWrt 25.12.2、`cmcc_rax3000m`；见 [早期性能研究](immortalwrt-25.12.2-build-performance.md)。旧 `/tmp/opencode/asu-real-repro/` 已不存在，但2026-11-07 已从远端原任务的 SQLite 记录只读恢复完整规范化 request，存放于受保护的 `/tmp/opencode/asu-replay-recovery-20261107/request.json`。独立执行当前仓库 `Request.Hash()`，与原任务哈希严格一致，无需用户重新制造请求。

恢复请求实际含95个不重复包，`options.build_cmd_packages` 也是95项，`diff_packages=true`，无版本约束、自定义仓库、公钥或 defaults，文件系统为 squashfs。此前“97个显式包”的口径未由当前记录证实；后续必须使用恢复的完整输入，不按包数反推。单任务构建日志已恢复，但它没有完整客户端 JSON，不能独立替代 request。

若原服务仍保留该记录，可从管理任务详情导出 `.request`，或使用经授权的只读数据库查询；导出本身不必触发重放。优先只取目标 request，而不是克隆整库。

SQLite 使用 WAL，在线不能仅拷贝主数据库文件当作一致快照；需要完整克隆时采用 SQLite 在线备份或停机后的一致快照。注意现有 Runtime handle：测试实例连到同一引擎时可能存在接管/回收生产容器的风险，不能把生产 DB 原样当测试 DB 启动。

来源：`internal/db/db.go:36-56`、`internal/cli/serve.go`、`internal/runner/jobs.go`。

request 中的仓库 URL、defaults 等可能带秘密或隐私。原始实验输入应本地受保护保存、不打印、不提交 Git；分享时另外制作脱敏副本。**删除 repositories/keys 会改变构建和缓存行为，不再是原输入重放。** `repository_keys` 是公钥，不应错误称为私钥。需要管理凭据时使用私密凭据输入，不在聊天里收集。

## 5. 分开验证三种主张

### 5.1 包缓存是否有效

冷/热两次重放用于观察包缓存的收益：保持补丁、镜像、请求及仓库不变，只改变初始共享包状态。冷构建成功后，热构建应看到播种文件；还要检查实际下载，不能仅凭耗时下降认定完全免下载。

生产缓存只跨任务保留非空普通 `.apk/.ipk`，不共享索引；自定义 repositories 请求跳过此缓存，失败构建不回收。热包缓存也仍可能请求索引，不能承诺网络不稳定时必然成功或亚分钟完成。

来源：`internal/runner/cache.go:49-63,72-182`、`internal/runner/runner.go:333-339`。

mirrors 的替换标记只能证明替换动作，不能单凭它证明源更快或内容一致。测速镜像站需另做单变量网络实验，不能同时切站并开补丁。

### 5.2 补丁是否保持正确性

同镜像、同请求、同仓库内容、两个全新容器，分别仅关闭/打开补丁；两组都必须完整成功，优化组必须明确记录补丁已应用，不能把源码不匹配后的安全跳过算作成功验证。

比较非空 manifest、存在时的 profiles.json、全部支持的 sysupgrade 固件的文件集合和字节；SBOM 动态字段单独分析，不要求整个 sha256sums 文件字节相同。固件相同不替代真机升级冒烟。

现有 `TestAPKABIImageBuilderComparison` 已实现主要断言，但输入只有 `Packages=["base-files"]`，保留设备默认包，不是已恢复的真实请求。测试按 baseline→optimized 共用初始为空的 PackageCache，因此第二组可能受益于第一组下载，**只能作为正确性对照骨架，不能作公平补丁基准**。

来源：`internal/runner/apk_abi_integration_test.go:50-86,111-151,154-229`。

### 5.3 补丁是否真正加速

建议以已恢复的真实请求为主样本，正确性通过后运行等初始状态的配对基准：

1. 计时外准备固定 package-only 种子快照；每次从该快照等量播种，上一轮回收不得改变下一轮输入。使用冻结仓库服务端快照控制索引/包版本，或至少记录在线索引摘要，将内容漂移的样本剔出配对归因。
2. 每次新容器、空任务私有 ABI 缓存；不复用 rootfs 或 `.asu/patches/apk-abi/cache`。两组仓库获取/预置方式必须相同；测试用索引快照不能伪装成生产包缓存共享能力。
3. 两种模式分别预热并排除预热结果，再交错运行 AB、BA 等多对样本；固定资源和日志级别、控制其他负载。不能用清理全机页缓存影响他人任务。
4. 记录高精度总耗时、manifest/image 阶段、退出码、包下载与产物摘要。报告各配对差值、代表值及离散程度；不只选最快单次。
5. 如需区分收益来源，再加诊断模式：原始查询、仅去只读 fakeroot、去 fakeroot 加缓存。最终默认补丁整体 A/B 是首要证据，不必一开始扩成全组合实验。

`duration_ms` 是任务运行墙钟，不是 ABI 查询时间；stages 是阶段切换时间戳。脚本 `##asu:timing` 用 `date +%s`，只有整秒分辨率，也包含索引、求解、安装或打包。不要把阶段差直接解释为单条命令收益。

来源：`internal/queue/diagnostics.go:214-264`、`internal/runner/runner.go:22-25`、`internal/runner/script.go:60`。

## 6. 真实缓存命中还需要独立诊断

当前 query.py 没有命中计数器。已应用标记不代表发生命中；缓存 JSON 文件数不是命中次数。建议单独的测试诊断运行记录 hit/miss/fallback、实际 APK ABI query 调用数，以及上下文各组件摘要，放到任务私有诊断文件，通过 Runtime 取回，不写 stdout、不注入 rootfs。追踪会增加成本，不能带着逐次追踪数据声称是无扰动正式测速。

来源：`internal/runner/assets/apk-abi/query.py:142-196`。

key 包括 APK 可执行文件、环境（排除明确 make/输出项）、argv、cwd、root APK DB、keys、仓库配置及索引内容。真实 manifest 和 image 都先删除并初始化 rootfs，再在 apk add 前展开 ABI 查询；重建 DB/本地索引的内容与环境是否一致、索引扫描是否覆盖实际文件名，需要完整路径诊断，不能由静态 fixture 推断必然命中。

来源：`internal/runner/assets/apk-abi/query.py:19-22,79-138,174-177`、`internal/runner/testdata/apk-abi/immortalwrt-25.12.2/Makefile:176-198,230-259`。fixture 的两个阶段只重复调用 ABI 函数，没有真实删 rootfs、initdb、更新索引：`internal/runner/testdata/apk-abi/fixture.mk:9-11`。

即使跨阶段没有命中，只读查询去 fakeroot 仍可能有收益；反之，Python 启动和重复内容指纹也有成本。两者都不能仅凭源码估算最终倍数。

## 7. 早期验证记录与复用入口

保留的 `/tmp/opencode/asu-apk-abi-validation/` 可能被清理，不是永久附件：

- `summary.md`：上一轮默认测试、lint、Go build 通过。
- `setup-smoke.log`：真实 Runner 注入到 make info 通过，不等于完整固件验证。
- `apk-abi-integration.log:5-6`：baseline exit=1，约9秒；optimized 未运行。
- `apk-abi-3524230991/baseline/logs/apk-abi-3524230991.log:950-970`：manifest 阶段多个 USTC packages.adb 请求出现 wget error 8、unexpected end of file。不能凭这些行断言具体 HTTP 状态或 ABI 补丁故障。

以下为早期验证的复用入口；本次远端实验使用第8节的真实请求脚本驱动，没有重跑此小输入集成测试：

```bash
# 离线受控缓存/Makefile回归，不构建固件。
go test ./internal/runner -run '^TestAPKABI$' -count=1 -v

# 当前小输入完整正确性对照；会下载包并构建，不是真实请求或公平基准。
ASU_TEST_APK_ABI_IMAGE=docker.io/immortalwrt/imagebuilder:mediatek-filogic-openwrt-25.12.2 \
ASU_TEST_RUNTIME_DRIVER=podman \
ASU_TEST_RUNTIME_SOCKET=unix:///var/run/docker.sock \
go test -tags integration ./internal/runner \
  -run '^TestAPKABIImageBuilderComparison$' -count=1 -v -timeout 35m
```

真实请求输入、等量种子、AB/BA及产物对照已由第8节临时驱动完成。若需要持续回归，再考虑将该驱动收敛为测试工具，并增加独立命中诊断；暂不为了实验增加公开 API 或生产 replay 的大批新选项。

## 8. 远端真实请求实测：当前补丁整体约3倍速度

### 8.1 输入、隔离与计时边界

2026-11-07，经用户授权，在 `root@192.168.5.25` 的独立 Podman 容器串行执行原版/优化版/优化版/原版（AB/BA），没有调用生产构建 API、修改生产数据库或部署二进制。

- 原生 Go overlay 调用 `Request.Normalize/Hash`、队列 `executionPackages`、`runner.Script` 和实际 `inputs`/embed，哈希匹配第4.1节原任务。规范化与执行列表均为95个包、无迁移变化；`diff_packages=true`、`filesystem=squashfs`、`verbose=true`。实际 Makefile 命令最终包含105项（10个默认包负项），manifest为285行；这三种计数不能混用。
- 固定现有镜像 ID `7b2e471fb55a13b1e6c711c7189848369f65b87fae837b774e07a38808429659`，核对 RepoDigest 与早期记录相同，`--pull=never`；使用既有 `asu-build` 网络和相同 USTC 替换。独立容器没有 `managed-by=asu-go`/`asu.*` 管理标签，无宿主 bind mount，不使用 exec 或替换下载工具。
- 计时外只读复制生产包缓存为冻结种子：282个非空普通 `.apk/.ipk`，合计14,584,548字节。每轮全新容器，播种完全相同的包，不播种索引、rootfs或任务私有ABI缓存；四轮均没有新增或更改包文件。
- 预备容器生成本次专用本地签名密钥，四轮使用相同输入；不是生产私钥。四轮下载到的7份远程索引内容相同。本地 `packages.adb` 内容摘要仍不同，单列为残余差异，不能据此声称所有仓库字节完全相同，也不推断其原因。
- 两份生产脚本仅一行不同：原版跳过 `apply.py`，优化版执行当前四份生产 assets。主会话独立比对本地与远端回收的资源归档，确认它们与当前代码完全相同；两个优化组均明确记录补丁已应用，Makefile的应用后摘要与guard相符。

主要指标是远端宿主 `perf_counter_ns` 捕获主流水线开始/结束标记之间的耗时，覆盖生产 Script，不含镜像拉取、容器创建、归档注入、包播种、诊断采样、产物提取。容器内纳秒时间戳交叉核对与宿主计时相差不到0.001秒。创建约0.27–0.34秒，控制文件注入约3.71–3.89秒，播种约0.85–0.95秒，提取约2.46–3.10秒，均另记，不冒充服务端完整任务耗时。

这是主进程脚本的隔离复现，**没有覆盖生产排队、Runner完整编排和服务端产物发布**。没有单独执行预热试次、全机清页缓存或完全控制宿主其他负载；两对样本用于初步验证，不是统计充分的性能保证。

### 8.2 实测结果

| 顺序 | 模式 | 主流水线（秒） | manifest阶段（整秒） | image阶段（整秒） | 容器退出码 |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | 原版 A | 321.952335 | 146 | 176 | 0 |
| 2 | 优化 B | 105.316929 | 37 | 68 | 0 |
| 3 | 优化 B | 107.735389 | 38 | 69 | 0 |
| 4 | 原版 A | 321.610935 | 145 | 176 | 0 |

- 第一对耗时减少67.29%，速度比3.057；第二对耗时减少66.50%，速度比2.985。
- 原版均值321.781635秒，优化版均值106.526159秒，平均节省215.255476秒（约3分35秒），耗时减少66.8949%，速度比3.02068。
- 阶段秒数来自生产脚本，只有整秒精度；它们包含索引、求解、安装、rootfs及打包等工作，不能当作独立ABI查询耗时。

本次只验证了“去只读fakeroot包装＋安全查询缓存”当前补丁整体，没有分离两者各自贡献。两个优化组各生成190个缓存JSON且没有降级告警；文件数不是命中次数，也不能证明跨manifest/image缓存复用已发生。

### 8.3 正确性与证据

主会话从四轮实际导出的文件重新核对文件集合并逐字节比较：每轮12个产物，除SBOM与相应 `sha256sums` 外均相同，包括全部6个固件、manifest、profiles.json及 `build_cmd_packages`。SBOM差异仅 `/metadata/timestamp`、`/serialNumber`；不是“所有产物字节一致”。本次sysupgrade固件SHA-256为：

```text
dc7b40d3090820025c0140ab57cfdbceade948786507398a97397180c29bcce2
```

上述结果不替代真机升级冒烟。原始请求、完整日志及临时新生密钥按受保护数据处理，不提交Git。

本地证据根为 `/tmp/opencode/asu-remote-abi-ab-20261107/`，可能被清理，不是永久附件：

- `production-script.diff`、`export_queue_test.go`、`export_runner_test.go`：原生输入映射和唯一开关接缝。
- `evidence/remote_experiment.py`、`wrapper.sh`、`diagnostics.py`：运行、计时、采样及失败即停逻辑。
- `evidence/experiment-exit.json`、`results.json`、各轮 `timings.json`/`exit-state.json`/`main.log`：实验与四轮均退出0。
- 各轮 `artifacts/`：实际回收的固件文件；`comparison-*.json`、`analysis.json`：逐轮比较及计算。
- 各轮 `diagnostics-before.json`/`diagnostics-after.json`、`package-delta.json`：空初始私有缓存、等量包种子、远程索引及资源摘要。
- `evidence/commands.jsonl`、`cleanup-containers.jsonl`：精确执行命令、退出码与自有容器删除记录。

实验结束后已核对预备容器加四轮构建容器均不存在，远端任务私有根目录已删除（含本次生成的实验私钥）；`cleanup-final.json` 保留精确清单和生产容器原ID仍为running的只读快照。主会话另对四轮实际产物执行 `sha256sum --check sha256sums`，均退出0，并独立复算上述均值、比较四轮字节及剔除两个动态字段后的SBOM内容。生产服务未部署此次补丁。
