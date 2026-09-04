# ProductFlow 文档

一份事实一个家。代码、测试和真实运行是最终证据；文档与它们冲突时改文档。

## 当前产品

| 文档 | 管什么 | 不管什么 |
|---|---|---|
| [`../CONTEXT.md`](../CONTEXT.md) | 领域词汇、权威边界、稳定不变量 | 页面教程、未实现方向 |
| [`PRD.md`](PRD.md) | 用户现在能做什么、非目标、成功标准 | 模块路径、部署命令 |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | 运行单元、代码所有权、数据流、测试锚点 | 未实现设想、操作教程 |
| [`USER_GUIDE.md`](USER_GUIDE.md) | 页面怎么操作、故障怎么处理 | 内部事务、模型表 |
| [`../AGENTS.md`](../AGENTS.md)、[`../go/AGENTS.md`](../go/AGENTS.md)、[`../web/AGENTS.md`](../web/AGENTS.md) | 怎么改这个仓库 | 产品愿景复述 |

`web/src/pages/HelpPage.tsx` 是 `USER_GUIDE.md` 的产品内投影，同一提交更新。

英文 `PRD.en.md`、`ARCHITECTURE.en.md`、`USER_GUIDE.en.md`、`ROADMAP.en.md` 是翻译，不是第二份规范。

## 还没做成

[`ROADMAP.md`](ROADMAP.md) 只写尚未存在、或尚未被真实验证的方向。已接线的能力写在 PRD / ARCHITECTURE / USER_GUIDE。仓库不再维护独立的 `specs/` 树。

[`audits/`](audits/) 保存跨层验收账本。未完成工作在 [`audits/tasks/`](audits/tasks/)：选一份指导，只读那一份。总账本只留合同与历史证据。生产可靠性合同见 [`audits/agent-production-readiness.md`](audits/agent-production-readiness.md)（实现切片已关闭）。

## 历史叙事

[`history/`](history/) 保存已经发生的开发线，不是当前能力声明。Agent 从自研 harness 到 Pi SDK、直播协议和写入所有权的时间线见 [`history/agent-runtime-timeline.md`](history/agent-runtime-timeline.md)。

## 历史决策档案

[`adr/`](adr/) 不是当前设计。改代码、改合同、做新功能时不要读它，也不要新写 ADR。当前事实只维护 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。下表只索引已有文件名。

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

[`agents/`](agents/) 只管 issue tracker、triage、domain 阅读约定。

## 写作规则

1. 当前事实写在 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。未验证方向写在 ROADMAP，不把已接线能力再抄一遍。
2. 规格不复述已交付合同。只写相对 PRD 多出来的能力、验收和不做。
3. 不再新增 ADR。已有 `adr/` 当档案；当前设计只维护 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。
4. 用户操作变化必须同时改 USER_GUIDE 和 HelpPage。
5. 实现敏感声明指向当前代码所有者或测试。
6. 主仓库不新增兼容层、双序列化或旧数据迁移；残留路径删除。见 [`../CONTEXT.md`](../CONTEXT.md) Mainline Scope。

`just docs-check` 校验索引、前端路由、code owner 路径和仓库内链接。规格目录若重新出现才检查状态标注。它不检查「同一句话是否写了六遍」。
