# 任务：工作流体验完整操作链的实现与验收覆盖核查

状态：完成
类型：证据
认领者：主代理-canvas-0905-1647
认领于：2026-09-05T16:47:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：维护者按确证缺口发布独立实现或浏览器验收任务

任务合同以本文件为准；认领与关闭遵循 [Issue 协议](../README.md)。

## 问题来源

用户授权本会话负责工作流体验组完整落地。C0-C6 已有文稿权威交付，但本组还负责创建、图编辑、运行恢复和结果使用。当前没有贯穿这些操作的实现与证据矩阵；不能把文稿门通过当作全部体验验收。

## 做成什么样

按当前 USER_GUIDE 和产品实现核对关闭 Agent 后的创建、图操作、场景运行、失败重试、结果预览下载与资产复用。每项记录入口、请求、业务效果、已有测试及其实际断言，区分实现缺陷、证据缺口和未承诺的路线图功能。将结果汇入父账本，必要后续任务各自可验收，不重派 C0-C6。

## 前置与并行

- 前置：已归档的 canvas-inspector-midrun、canvas-c4-remainder、canvas-c4-run-controls；当前 USER_GUIDE 与 schema-v3 合同。
- 冻结输入：本任务只读生产源码及测试，不修改 Agent 题库、Skill、图片池、imagesession；以采证时 HEAD 和目标文件 diff 为准。
- 运行资源：静态调用链核查与不调用 provider 的现有确定性测试；不切共享 provider，不暂停 worker，不写共享业务 DB，不占图片组浏览器。原始测试日志放 `/tmp/productflow-canvas-coverage-0905-1647/`。
- 认领确认：用户授权本会话负责本组；本组主代理核对共享看板、四项保留认领任务的写入范围与资源后登记。与现有 eval、图片池及 imagesession 工作无写入交集；公共索引只整合本任务行，不提交其他认领记录。不创建认领提交。

## 只改这些文件

- 本文件、父账本 `docs/audits/canvas-test-system.md`。
- 公共任务索引及归档索引：仅本任务登记、关闭与已核实的后续任务。
- 源码只读；发现缺陷记录因果与复现依据，另发有界任务。

## 不要碰

- Agent、图片质量、平台可靠性的实现及在途任务。
- 全部路线图功能的一揽子实现；不引入新运行模型或另一个测试系统。

## 现在代码在哪

- `docs/USER_GUIDE.md` 第 2-4 节：商家当前操作合同。
- `web/src/pages/product-create/`、`web/src/pages/workbench/`：创建、画布、结果与资产操作入口。
- `web/src/lib/api.ts`、`go/internal/graph/`、`go/internal/product/`、`go/internal/media/`：按实际调用定位业务 owner，路径不存在时记录实际位置。
- `web/e2e/`、相邻前端测试与 Go HTTP/合同测试：覆盖证据。

## 合同

- 关闭 Agent 仍能编辑、运行、撤销与使用结果；保持 O1-O7 与 AR-01。
- 测试存在、测试本轮通过、浏览器真实路径验收分别记录。
- 未来产品功能不伪装成当前 bug；已知证据缺口不写成已验收。

## 怎么验收

- 父账本新增完整操作矩阵，每项有当前入口和对应测试或明确缺口。
- 对关键尚未归入文稿门的路径抽查调用链与断言；运行贴近这些边界的现有前端确定性测试，记录命令和结果。
- 所有已发现缺口有优先级、归属和可独立验收的后续条件。
- `just docs-check`、本任务 diff 检查、自审与单次交付提交。采证完成不等于完整操作链通过。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：主代理-canvas-0905-1647。
- 交接：无运行资源占用；当前仅登记。

## 证据

- 命令 / 日期 / 结果：2026-09-05，`pnpm --dir web test:run src/pages/product-create src/pages/workbench/canvas src/pages/workbench/chrome/image-explorer`，39 files / 274 tests passed，1.36s。命令输出由当前会话保存，无另行原始日志文件。未运行 Go PG、浏览器或 provider。
- 核查结果：父账本完整操作链矩阵记录创建、图编辑、文稿保存、场景运行、失败恢复、结果下载、交付包、资产复用和配方确认。未发现足以裁定生产 bug 的动态证据；确认场景运行无浏览器提交后断言，can retry 用例只检查按钮可见，delivery 无浏览器文件验收，recipe 浏览器只到预览/取消。按 P1 运行恢复、P1 结果交付、P2 资产/配方顺序补证据。
- 基线 commit：`1dccfb59a09802eab194ee0d568079c9c4c8b8e5`，目标业务源码和测试无 diff；其他组在途改动排除。
- 交付定位：随本任务提交。
- 文档校验：`just docs-check` 和本任务 `git diff --check` 通过；归档与后续单发布后复核。
- 审核者 / 结论：主代理-canvas-0905-1647 自审通过，非独立审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：有界核查完成；完整操作链仍部分完成，后续由 canvas-run-recovery-proof、canvas-delivery-proof、canvas-asset-recipe-proof 依次交付。C0-C6 既有通过结论不变。
