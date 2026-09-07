# 任务：按用户裁定纠正未知调用额度与图片采用政策

状态：完成
类型：实现
认领者：root/policy-alignment
认领于：2026-09-08T00:38:22+08:00
完成后可拆：无

任务状态、认领和提交遵循 [Issue 协议](../README.md)。

## 问题来源

[设计校准](saas-design-alignment.md) 已纠正文档和明显占位代码，用户已于 2026-09-08 同意以下两项建议。本任务交付相应实现，旧策略不再指导后续开发。

## 做成什么样

用户明确供应商结果无法确认时的用户额度处理，以及有限图片检查失败时用户是否仍可确认采用。按裁定修改真实业务边界和受影响界面，保留原始执行事实与历史评测结果。

## 用户裁定（2026-09-08 已同意两项建议）

1. 未知调用默认 72h 到期释放用户预留，保留供应商结果未知与平台成本事实，不自动按预留全额结算。
2. 有限图片检查失败或无法判断时展示问题，允许用户明确确认采用；保存实际检查状态并允许默认下载。越权、损坏、缺失素材与无效规格仍拒绝。

## 前置与并行

用户已同意两项建议。主代理持有 delivery 与文档，frontend-policy 持有 Web 采用相关代码；额度子代理仅持有 go/internal/quota 与相关 scanner 回归。隔离测试基础库 pf_policy_alignment，按包测试，不启停共享服务。每个答案到达后可独立推进相应结果，无需等另一个。认领前按实际调用链登记 quota/执行回收或 delivery/图片检查及 Web reader 的排他范围，不修改模型评分、冻结样本与真实 provider 配置。

## 合同与验收

- 额度变化不把 unknown 伪造成供应商成功或零成本。扫描、人工处理和迟到结果竞争只能形成一次有效余额变化；覆盖重试、幂等和原始关联记录。
- 采用权限、用户确认和质量状态分别表达；确认采用不能修改历史检查结果或宣称图片合格。覆盖受影响 API、持久快照、下载与 Web 状态。
- 决策回写 ROADMAP、商家平台、图片质量和体验组的受影响条款；历史证据不重算。
- 运行受影响 Go/PG、Web 回归和静态门、docs-check。真实付费评测不由本任务自动授权。

## 阻塞与交接

- 原因：无；用户已批准两项建议。
- 解除条件：已满足。
- 跟进者：主代理。
- 交接：设计纠偏切片已交付；本任务已实施，测试只用 pf_policy_alignment 派生隔离库，无共享服务占用。

## 证据

- 2026-09-08：用户已同意两项建议，实施与验证沿已冻结合同。
- 交付定位：随本任务提交。
- 审核者 / 结论：主代理自审 delivery、子代理 quota/Web 全部交付 diff 与集成；确认前无采用写入，确认后质量状态不升级，额度释放竞争只有一次终态变化。
- Issue 结果 / 剩余缺口：政策实现与回归已完成；不代表真实图片质量、商业发布或身份收敛已验收。身份变更独立提交。

## 本轮所有权

- root/policy-alignment：delivery 采用输入到快照、Web 确认与下载；活文档、任务记录、最终集成与提交。
- quota-policy：仅 go/internal/quota/ 及必要到期扫描测试；不得改 Web、delivery、其他 docs，不得提交。与主代理不重叠。
- 保留已有 HomePage、home e2e 与图片质量 smoke 未提交变更；不运行付费模型。

- frontend-policy：仅 web/src/lib/api.ts（错误 code 与采用输入）、types.ts（必要类型）、i18n.ts、采用 helper/results 组件及测试、采用浏览器测试；不得触碰 HomePage/home.spec.ts。不改 Go/docs。
- 已冻结接口：POST delivery-adoptions 新增 acknowledge_quality_warnings:boolean；未确认且服务端检查 fail/unchecked 返回409 code=adoption_quality_confirmation_required 与可读 detail，无写入；确认后201，slot quality_status/detail 保留服务端结论，qualified=false。已确认的非合格采用可下载，qualified_only=true 仍过滤；越权、缺失损坏字节、规格无效仍拒绝。

- 主代理 delivery：完整包通过（40 个 pass 事件含子用例/包，0 skip/0 fail）；确认前无写入、确认后 fail/unchecked 保留，实际派生与 ZIP 下载、仅合格过滤、假 pass、OCR 缺字/无期望、越权/损坏/非法规格验证。

## 最终验证

- Go/PG：隔离 pf_policy_alignment 派生库完整 delivery 40 个 pass 事件（含子用例和包），quota 22 个 pass 事件，均 0 skip/0 fail；额度到期与人工处理/迟到结果竞争另有 race 重复 10 次通过。身份集成期间再次运行 delivery/quota 也通过。
- Web：完整 104 个测试文件、734 项测试通过，lint、just web-build（类型、生产构建、体积预算）通过。最终测试断言调整后 focused 3 文件/16 测试与相关 lint 通过。
- 浏览器：Playwright delivery-adoption-quality-policy.spec.ts 共17项通过。确认、取消、普通409、商品变化清除确认、冻结请求、确认后fail/detail保留、默认下载；四语言、light/dark、390/1440像素的实际viewport和确认框边界均验证，主代理抽查中手机与英桌面截图。
- 日志：/tmp/pf-policy-browser.log、/tmp/pf-policy-web-tests.log、/tmp/pf-policy-web-build.log、/tmp/productflow-quota-policy-test.jsonl。浏览器route mock未调用真实模型，未修改共享开发数据库或重启开发栈。
- 主代理浏览器补查时preview目录曾被并行首页构建短暂清空（页面404）；改用现有开发服务器后17项通过，不修改产品行为绕过失败。
