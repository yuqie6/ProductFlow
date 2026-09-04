# 任务：L2 记录内容身份与实际运行配置

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T06:58:51+08:00
业务组：评测
父账本：agent-eval-system.md
完成后可拆：与 eval-l2-terminal-observation 均交付后恢复 eval-state-live 前置核验

任务合同以本文件为准；认领、审核和关闭遵循 [Issue 协议](../README.md)。

## 问题来源

[L2 采证核验](../eval-state-live.md) 确认旧 `writeEvalRunJSON` 的 task_set_hash 仅覆盖 ID 加 NUL，题目与 world 改动不改变身份；缺少实际加载 Skill 与有效 provider/reasoning 身份。P2 已记录真实 `/healthz` harness_hash，本任务保留该来源，不用磁盘推算代替已加载身份。

## 做成什么样

新 L2 run 能追溯本次实际选择的任务内容及所用 world、已加载 Skill、有效模型/provider/reasoning、代码与脏工作树身份。启动前校验可确定的内容/进程身份；实际模型在 Pi session 中延迟解析，以请求前 checkpoint 为事实，缺失或不一致不能签收为完整批次。不得回填历史产物。哈希 canonical 化复用现有逻辑或提供与现有格式明确区分的版本，不新增宽泛兼容读取器。

## 前置与并行

- 前置：无；认领前核对 eval-l2-terminal-observation 占用，同一 Go runner 必须串行。
- 冻结输入：不更改 Skill、任务、world、grader；新增诊断身份不得改变 Pi 执行行为。真实采证另由 eval-state-live 执行。
- 运行资源：隔离 httptest/假 provider/临时产物；不改共享 DB/provider、图片组浏览器、dev 服务或真实旧 run。

## 只改这些文件

- `go/internal/agent/eval_state_gopg_test.go`、`harness_attribution_test.go` 与必要新增评测身份 `_test.go`。
- 维护者于 2026-09-05 核实：health 已暴露已加载 catalog hash，模型按 session 延迟解析。批准 `agent-service/src/pi-runtime.ts`、`turn-runtime.ts` 及其对应测试，仅为现有 before_model_request checkpoint 增加实际模型配置/Skill 身份；复用已有 checkpoint 持久化，不增 API、表或修改模型请求行为。
- 本文件；维护者同步父账本、看板与归档。

## 不要碰

- 被测 Agent prompt、Skill、生产工具、执行权限和状态转换；任务与 grader；旧批次 metadata；L2 终态等待由独立 issue 负责。

## 现在代码在哪

`writeEvalRunJSON`、`LoadEvalTasks`、`LoadEvalWorlds` 与 `agent-service` health、Skill loader、L1 run metadata/集合内容哈希；取得认领后追踪实际配置来源，复用已有身份定义。

## 合同与验收

- 父章程证据格式与 L2-01/L2-06：同 ID 修改 utterance/expect/world 必须改变相关内容身份，任务选择变化须可辨识，稳定等价输入具有稳定身份。
- 覆盖有效已加载身份、缺失/畸形身份拒绝、不能将本地目录或 env 请求值冒充远端 effective 值、脏工作树记录与秘密不落日志。元数据写入错误必须向上报告。
- 运行 focused Go tests；如涉及 Node 则按其包规则运行相关测试与 build；`just docs-check`。真实模型 pass 与本实现任务分开验收。

## 证据

- 认领已在协调工作树登记并复核，基线 `624d6861`。图片组仍占用其资源，不做共享模型配置或服务重启。
- 合同裁定：原“全部身份在 trial 前确定”不符合当前模型延迟解析链。批次启动记录 incomplete，任务/world/工作树/health 预检失败即停止；每个实际模型请求 checkpoint 提供有效 SDK 配置，批次收尾核对全部 trial 与冻结身份后才能 complete。缺模型请求不能用 env 或配置预估值代替。
- 实现：删除旧 ID-only writer，`eval_provenance_test.go` 使用现有 `canonjson` 保存选中 task/world 的 `inputs.json` 与内容 hash；health 提供 harness/已加载 catalog hash。Git 身份覆盖 tracked/untracked 内容、状态与 commit；不输出内容或路径列表。`provenance_version=l2-content-v1` 区别旧 hash 合同。快照与初始 metadata 以 exclusive create 写入，不覆盖历史；后续更新使用同目录临时文件 + rename，写入/关闭错误向上返回。
- Pi `requestConfiguration` 读取已解析 SDK model、API、endpoint hash、thinking level、summary/verbosity/service tier 选项及上下文/输出上限；实际 checkpoint 同时保存 catalog hash。未增加表或端点，不改变模型选择、请求参数或执行状态转换。此身份描述 SDK 配置，不声称供应商最终执行参数；仅增加诊断字段，不回填历史 checkpoint。
- L2 逐次关联 checkpoint 与 invocation 的 provider/model，检查 harness/Skill 身份和配置形状；白名单导出到 trial details 与 transcript。批次结束检查每个 task/trial 唯一且齐全、真实终态、配置一致、输入快照与 checkout 未改变；complete 允许业务 FAIL，缺身份/漂移/缺样本为 invalid，中止保留 incomplete。全程固定 checkout 仍为采证前置，首尾 hash 不提供恶意修改后还原的防护。
- `go test -C go ./internal/agent -run '^TestL2' -count=1 -v` 通过（0.796s）；覆盖同 ID 修改 utterance/expect/world、等价 JSON、dirty tracked/untracked 内容、缺失 Skill、现有目录拒绝覆盖、写失败和 complete/invalid 判定。
- 带开发环境的 scoped Go 测试包含真实 Pi + 隔离 PG + 假 provider 的双 attempt 请求身份读取，以及 world/grader/loader/终态观察回归，全部通过（26.184s）。新增导出边界断言后：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent -run "^(TestL2|TestDurableAnswerCreatesNewAttemptAndInjectsPiToolResult|TestEvalGoTerminal|TestEvalL2TrialObservationFailureSkipsBusinessGrading|TestEvalWorldsSeedFourKinds|TestEvalStateGraderSeesRename|TestLoadEvalTasks)" -count=3 -race'` 通过（23.248s），17 个顶层测试重复 3 次，无 race 报告。
- Node 初次全套 253 pass / 2 skip / 1 fail，失败为进程启动 10s 未观察到 listening，未出现本次身份断言失败；确认没有残留测试进程后，单独恢复用例 4/4 通过（8.57s），随后原 `just agent-service-test` 全套 254 pass / 2 skip、35 files 通过（11.05s）。没有放宽超时或改恢复代码。`pnpm --dir agent-service build` 通过。
- Node 新增断言覆盖模型未解析拒绝读取、实际 env override、后续 config 变更不冒充已加载模型、endpoint/key 不泄露及实际 checkpoint 字段。Go/Pi fake provider 调用验证了 PG 中真实落盘字段可读取，不是仅校验手写 metadata。
- 审核者：主代理-agent-0905-0458（自审）；专属 diff 未包含图片组改动、Skill/任务/grader 修改、密钥或真实旧产物。Issue 完成，交付随本任务提交；L2/P2 业务门槛仍未通过，真实新批次待独立合同校正和冻结输入。
- 归档后 `just docs-check` 与 `git diff --check` 通过；共享图片组状态在检查期间曾短暂不同步，复核其任务文件与索引已一致，没有将图片组改动纳入本交付。全部本任务测试进程已结束。
