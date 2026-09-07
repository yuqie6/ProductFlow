# 任务：按账号自有商家与平台管理职责纠正现行设计和明显偏离代码

状态：完成
类型：实现
认领者：root/design-alignment
认领于：2026-09-08T00:03:35+08:00
完成后可拆：无

任务状态、认领和提交遵循 [Issue 协议](../README.md)。

## 问题来源

用户在竞品分析后确认自托管 SaaS 与自营站点，现明确普通账号对应自己的商家，管理员可管理所有商家的商品。旧设计擅自扩展成多组织成员与切换，并把有限质量检查和未知调用处理固化为用户政策。用户要求整理各组文档并清理明显偏离代码；重要产品政策通过 grill-me 问清。

## 做成什么样

现行文档不再指挥多商家切换、团队角色和临时支持会话建设；商品始终归所属商家，平台管理员权限与商家身份分离。保留隔离、可靠执行和真实质量证据。取消本轮未提交工作区增量，清理无用支持会话占位。未知额度与采用检查标明现有行为、待裁定问题和实现交接，不擅自制定政策；实际政策变更由独立待决任务承接。

## 前置与并行

基线 cd78b456；工作区切换已暂停，三个执行代理停止写入。共享开发栈不重启，测试使用隔离数据库。图片质量 smoke 的既有未提交变更保留，不混入提交。

## 修改范围与所有权

- 主代理：ROADMAP/PRD/CONTEXT/README 与翻译、商家平台、任务池和现行任务依赖；本轮未提交 Web 工作区增量清理，以及 MerchantOpsPanel、i18n 与 smtp-settings 浏览器测试的支持占位 reader 删除；最终整合与审核提交。
- root/alignment-groups：仅 docs/audits/{README,agent-eval-system,agent-self-harness,image-quality,canvas-test-system,performance-governance}.md；不修改历史证据与其他文件。
- root/workspace_backend_audit：删除 go/internal/auth/support.go 的支持会话草案与占位路由，调整 auth/http.go、ops_test.go、merchant_isolation_gate_test.go、go/cmd/productflow-api/routes_contract_test.go、contracts/http-routes.json、web/src/lib/api.ts 中该无用 API；不得改其他权限或业务。新商品管理后台功能与彻底重构身份表不在本项实施范围；相应目标写回现有所有者。

## 合同

一个普通账号自有一个商家；无团队或工作区选择产品流程。管理员跨商家管理已有商品，商品归属不变，不以伪造商家成员或支持会话实现。当前代码事实与目标明确区分。历史测试结果不改写；评测阈值不偷换；自进化保留既定授权及独立验收，不变成商家试用的前置。用户政策未答复前不改相关业务行为。

## 怎么验收

复查当前设计与各组一致性、原错误决策残留、链接和 just docs-check。代码删除查全部读写者并跑相关 Go/Web 回归和静态门；不运行真实付费模型，不宣称整站发行或真实图片质量通过。

## 阻塞与交接

- 原因：本轮文档与明确删除项已完成。两个政策问题未答复，其实现转交 [政策纠偏](saas-policy-alignment.md)，不以等待答复占用账号与管理文件。
- 解除条件：本任务无需；后续政策任务待用户答复。
- 跟进者：root/design-alignment。
- 交接：主代理持有文档与整合；没有新增运行资源。

## 证据

- 2026-09-08：清除未提交的 WorkspacePicker/Gate、selection/context/edits/messages 和 API/generation 增量；Web 恢复原已验证商家选择行为，本轮不宣称身份遗留已收敛。
- 删除支持草案四路由与唯一 Web reader，主代理重新创建隔离基础库后验证 auth 包与 cmd API 注册/新增退役路由合同，逐项核对 JSON 测试结果无 SKIP（auth 47 个 pass 事件含子用例/包，0 fail）；测试库 pf_alignment_support_gotest_auth，无共享开发库修改。早前代理成功退出的摘要不足以证明 PG 实跑，不作最终证据。主代理创建的基础库与测试库已删除。
- Web：103 files / 730 tests passed；pnpm lint、just web-build（含预算）通过。SMTP 设置浏览器 8 cases，390/1440、四语、明暗覆盖，无已删除支持 API 请求；截图 web/test-results/alignment-support。
- just docs-check、git diff --check 通过（归档整合后复核）。
- 工程规则正反走查：普通账号直接进自有商品；管理员跨商管理须独立权限而非成员伪装；“做 SaaS”不能触发团队/工作区切换；未知收费或质量拦截先提问。未改变历史模型评分、冻结 Gate 或已有角色隔离测试的历史意义。
- 基线 commit：cd78b456。
- 交付定位：随本任务提交。
- 审核者 / 结论：主代理审核两个代理完整 diff 并整合 Web reader；主代理自审其余文档和任务依赖。
- Issue 结果 / 剩余缺口：现行文档与支持占位删除已验收；管理员完整商品管理、身份模型遗留和待裁定政策未实现，分别保留目标与待决任务。
