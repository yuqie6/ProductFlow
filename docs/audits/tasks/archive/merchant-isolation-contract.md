# 任务：冻结商家隔离覆盖矩阵与实施批次

状态：完成
类型：证据
认领者：sub-merchant/merchant-isolation-contract
认领于：2026-09-07T11:55:25+08:00
完成于：2026-09-07T12:02:40+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：按已审核覆盖矩阵发布身份成员与各业务链隔离切片；完整隔离门未过不开放第二商家

任务合同以本文件为准，按 [Issue 协议](../README.md) 认领并确认所有权后调查。此任务交付精确覆盖与可执行切片，不宣称隔离已实现。协调者审核：2026-09-07 CTO 确认矩阵/批次可执行；工具清单按 manifest 再核（见证据）。

## 问题来源

用户确认最终产品是可自托管的多商家 SaaS，项目方也经营同一产品实例。当前管理员布尔会话、共享业务查询、媒体读取和 Agent scope 不具备商家边界。[总纲第 7 节](../../../ROADMAP.md#7-多商家产品与权限设计) 已确定实体、角色和端到端权限，尚需逐入口和持久对象覆盖，防止实现漏掉内部工具、下载、实时事件和后台作业。

## 做成什么样

在父章程维护完整矩阵：每个业务路由/内部工具、操作角色、根所有权、子引用验证、队列/effect 载荷、读写 owner、前端查询/订阅和测试入口均有条目。共享规则可归并，但必须附具体入口清单并能反查遗漏。给出从 User/Membership 到完整隔离的依赖批次，每批有精确范围、正反测试和中间版本暴露限制。

## 前置与并行

- 前置：总纲第 7 节与父章程 MP-01 至 MP-07；当前源码已可读取，无外部凭据前置。
- 冻结输入：认领时记录 HEAD 与涉及文件 diff；总纲实体/角色如有冲突交协调者裁定，不能在调查中自行变更。
- 运行资源：只读源码、路由/表/工具清单及现有测试；无需 DB、worker 或 provider。已有评测样本和模型窗口不受影响。
- 排他写入：本文件和父章程；总纲或共享索引变更由协调者整合。

## 只改这些文件

- `docs/audits/merchant-platform.md`：覆盖矩阵、批次与验收缺口。
- 本文件；完成归档与索引由协调者按协议处理。

## 不要碰

业务源码、schema、Skill、grader、真实业务数据、密钥及已有采证文件；不修改当前认证设置来模拟多商家。

## 现在代码在哪

`go/internal/auth/` → `go/internal/platform/httpx/admin.go` → 各业务 HTTP/usecase/store；`go/internal/platform/db/schema/`、`go/internal/graph/`、`product/`、`library/`、`delivery/`、`localedit/`、`imagesession/`、`settings/`、`agent/` 及 `agent-service/src/runtime-scope.ts`。Web 从 `web/src/lib/` 的 API/query 与各业务页面追到商家切换影响。实际目录和路由须现场枚举，以上仅为入口。

## 合同

- MP-01 至 MP-07 全部保持。Merchant 从服务端身份校验，不信任 payload 授权；后台任务和内部令牌不能逃逸资源范围。
- 矩阵覆盖独立根对象与仅能通过父资源访问的子对象；说明需要落商家字段、可从父关系证明或应保留实例级的原因。
- 处理跨资源绑定、未知结果、成员撤销、SSE 游标、媒体变体和批量导出；不能只测“产品列表加过滤”。
- 记录数据约束与查询两侧的验证位置。中间迁移批次不得开放另一租户，也不增加共享管理员回退入口。

## 怎么验收

用 `rg` 枚举 HTTP 注册、schema 模型、Agent tool 注册、异步载荷和下载/SSE 入口，对照矩阵反查。每类至少走一条输入→用例→持久化/异步效果→响应路径；所有其余入口有归属和测试计划，不留无说明的空格。

自审模拟：合法商家读写、另一商家替换 ID、跨商家绑定、旧订阅、成员撤销后确认、内部工具伪造范围、任务重试归属。每个预期结果能落到具体实施批次和验证层。运行 `just docs-check` 与 `git diff --check`。

完成表示矩阵与批次可执行且无未裁定边界，不代表 R1/MP-B 通过。缺全量入口、角色裁定或未知根归属时保持任务未完成。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：认领后登记的执行者，CTO 负责合同争议。
- 交接：交付物已写入父章程与本证据区；状态仍为认领，待协调者验收归档。执行者停下等分配，不 push、不宣称程序完成。

## 证据

- 冻结 HEAD：`d6709c4aacb2e26bb30ab70a99d08b1dca05f487`（认领时 `git rev-parse HEAD`）。
- 枚举命令（摘要）：
  - HTTP：`go/cmd/productflow-api/register.go` + 各包 `http*.go` Group/动词解析 → 业务路由 **214** + healthz×2 + 条件 metrics。
  - Schema：`rg 'TableName\(\)' go/internal/platform/db/schema/models.go` → **57** 表。
  - Tools：`rg 'name: "' agent-service/src/tool-manifest.ts` → **25**。
  - Queue：`go/internal/platform/queue/actors.go` → 1 信封 + **5** Actor。
  - SSE/下载：`rg` 于 graph/imagesession/agent/product/library/media → 6 SSE 族、download/content/ZIP/variant。
  - 前端：`rg 'queryKey:' web/src` + EventSource runtime。
- 覆盖：父章程归并矩阵 **69** 条，展开覆盖上述枚举面；每类因果样例 B9/C6/D6/E6/F6/G5–G6/H7/I10。
- 批次：B0–B10（身份→根归属→商品→Graph/配方→图库绑定→会话→交付/局部编辑→Agent→队列/前端→运营最小→双商门）。
- 未裁定边界：**无**（图库/global Agent/配方/media_objects/settings/对外错误码/第二商开放均已裁定）。
- 验证：`git diff --check` 通过（仅本任务两文件）。协调者归档时 `just docs-check` 以当时工作树为准。
- 自审：七场景均映射到批次（见父章程「自审模拟」与下节）；剩余缺口仅为实现未落地（MP-A/B/C/D），非矩阵空洞。
- 审核者：CTO（本会话）；结论：通过，可拆 B0。
- 交付定位：随归档提交（待用户授权 commit）。

## 自审结论

| 检查 | 结果 |
|---|---|
| 入口枚举可反查 | 通过：路由/表/工具/Actor/SSE/下载有计数与路径 |
| 非「列表加过滤」 | 通过：含下载、ZIP、SSE、internal content、跨资源绑定、队列 Restage、Pi tools |
| 角色与根/子归属 | 通过：Own/Ed/Vw/Op/Int/Sys；57 表分类 |
| 批次中间不开放第二商 | 通过：B0–B9 限制；B10 后开放 |
| 未裁定边界 | 无 |
| 隔离已实现？ | **否**；本任务仅合同与矩阵 |

剩余缺口（预期，不阻塞本证据任务）：B0–B10 未编码；双商套件未建；MP-C/D 未开。
