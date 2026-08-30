# ProductFlow 文档

一份事实一个家。代码、测试和真实运行是最终证据；文档与它们冲突时改文档。

## 当前产品

| 文档 | 管什么 | 不管什么 |
|---|---|---|
| [`../CONTEXT.md`](../CONTEXT.md) | 领域词汇、权威边界、稳定不变量 | 页面教程、未实现方向 |
| [`PRD.md`](PRD.md) | 用户现在能做什么、非目标、成功标准 | 模块路径、部署命令 |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | 运行单元、代码所有权、数据流、测试锚点 | 未实现设想、操作教程 |
| [`USER_GUIDE.md`](USER_GUIDE.md) | 页面怎么操作、故障怎么处理 | 内部事务、模型表 |
| [`../AGENTS.md`](../AGENTS.md)、[`../go/AGENTS.md`](../go/AGENTS.md)、[`../backend/AGENTS.md`](../backend/AGENTS.md)、[`../web/AGENTS.md`](../web/AGENTS.md) | 怎么改这个仓库 | 产品愿景复述 |

`web/src/pages/HelpPage.tsx` 是 `USER_GUIDE.md` 的产品内投影，同一提交更新。

英文 `PRD.en.md`、`ARCHITECTURE.en.md`、`USER_GUIDE.en.md`、`ROADMAP.en.md` 是翻译，不是第二份规范。

## 还没做成

[`ROADMAP.md`](ROADMAP.md) 只写尚未存在、或尚未被真实验证的方向。已接线的能力写在 PRD / ARCHITECTURE / USER_GUIDE。仓库不再维护独立的 `specs/` 树。

## 已接受决策

[`adr/`](adr/) 正文冻结。被取代的部分在状态行标明后继 ADR，不把旧 ADR 改写成今天的实现。

| 文档 | 状态 | 何时读 |
|---|---|---|
| `adr/0001-agent-draft-authority.md` | Accepted；直播 journal 见 0013 | PostgreSQL 权威、unknown、全局库 Draft |
| `adr/0002-canonical-product-images.md` | Accepted | 媒体身份、商品图片、封面、lineage |
| `adr/0003-schema-v2-workflow.md` | Historical；在线图见 0008 | GenerationSpec / DeliverySpec / 一层分组仍有效 |
| `adr/0004-legacy-v1-cutover.md` | Superseded by 0010 | 原 V1 归档闸门；主线不再执行 |
| `adr/0005-agent-workbench-ui.md` | Accepted；直播路径见 0013 | 工作台交互与工具步骤投影 |
| `adr/0006-media-library-authority.md` | Accepted；§7 superseded by 0010 | 全局图库、子图库、来源生命周期 |
| `adr/0007-pi-agent-runtime-boundary.md` | Accepted；耐久 gate 见 ROADMAP | Pi adapter、Skill、Tool |
| `adr/0008-free-canvas-agent-graph-authority.md` | Accepted | 为什么是 live graph 与 Graph Command |
| `adr/0009-agent-canvas-sandbox.md` | Accepted；第 1～4 刀已落地 | 人是画布主控、会话归属、可选 Goal |
| `adr/0010-mainline-no-compatibility.md` | Accepted | 主仓库不保兼容、不写旧数据迁移 |
| `adr/0011-go-vertical-slice-rewrite.md` | Accepted；cutover 已完成 | 业务后端按垂直切片迁 Go |
| `adr/0012-gorm-command-writes.md` | Accepted | 命令路径用 GORM 模型写库，不用手写 INSERT |
| `adr/0013-agent-live-journal-bff.md` | Accepted | Go 是浏览器 BFF；live UI journal 在 agent-service |

## 协作元数据

[`agents/`](agents/) 只管 issue tracker、triage、domain 阅读约定。

## 写作规则

1. 当前事实写在 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。未验证方向写在 ROADMAP，不把已接线能力再抄一遍。
2. 规格不复述已交付合同。只写相对 PRD 多出来的能力、验收和不做。
3. ADR 不更新成现状仪表盘。
4. 用户操作变化必须同时改 USER_GUIDE 和 HelpPage。
5. 实现敏感声明指向当前代码所有者或测试。
6. 主仓库不新增兼容层、双序列化或旧数据迁移；残留路径删除。见 [`adr/0010-mainline-no-compatibility.md`](adr/0010-mainline-no-compatibility.md)。

`just docs-check` 校验索引、前端路由、code owner 路径和仓库内链接。规格目录若重新出现才检查状态标注。它不检查「同一句话是否写了六遍」。
