# 任务：交付图与下载包的浏览器验收

状态：完成
类型：实现
认领者：主代理-canvas-0905-1647
认领于：2026-09-05T17:08:54+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

认领、验收和关闭遵循 [Issue 协议](../README.md)。

## 问题来源

[完整操作链核查](canvas-workflow-coverage.md) 确认已有 rendition/ZIP 实现与 Go 内容测试，浏览器没有选规格到下载文件的完整证据。不得重建另一套交付实现。

## 做成什么样

从当前生图结果在检查器选择交付规格，保存后创建或获取对应 rendition；验证结果尺寸、格式、源资产与节点当前原图身份不变。浏览器预览、单图下载、多选交付图导出 ZIP；解包核对真实文件、manifest、源图 lineage 与 SHA256。原图/未成功交付图混选不得静默被过滤或变成成功导出。生成/下载失败必须显示错误，不报告成功。

## 前置与并行

- 前置：现有 `delivery/http_test.go`、`export_archive_test.go` 合同及完成的操作链核查；可独立实施，不依赖模型涨分。
- 冻结输入：DeliverySpec 与 rendition job/asset 合同；不改生成图质量或 provider 参数。
- 运行资源：独立测试 DB/storage/浏览器，使用确定性生成源图；不得切图片组 provider、停止共享 worker、重建 dev DB。与本组运行恢复任务默认串行，专属产物 `/tmp/productflow-canvas-delivery-proof/`。
- 认领确认：本组主代理在运行恢复交付 `daa4672c` 后核对本包、现有 Agent/library 与图片任务占用和目标 diff，登记并复核。继承本组隔离栈 API 29392/Web 29393/Redis 16489 和临时 PG；使用已提交 `canvasWorkflow.ts` 夹具，不触碰其它组服务。无认领提交。

## 只改这些文件

- 本组独立 `web/e2e/` 交付图 spec/helper。
- `DeliveryRenditionPanel.tsx`、`deliveryRenditions.ts`、`chrome/image-explorer/` 及相邻测试：仅真实复现要求的修复，记录实际范围。
- 后端 `go/internal/delivery/` 只读起手；发现根因跨后端时由维护者补齐必要写入范围，不在前端兜底。
- 本任务、父账本、任务索引。
- `justfile`：仅新增 `web-e2e-canvas-delivery` 入口；平台组当前无该文件在途 diff。新 spec 使用系统 `unzip` 解析下载包，不新增依赖或另一个渲染器。

## 不要碰

Agent、图片池、imagesession、C0-C6、schema 或新交付设计。裁切实时预览等路线图增量不在本单。

## 现在代码在哪

Inspector → `DeliveryRenditionPanel` → api create/list/retry rendition → `go/internal/delivery`；Explorer `evaluateDeliveryExportSelection` → `api.downloadDeliveryExport` → export archive。现有 Go 测试核 manifest/lineage/hash，当前浏览器只验证过 C5 原图 bytes。

## 合同

DeliverySpec 不重新调用模型、不替换生成原图。每个导出文件追溯到已成功 job 和明确源图；完整导出失败不伪装成部分成功。

## 怎么验收

浏览器真实下载并解析文件与 ZIP；相关 Web test:run/lint/build 和 Go delivery 隔离 PG 回归；`just docs-check`、自审与单次交付提交。仅类型/按钮测试不能验收本单。

## 阻塞与交接

- 原因：无实现前置阻塞，执行前落实隔离运行环境。
- 解除条件：隔离环境就绪。
- 跟进者：工作流体验组主代理。
- 交接：测试进程结束；本组主代理保留隔离栈供资产/配方任务串行使用，共享 provider/worker/DB 未改。未改生产源码。

## 证据

- 命令 / 基线 / 结果：2026-09-05，交付基线 `daa4672c`，新增测试不改生产行为。`WEB_BASE_URL=http://127.0.0.1:29393 just web-e2e-canvas-delivery --output=/tmp/productflow-canvas-delivery-proof/accepted --trace=on`（进程传隔离专用管理员和设置凭据），2 passed，16.8s。
- 证据：accepted 目录内 Playwright traces、`delivery.png`、`original.png` 和 `delivery-export.zip`。浏览器点击详情竖图/场景横图，持久化 1200x1600/1600x1200 PNG，原 source id/hash 不变；交付图下载 PNG 头/尺寸验证，原图下载 hash 对照；两项 ZIP manifest 的 job/source/result/run lineage 和文件 SHA256 一致；混选原图禁用导出；提交/导出 503 显示错误且不下载假成功文件。
- Go：`bash scripts/with_dev_env.sh go test -C go ./internal/delivery -count=1 -v`，24 tests passed，1.555s，真实隔离 PG，无 skip。包括未完成/未知 job 拒绝、部分导出、损坏文件和重试冲突。Web 非 succeeded 选择拒绝由现有 deliveryExport 单测覆盖，不伪造失败 job 的不存在资产。
- Web：test:run 91 files / 647 tests passed，lint/build passed（已有大 chunk warning）；最终只修正测试中的下载原图标签。`just docs-check` / 本任务 diff check passed。
- 调试裁定：默认规格已自动创建对应 rendition，选择不同规格验证变更；等价预设通过 DeliverySpec 识别，验持久化规格，不要求保存未建模的 preset key。预览经可见关闭按钮退出。未因此改产品模型或放宽文件断言。
- 交付定位：随本任务提交。
- 审核者 / Issue 结果 / 业务门槛结果：主代理-canvas-0905-1647 自审通过，非独立审核；完成。原图下载与交付图/交付包操作链验收通过，不代表图片审美/保真质量过线。
