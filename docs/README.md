# ProductFlow 文档

按成品仓库组织。一份事实一个家。修葺切片、接线仪表盘、「只从某节进入」不是文档类型。

代码、测试、迁移和真实运行行为是最终证据。文档与它们冲突时改文档。

## 当前产品

| 文档 | 管什么 | 不管什么 |
|---|---|---|
| [`../CONTEXT.md`](../CONTEXT.md) | 领域词汇、权威边界、稳定不变量 | 页面教程、文件清单、未实现方向 |
| [`PRD.md`](PRD.md) | 用户现在能做什么、非目标、成功标准 | 模块路径、部署命令 |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | 运行单元、代码所有权、数据流、测试锚点 | 未实现设想、操作教程 |
| [`USER_GUIDE.md`](USER_GUIDE.md) | 页面怎么操作、故障怎么处理 | 内部事务、模型表 |
| [`../AGENTS.md`](../AGENTS.md)、[`../go/AGENTS.md`](../go/AGENTS.md)、[`../backend/AGENTS.md`](../backend/AGENTS.md)、[`../web/AGENTS.md`](../web/AGENTS.md) | 怎么改这个仓库 | 产品愿景复述 |

`web/src/pages/HelpPage.tsx` 是 `USER_GUIDE.md` 的产品内投影，同一提交更新。

英文 `PRD.en.md`、`ARCHITECTURE.en.md`、`USER_GUIDE.en.md`、`ROADMAP.en.md` 是翻译，不是第二份规范。改当前能力或路线时同一提交更新。

## 为什么这样建

[`adr/`](adr/) 记录已接受决策。正文冻结。被取代的部分在状态行标明后继 ADR，不把旧 ADR 改写成今天的实现。

## 还没做成产品

[`ROADMAP.md`](ROADMAP.md) 只写尚未存在、或尚未被真实验证的方向。每条最多指向一份规格。已接线的能力写在 PRD / ARCHITECTURE / USER_GUIDE，不在路线图里再列一遍。

## 进行中的设计

[`specs/`](specs/) 是临时合同。落地后内容进入 PRD / ARCHITECTURE / USER_GUIDE，规格删除或缩成一句指针。规格必须标注 `文档状态：Draft` 或 `文档状态：Approved`。

| 文档 | 状态 | 相对当前产品多出来的东西 |
|---|---|---|
| `specs/workbench.md` | Draft | 工作台作为生产面必须达到的完成度；浏览器证明之前不宣称完成 |
| `specs/studio-increments.md` | Draft | 镜头列表默认主区、生成套图文案、推荐套图、局部修、保真核对 |
| `specs/shot-scene-assembly.md` | Approved | Shot/scene 的 group 与默认边 |
| `specs/agent-canvas-sandbox.md` | Draft | Goal 托管环 |
| `specs/global-agent-human-workflow-design.md` | Approved | 全局 Agent 与人工工作流设计 |
| `specs/pi-agent-runtime-integration.md` | Approved | Pi 未过的 durability / Fresh Observation gate |
| `specs/go-backend-rewrite-prd.md` | Approved | 业务后端迁 Go 的产品合同；默认运行时已切 Go，残留闸门见 ROADMAP |
| `specs/go-backend-rewrite-design.md` | Approved | 垂直切片、封印基线、Strangler 顺序 |
| `specs/productflow-studio-requirements.md` | Draft | Studio 需求清单 |

## 某次部署的证据

[`rollout/`](rollout/) 和 [`operations/`](operations/) 管未完成的操作证据。V1 cutover 与旧 Gallery 回填不再是主线义务，见 [`adr/0010-mainline-no-compatibility.md`](adr/0010-mainline-no-compatibility.md)。

| 文档 | 作用 |
|---|---|
| `rollout/pi-agent-durability.md` | Pi Agent 耐久证据与生产开关 |

## ADR

| 文档 | 状态 | 何时读 |
|---|---|---|
| `adr/0001-agent-draft-authority.md` | Accepted | PostgreSQL 权威、unknown、全局库 Draft |
| `adr/0002-canonical-product-images.md` | Accepted | 媒体身份、商品图片、封面、lineage |
| `adr/0003-schema-v2-workflow.md` | Historical copy; archived original | schema-v2 当时决策；当前形状读 ARCHITECTURE |
| `adr/0004-legacy-v1-cutover.md` | Superseded by 0010 | 原 V1 归档/冻结/闸门；主线不再执行 |
| `adr/0005-agent-workbench-ui.md` | Accepted | 工作台交互与工具步骤投影 |
| `adr/0006-media-library-authority.md` | Accepted, amended; §7 superseded by 0010 | 全局图库、子图库、来源生命周期 |
| `adr/0007-pi-agent-runtime-boundary.md` | Accepted, rollout pending | Pi adapter、Skill、Tool |
| `adr/0008-free-canvas-agent-graph-authority.md` | Accepted | 为什么是 live graph 与 Graph Command；当前形状读 ARCHITECTURE |
| `adr/0009-agent-canvas-sandbox.md` | Accepted；第 1～4 刀已落地 | 人是画布主控、会话归属、可选 Goal |
| `adr/0010-mainline-no-compatibility.md` | Accepted | 主仓库快速破坏性更新，不保兼容、不写旧数据迁移 |
| `adr/0011-go-vertical-slice-rewrite.md` | Accepted | 业务后端按垂直切片迁 Go；Python 行为是封印基线 |

## 协作元数据

[`agents/`](agents/) 只管 issue tracker、triage、domain 阅读约定。

[`archive/`](archive/) 只管仍有独立追溯价值、已退出默认阅读路径的记录。被当前文档吸收的草稿直接删除，历史用 Git。schema-v2 当时决策见 [`archive/adr/0003-schema-v2-workflow.md`](archive/adr/0003-schema-v2-workflow.md)。

## 写作规则

1. 当前事实写在 CONTEXT / PRD / ARCHITECTURE / USER_GUIDE。未验证的浏览器证据写在 ROADMAP，不把「已接线」再抄进路线图。
2. 规格不复述已交付合同。只写相对 PRD 多出来的能力、验收和不做。
3. ADR 不更新成现状仪表盘。
4. 用户操作变化必须同时改 USER_GUIDE 和 HelpPage。
5. 实现敏感声明指向当前代码所有者或测试。
6. 主仓库不新增兼容层、双序列化或旧数据迁移；残留路径删除，不包裹。见 [`adr/0010-mainline-no-compatibility.md`](adr/0010-mainline-no-compatibility.md)。

`just docs-check` 校验索引、前端路由、code owner 路径、规格状态标注和仓库内链接。它不检查「同一句话是否写了六遍」。
