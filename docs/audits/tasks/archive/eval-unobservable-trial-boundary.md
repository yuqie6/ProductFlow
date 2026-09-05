# 任务：阻断不可测工具反馈形成可消费的能力轨迹

状态：完成
类型：实现
认领者：主代理-eval-quality-0905-2125
认领于：2026-09-05T21:25:56+08:00
完成后可拆：协调者确认 Go 节点合同稳定后补齐结构操作观察，再恢复开发基线

遵循 [Issue 协议](../README.md)。协调者已检查现有占用并确认认领；前一证据任务进程已结束，未提交记录由协调者保管。

## 问题来源

用户要求测评真实可用后才能交给自进化。固定提交 `8cf6e08a` 的第三批 `20260905T131829Z-35b08c45` 中，解散并重排题的单独 dissolve apply 缺失 Go 观察，stub 返回 `eval_unobservable`，模型随后继续重试、提案。报告已识别 unknown 工具为不可测；需核实执行和开发材料导出是否仍将受环境缺口影响的轨迹作为能力数据。

## 做成什么样

缺失真实观察有明确不可测身份，不能伪装真实业务拒绝或有效能力失败；不可测批次不能导出为自进化开发输入。按真实调用链补齐缺失边界，不重写已有正确报告逻辑。

## 前置与并行

- 前置：读取义务校正 `8cf6e08a` 已交付；第三批已停止，固定 checkout 与所有原始产物只读保留。
- 本单独占相关 eval 执行、导出及回归测试文件；不占用 Skill 候选、共享数据库、provider 或浏览器。
- Go 节点合同由 `node-detail-redesign.md` 持有，本单不生成新的 Go 结构观察，不复制生产图执行器。

## 修改与调查范围

- 实际修改 `agent-service/evals/live-runner.ts`、`collections.ts`、`collections.test.ts`、`live-observability.test.ts`、`stub-world.test.ts`；复用既有报告 eligibility，不修改 stub 实现、schema 或 grader。
- 本任务及协调看板；活文档仅更新受影响的现有合同。
- 不改题意、expected、生产 Skill/runtime、Go 实现、评分阈值；不能以假成功或假失败填充缺失观察。

## 怎么验收

- 确定性回归覆盖缺失结构观察、真实已观察成功/失败、unknown 身份及开发导出拒绝；现有正常运行和导出合同保持。
- 相关 eval 测试、TypeScript 检查、`just agent-service-check-contracts`、`just docs-check`。
- 不要求新的付费全量采样；该单完成不等于结构观察已齐全，也不等于完整开发基线通过。

## 阻塞与交接

- 无运行进程；原第三批保存在 `storage-dev/eval-development-20260905-r3`。后续结构观察采集须等待 Go 节点合同稳定并确认资源所有权。

## 证据

- 2026-09-05：确定根因是开发导出只校验身份/完整性，未检查原始记录的可测性。报告层原本已将 unknown 标为不可测，无需修改。live 记录原本仍沿用模型终态，现单独标记 `unobservable` 并记录诊断原因，原始 terminal/output/stub_calls 保留。
- 导出在读任何转录前复用 `buildRunReport(...).measurementEligible`；显式不可测及 unknown 读/写整批拒绝，不信任旧 summary 中的成功标记，不剔除失败 trial。真实已观察成功/失败仍能导出。
- 回归包含真实 stub 的单独 dissolve 缺失观察复现、图状态未伪造、已录结构成功与 409 拒绝；实际 run 存储链通过本地替身模型验证不可测身份、原始终态和转录保留。没有发起新 provider 请求，未声称生产模型通过。
- `pnpm exec vitest run evals/collections.test.ts evals/live-observability.test.ts evals/stub-world.test.ts evals/report.test.ts`：44 passed。
- `pnpm exec vitest run evals`：115 passed / 5 skipped；跳过项为 opt-in live，测试中的 fixture fail 输出是预期的诊断路径，测试断言全部通过。
- `pnpm exec tsc --noEmit -p tsconfig.json`、`just agent-service-check-contracts` 通过；`just docs-check` 与完整 diff 自审随归档执行。
- 原始依据：`8cf6e08a` / `20260905T131829Z-35b08c45`，产物根 `storage-dev/eval-development-20260905-r3`，59/225 条中 2 条含 unknown，原始证据不改写。停止批次的详细身份和成本缺口由 [开发基线](../eval-development-baseline.md) 持有。
- 交付定位：随本任务提交。审核者：主代理-eval-quality-0905-2125，自审；边界修复与确定性证据满足本单合同。
- 剩余缺口：结构操作 Go 观察仍不完整，未复采付费全量 L1、未交付有效开发包，不能据此启动依赖该包的自进化实验。Go 节点合同仍由另一任务持有，等待稳定后独立补齐观察；本单不复制生产执行语义。
