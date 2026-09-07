# ProductFlow 文档

一份事实一个家。代码、测试和真实运行说明当前行为；与文档冲突时，核对预期合同并区分实现缺陷和文档过时。只读审查报告差异，已授权的修复更新受影响的实现或活文档。

## 当前产品

| 文档 | 管什么 | 不管什么 |
|---|---|---|
| [`../CONTEXT.md`](../CONTEXT.md) | 领域词汇、权威边界、稳定不变量 | 页面教程、未实现方向 |
| [`PRD.md`](PRD.md) | 用户现在能做什么、非目标、成功标准 | 模块路径、部署命令 |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | 运行单元、代码所有权、数据流、测试锚点 | 未实现设想、操作教程 |
| [`USER_GUIDE.md`](USER_GUIDE.md) | 页面怎么操作、故障怎么处理 | 内部事务、模型表 |
| [`../AGENTS.md`](../AGENTS.md)、[`../go/AGENTS.md`](../go/AGENTS.md)、[`../web/AGENTS.md`](../web/AGENTS.md) | 怎么改这个仓库 | 产品愿景复述 |

`web/src/pages/HelpPage.tsx` 是 `USER_GUIDE.md` 的产品内投影。两处共享的用户操作内容变化时，在同一交付中同步受影响部分；只改文档错字、链接或帮助页未展示的内容，不要求改动前端代码。

英文 `PRD.en.md`、`ARCHITECTURE.en.md`、`USER_GUIDE.en.md`、`ROADMAP.en.md` 是翻译，不是第二份规范。

## 还没做成

[`ROADMAP.md`](ROADMAP.md) 是可自托管多商家 SaaS 的产品方向与正式版总纲，拥有竞品吸收、未来体验、隔离与经营要求、阶段出口和未决问题。其中现场实现只用于说明决策依据，不替代当前能力文档。已接线的能力写在 PRD / ARCHITECTURE / USER_GUIDE。仓库不再维护独立的 `specs/` 树。

[`audits/README.md`](audits/README.md) 索引商家平台、Agent 质量、Agent 自进化、图片质量、工作流体验、平台可靠性六组。**一组一份文档**；拆合时迁移有效内容与证据，不保留重复章程。业务组任务和用户独立交办任务共用 [`audits/tasks/README.md`](audits/tasks/README.md)，由该协议维护认领、提交和归档流程。当前会话直接处理的修复和只读调查无需建单。

## 历史叙事

[`history/`](history/) 保存已经发生的开发线，不是当前能力声明。Agent 从自研 harness 到 Pi SDK、直播协议和写入所有权的时间线见 [`history/agent-runtime-timeline.md`](history/agent-runtime-timeline.md)。

## 历史决策档案

[`adr/`](adr/) 保存历史决策，按历史追溯需要读取，不作为修改前置或当前设计依据，也不新增 ADR。当前事实维护在 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。下表只索引已有文件名。

| 文档 |
|---|
| `adr/0001-agent-draft-authority.md` |
| `adr/0002-canonical-product-images.md` |
| `adr/0003-schema-v2-workflow.md` |
| `adr/0004-legacy-v1-cutover.md` |
| `adr/0005-agent-workbench-ui.md` |
| `adr/0006-media-library-authority.md` |
| `adr/0007-pi-agent-runtime-boundary.md` |
| `adr/0008-free-canvas-agent-graph-authority.md` |
| `adr/0009-agent-canvas-sandbox.md` |
| `adr/0010-mainline-no-compatibility.md` |
| `adr/0011-go-vertical-slice-rewrite.md` |
| `adr/0012-gorm-command-writes.md` |
| `adr/0013-agent-live-journal-bff.md` |
| `adr/0014-canvas-document-cook.md` |
| `adr/0015-canvas-ports-run-queue.md` |
| `adr/0016-retire-python-backend.md` |
| `adr/0017-agent-full-journal-ui-protocol.md` |
| `adr/0018-agent-turn-write-ownership.md` |

## 协作元数据

[`agents/`](agents/) 管产品 GitHub Issues 与本地执行 issue 的分工、triage 和 domain 阅读约定。仓库内公共任务池是 [`audits/tasks/README.md`](audits/tasks/README.md)，详细证据留在任务；有关联业务组时，章程保留验收结论和证据链接。

## 写作规则

1. 当前事实写在 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。未验证方向写在 ROADMAP，不把已接线能力再抄一遍。
2. 规格不复述已交付合同。只写相对 PRD 多出来的能力、验收和不做。
3. 不再新增 ADR。已有 `adr/` 当档案；当前设计只维护 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。
4. 用户操作变化更新受影响的 USER_GUIDE 内容；与 HelpPage 共享的操作按上方投影规则同步。
5. 实现敏感声明指向当前代码所有者或测试。
6. 主仓库不新增面向退役范式的兼容层、双序列化或旧 JSON/V1/v2 迁移；残留路径删除。这**不**禁止首个稳定版起的经营数据 N→N+1（`schema.Apply` / `release-upgrade`），见 [`../CONTEXT.md`](../CONTEXT.md) Mainline Scope 与 [`../release/README.md`](../release/README.md)。
7. 整理或归档更新活跃索引和链接，保留历史结论、原始 run_id、采证基线与证据归属。历史文件可修复失效链接，但不改写当时的判断。删除范围外或他人的材料仍需明确授权。

`just docs-check` 校验索引、前端路由、code owner 路径、仓库内链接，以及内部 issue 元数据、看板同步和归档索引。规格目录若重新出现才检查状态标注。它不裁定验收证据是否充分，也不自动判断文件写入或运行资源冲突。
