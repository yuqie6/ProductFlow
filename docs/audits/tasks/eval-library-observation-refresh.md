# 任务：独立复核素材观察合同并冻结可用评测输入

状态：开放
类型：实现
认领者：—
认领于：—
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：协调者确认 eval-development-baseline 与 eval-skills 的新基线执行窗口

遵循 [Issue 协议](README.md)，认领确认后再调查和修改。

## 问题来源

[测量修复](archive/eval-observable-input-contract.md) 保留 12 条素材任务的 observability_blocker；其中 10 条属于 L1。[生产读取修复](archive/agent-library-read-contract.md) 新增真实 revision/before、目录发现和归档读取、指定工作流关联事实，但没有修改考题或评分。生产修复不能自行清除自身评测阻塞。

## 做成什么样

逐条证明实际可见输入足够支撑素材操作，观察桩与 Go 返回合同一致；只有证据完整的阻塞才能移除。正例能识别正确业务结果，错误目标、错误 before/revision、额外操作及未知结果仍不通过。独立冻结输入后才允许下游运行候选比较；本单不宣称真实模型达标。

## 前置与并行

- 前置：[生产读取修复](archive/agent-library-read-contract.md) 完成交付；通过归档 Git 历史取得固定提交。先验证该提交的生产回归，再消费其读取合同，不能依赖进行中的变更。
- 冻结：生产 `agent-service/src/`、`.pi/skills/`、Go 业务实现及旧 A 产物不动；不得用 Skill 硬编码题目事实。
- 运行：隔离 Node 临时目录与 `<dbname>_gotest_agent`，PG 串行；不改 dev/provider/worker/图片池/浏览器。eval-skills 保留原认领但未运行新 A/B；此处冻结完成前仍不得启动同一输入的候选比较。

## 只改这些文件

- `agent-service/evals/stub-world.ts`、对应测试、Go 观察快照生成测试及 `fixtures/`；只同步生产必要读取形状，不重做业务事务。
- 12 条带 observability_blocker 的素材任务、必要 reference 读取、`go-world.test.ts`/host 映射；旧的“生产没有 revision”断言须用正反实际观察替代，不能直接删除有价值测试。
- 必要 schema/loader 同步、覆盖和哈希测试；评分规则、阈值、split 保持，发现确需改 grader 的独立根因交协调者先调整任务。
- 本文件、父账本、相关下游阻塞条件和必要活文档。

## 现在代码在哪

`tools_assets.go` 的 LibraryAssetMetadata/ListLibraryAssets；`organization_reads.go` 的目录独立分页与 workflow.linked；Node `tool-manifest.ts`/`tools.ts` 的 include_archived、folder_query、folders_after_id、workflow_id；`library_read_contract_test.go` 的六类 HTTP 读取→确认→复读配对。生产确认新增 before 检查，冻结 world 中的文件名/标签/目录事实须与 seed 后真实读一致，不能靠预期错误被忽略过题。

## 验收

- 逐条复核三释义、page selection、可达读取和 expect；默认不见归档资源、显式读取才能恢复；关联字段来自目标工作流实际状态；目录分页不凭空补齐目录。
- L1 桩与 Go 生成观察对照；默认/归档列表、目录续页、未匹配目标、linked=true/false、错误身份配对；失败读不当成功证据。
- opt-in Go/Pi 决策 host 的素材测试用实际读返回构造合法请求，确认前不写、确认后真实持久化；不得仅注入已知合法参考答案。租约/journal 非本单能力结论。
- `just agent-service-test`、Agent build/显式 eval 类型检查、`just agent-evals-coverage`、隔离 Go eval 回归与 Go-world 联测、`just docs-check`、`git diff --check`。
- 独立只读审核全部差异与配对证据，并独立复算 task/world/fixture hash；本单明确授权只读审核委派，不委派未认领的实现。存在 unknown/unobservable 时保持禁止能力比较，不以删题取得资格。
- 任务完成只交付固定可测输入。有效用途清单、真实开发批次、导出身份和候选验收继续由下游任务验证；不给历史成绩补发资格。

## 交接

尚未认领、未修改评测文件。生产读取修复与测量更新分提交，本单发布不等于解除下游阻塞。交付及审核证据在执行后补齐。
