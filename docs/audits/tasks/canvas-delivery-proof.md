# 任务：交付图与下载包的浏览器验收

状态：开放
类型：实现
认领者：—
认领于：—
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无

认领、验收和关闭遵循 [Issue 协议](README.md)。

## 问题来源

[完整操作链核查](archive/canvas-workflow-coverage.md) 确认已有 rendition/ZIP 实现与 Go 内容测试，浏览器没有选规格到下载文件的完整证据。不得重建另一套交付实现。

## 做成什么样

从当前生图结果在检查器选择交付规格，保存后创建或获取对应 rendition；验证结果尺寸、格式、源资产与节点当前原图身份不变。浏览器预览、单图下载、多选交付图导出 ZIP；解包核对真实文件、manifest、源图 lineage 与 SHA256。原图/未成功交付图混选不得静默被过滤或变成成功导出。生成/下载失败必须显示错误，不报告成功。

## 前置与并行

- 前置：现有 `delivery/http_test.go`、`export_archive_test.go` 合同及完成的操作链核查；可独立实施，不依赖模型涨分。
- 冻结输入：DeliverySpec 与 rendition job/asset 合同；不改生成图质量或 provider 参数。
- 运行资源：独立测试 DB/storage/浏览器，使用确定性生成源图；不得切图片组 provider、停止共享 worker、重建 dev DB。与本组运行恢复任务默认串行，专属产物 `/tmp/productflow-canvas-delivery-proof/`。

## 只改这些文件

- 本组独立 `web/e2e/` 交付图 spec/helper。
- `DeliveryRenditionPanel.tsx`、`deliveryRenditions.ts`、`chrome/image-explorer/` 及相邻测试：仅真实复现要求的修复，记录实际范围。
- 后端 `go/internal/delivery/` 只读起手；发现根因跨后端时由维护者补齐必要写入范围，不在前端兜底。
- 本任务、父账本、任务索引。

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
- 交接：未认领，无进程与写入占用。

## 证据

- 命令 / 基线 / 结果：待执行。
- 交付定位：随本任务提交。
- 审核者 / Issue 结果 / 业务门槛结果：未验收。
