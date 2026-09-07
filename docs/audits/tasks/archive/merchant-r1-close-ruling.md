# 任务：总纲 R1 身份与隔离关闭裁定

状态：完成
类型：证据
认领者：sub-merchant/merchant-r1-close-ruling
认领于：2026-09-07T16:14:58+08:00
完成于：2026-09-07T16:19:32+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：MP-C 额度；运营邀请第二外部商（另决）；≠开放互不信任第二商上线

任务合同以本文件为准；认领、阻塞、审核与关闭遵循 [Issue 协议](../README.md)。按 [所有权前置规则](../README.md#认领与并行) 已确认认领。

## 问题来源

方向 1 要求多商家正式进入主线。B0–B10 / MP-B 自动化门已过，但 ROADMAP **R1** 仍标「未实现」。需按 R1 条文对照既有证据：两商家、多角色、成员撤销；HTTP/资源/队列/事件/Agent/后台交叉拒绝。不得因自动化门通过就默示开放第二外部商产品注册。

## 做成什么样

1. 逐条对照 ROADMAP R1 与 [merchant-platform](../../merchant-platform.md) MP-A/MP-B、B0–B10 归档证据；列出「已齐 / 缺口」。
2. 若缺口仅为可补的自动化用例（不开放外部第二商 UI）：在 `go/internal/auth`（或既有 isolation gate）最窄补测后复跑。
3. 若证据已齐：更新 `docs/ROADMAP.md` R1 为**通过**，父章程写明残余（`CreateMerchant` 仍拒第二开发商直至运营邀请；≠MP-C/MP-D；≠开放互不信任商家上线）。
4. 若关键缺口无法在本窗补齐：诚实 **未通过**，列阻塞项；**不得**假标通过。

## 前置与并行

- 前置：B10 [merchant-isolation-gate](merchant-isolation-gate.md) 已归档。
- 隔离：补测用测试 DB；禁止共享 `productflow` down；禁止改运营策略擅自开放第二外部商。

## 只改这些文件

- 本文件
- `docs/ROADMAP.md`（R1 行，仅当裁定成立）
- `docs/audits/merchant-platform.md`（R1/MP-B 结论）
- 若补测：`go/internal/auth/*_test.go` 或 isolation gate 测试（最窄）

## 不要碰

- 放开 `CreateMerchant` 给第二外部商；MP-C 账本实现；Skill/grader。

## 合同

- ROADMAP R1；夹具双商 + 自动化交叉拒绝可满足「两商家」证据口径，须在裁定中写明 ≠ 产品上线第二互不信任商。
- 完成可 FAIL。

## 怎么验收

- 对照表 + 引用归档 run/测试名；补测命令与结果。
- `just docs-check`；相关 `go test` 包。
- ROADMAP / 父章程与裁定一致。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：已归档；交付随本任务提交。未放开 CreateMerchant。

## 证据

### 对照表（ROADMAP R1 ↔ 归档）

| R1 条文 | 证据锚点 | 结论 |
|---|---|---|
| 两商家 | 夹具 `auth.SeedDualMerchants`；`TestMerchantIsolationGateB10` seed A/B；各域 `TestMerchant*` 双商数据；总纲 §11 允许「内部环境用两个测试商家」验证 R1 | **已齐**（夹具口径；≠产品第二商上线） |
| 多角色 | B0：[merchant-identity-skeleton](merchant-identity-skeleton.md)；`TestInviteAcceptAndRole`（Owner→邀请 Editor）；Viewer 邀请于 `TestNonMemberForbidden`；商家角色拒 Op 面 `TestMerchantRoleForbiddenOnSettingsAndQueue`；MP-A | **已齐** |
| 成员撤销 | `TestNonMemberForbidden`（DELETE membership → 403）；门禁 `scenario_成员撤销后读写`（撤销后 get/list 403，`RequireWorkingMerchant`）；Agent 分区「撤销后 confirm」（B7/B10 inventory） | **已齐** |
| HTTP / 资源交叉拒绝 | B1–B6 归档 + `TestMerchantProductChainIsolation` / Graph / Recipe / Library / ImageSession / Delivery / LocalEdit；跨商统一 `CrossMerchantDetail` 404；门禁 `scenario_跨商替换ID` / 伪造商家头 403 | **已齐** |
| 队列 / 后台任务 | B8：[merchant-queue-frontend](merchant-queue-frontend.md)；`TestStageSnapshotsMerchantAndRejectsRewrite`；`TestRestageIfIdlePreservesMerchantMixedQueue`；分区 J | **已齐** |
| 事件 | Graph/ImageSession/Agent SSE 跨商 404 与旧订阅（B3/B5/B7/B8；B10 inventory C/F/H） | **已齐** |
| Agent | B7：[merchant-agent-tools](merchant-agent-tools.md)；`TestMerchantAgentToolsIsolation`（伪造 scope / 跨商 content / 工具 harness） | **已齐** |
| 第二外部商产品注册 | `TestSecondMerchantRejected` + 门禁 CreateMerchant **409**；产品策略保持直至运营邀请 | **刻意保持拒绝**（残余非宣称，不阻塞 R1） |

本窗**无需补测**：既有 auth invite/revoke 与 B10 套件已覆盖可自动化缺口；未改 `CreateMerchant`。

### 复跑命令（2026-09-07T16:17:20+08:00）

| 命令 | 结果 |
|---|---|
| `bash scripts/with_dev_env.sh bash -lc 'cd go && go test ./internal/auth/ ./internal/product/ ./internal/graph/ ./internal/recipe/ ./internal/library/ ./internal/imagesession/ ./internal/delivery/ ./internal/localedit/ ./internal/agent/ ./internal/platform/queue/ -run "Merchant\|IsolationGate\|StageSnapshotsMerchant\|RestageIfIdlePreservesMerchant\|SecondMerchant\|InviteAccept\|NonMember\|LastOwner\|SessionRevoke" -count=1 -p 1 -timeout 20m'` | **PASS**（10 包 ok） |
| `pnpm --dir web exec vitest run src/lib/merchantBoundary.test.ts src/lib/opsAccess.test.ts` | **PASS** 6 tests |
| `just docs-check` | **PASS** |

B10 归档原跑见 [merchant-isolation-gate](merchant-isolation-gate.md)（工作树基线 `fb0ad286`）。

### 协调者复验

| 检查 | 结果 |
|---|---|
| `go test ./internal/auth -run IsolationGate\|SecondMerchant\|InviteAccept\|NonMember` | PASS |
| `CreateMerchant` 仍拒绝第二商（门禁/测试合同） | 保持 409（未改产品策略） |

### 总纲裁定

**总纲 R1：通过。**

依据：上表条文均有可归档自动化证据；「两商家」按合同采用**测试夹具双商**，与产品开放第二互不信任商分离。

### 残余非宣称（不阻塞本裁定）

1. `CreateMerchant` **仍 409**，直至运营邀请；本裁定 **≠** 开放互不信任第二商产品上线。
2. **≠ MP-C**（商业额度账本）；**≠ MP-D**（完整运营支持会话产品化；仅 A8 草案）。
3. 不宣称 R2–R5；不宣称满载混合经营 SLA。
4. 额度公平调度、真实邀请试用属后续运营/组任务。

### 审核

- 审核者：CTO（本会话）；结论：对照表与复跑可采信；**总纲 R1 通过**（夹具双商口径）。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-r1-close-ruling.md` 查询）
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成；**R1 通过**；残余：`CreateMerchant` 仍 409；≠MP-C/MP-D；≠第二外部商产品上线。
