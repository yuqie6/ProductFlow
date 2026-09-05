# 任务：独立复核素材观察合同并冻结可用评测输入

状态：完成
类型：实现
认领者：主代理-eval-quality-0905-1804
认领于：2026-09-05T18:04:20+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：协调者确认 eval-development-baseline 与 eval-skills 的新基线执行窗口

遵循 [Issue 协议](../README.md)，认领确认后再调查和修改。

## 问题来源

[测量修复](eval-observable-input-contract.md) 保留 12 条素材任务的 observability_blocker；其中 10 条属于 L1。[生产读取修复](agent-library-read-contract.md) 新增真实 revision/before、目录发现和归档读取、指定工作流关联事实，但没有修改考题或评分。生产修复不能自行清除自身评测阻塞。

## 做成什么样

逐条证明实际可见输入足够支撑素材操作，观察桩与 Go 返回合同一致；只有证据完整的阻塞才能移除。正例能识别正确业务结果，错误目标、错误 before/revision、额外操作及未知结果仍不通过。独立冻结输入后才允许下游运行候选比较；本单不宣称真实模型达标。

## 前置与并行

- 前置：[生产读取修复](agent-library-read-contract.md) 完成交付；通过归档 Git 历史取得固定提交。先验证该提交的生产回归，再消费其读取合同，不能依赖进行中的变更。
- 冻结：生产 `agent-service/src/`、`.pi/skills/`、Go 业务实现及旧 A 产物不动；不得用 Skill 硬编码题目事实。
- 运行：隔离 Node 临时目录与 `<dbname>_gotest_agent`，PG 串行；不改 dev/provider/worker/图片池/浏览器。eval-skills 保留原认领但未运行新 A/B；此处冻结完成前仍不得启动同一输入的候选比较。

## 只改这些文件

- `agent-service/evals/stub-world.ts`、对应测试、Go 观察快照生成测试及 `fixtures/`；只同步生产必要读取形状，不重做业务事务。
- 认领后因果核验：`evalworld_test.go` 未播种标签/归档快照，注入名称与 L1 不一致；允许修正该测试播种层。保留 world JSON 的快照语义与 revision，不改生产实现；引用前值须来自该快照实际读返回。
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

协调主代理已核对生产前置提交 `0defddef`、当前看板和工作区，确认本单认领。eval-skills 仍阻塞，旧 A 与 Skill 保持冻结；本单独占上述评测观察与 Go 测试路径，PG 串行，不占用图片池或画布运行资源。生产读取修复与测量更新分提交，认领不等于解除下游阻塞。交付及独立审核证据在执行后补齐。

## 交付与审核

- 交付定位：随本任务提交，通过本归档 Git 历史定位。主代理-eval-quality-0905-1804 执行、自审；独立只读审核 `/root/library_observation_review` 接受，未委派实现。审查结论只覆盖本单可观察性与回归，不签收真实模型能力。
- Go 播种原本遗漏标签/归档，注入名称与 L1 不一致；已恢复声明的 world 快照，revision 表示快照版本，不冒充播种调用次数。关联目标使用真实创建的工作流，global scope 路由不再凭 ProductID 推断；page filters 的 product/workflow/folder ID 均映射实际身份。
- Go 观察夹具和 L3 host 使用 `testdb.IsolatedMigrated` 一次性库，结束清理；没有重置共享 dev 或包级数据库。首次生成暴露旧包级测试库残留会污染全局列表，因此抽出 `http_test.go` 的现有服务构造器接入隔离 DB，未复制业务实现。
- `TestEvalLibraryObservationFixtures` 从生产读取生成 `fixtures/library-observations.json`：保留元数据、真实原始文件名、标签、归档、目录数量及关联真值。仅规范化随机身份，创建时间由冻结 world 播种；不按预期答案改写实际返回。
- 12 条参考草案逐一使用实际读取的 before/revision，经确认后直接复读 rename/move/set_tags/archive/restore/link_workflow 结果。增加目录三页、素材两页、跨筛选 cursor 拒绝、linked=true/false 且仅本页成员等证据。
- L1 素材桩消费 Go 快照；缺快照保持 unknown。默认不见归档、恢复要求 include_archived，目录独立分页，workflow_id 明确指定，search 规范空白并保留 LIKE 通配/转义和 cursor 签名语义。inspect 拒绝空/重复身份并规范空白。
- 新发现的错误测评：set-tags 三释义要“秋季、主推”，expect/reference 却写“秋季、主图”；已修正答案，保留用户请求。旧 rename before 的“旧名”改为真实素材名；关联题标题与 world 对齐；L5 改名三释义补齐同一目标“新名”，前值使用生产规范后的注入名称。
- 独立审核发现固定题 ID 快照会令 L5 派生素材题不可测；改为按显式 adversarial base origin 消费 Go 快照并叠加生产规范化注入。16 条派生题实际读到注入、记录 exposure，再以读到的 before 提交合法草案；没有用不存在的观察记安全通过。
- Node 配对覆盖错误 before/revision/身份、额外操作、failed/unknown 不通过；restore/link 的 expect 增加明确读取参数。未改 grader、阈值、split、生产 Agent/Skill/library 实现。原 12 条阻塞经逐条核验移除，没有删题。
- L3 Go host 转发真实列表查询参数，草案使用实际读取所得事实；错误 before 确认失败且资产不变，正确确认后由原最终状态 grader 复读。未声称完整 Pi journal/lease 崩溃恢复或真实模型批次。

## 验证与固定输入

- 2026-09-05：生产前置 `go test -C go ./internal/agent -run TestLibraryRead -count=1` 通过（1.424s）。所有 Go 命令经 `scripts/with_dev_env.sh` 载入环境，使用隔离 testdb，串行运行。
- `go test -C go ./internal/agent -count=1` 全包通过（79.735s）；最后 page folder 映射回归 `-run TestEvalLibraryObservationFixtures -count=1` 通过（22.277s）。此前失败包含旧 before 与新增测试错误地要求不存在的可选 filter；修正后无漂移，未通过重生成掩盖失败。
- `just agent-service-test`：36 文件通过、1 跳过，296 通过、6 跳过。Agent build 与显式 eval strict TypeScript 检查通过。未调用真实模型。
- `PRODUCTFLOW_RUN_AGENT_EVALS_GOPG=1 pnpm --dir agent-service exec vitest run evals/go-world.test.ts`：4/4 隔离 Go 决策回归通过。Node 默认跳过这些 opt-in 用例，不能将 skip 计通过。
- 最终复跑 Go-world 4/4（15.32s），eval strict TypeScript 与 `just docs-check`、`git diff --check` 通过；执行进程均已结束。
- `just agent-evals-coverage`：83 tasks、24/24 tools、12/12 Graph ops。覆盖率不代表模型得分。独立审核另跑 4 文件/30 测试，通过；PG 由主代理串行验证。
- 冻结：83 tasks、75 L1、11 worlds、13 fixtures，共 107 JSON；observability_blocker 为 0。完整 taskSetHash：`1283d72edd0ed6a9ffb7dcd36f63c7652e14ea8eb3a8e1fc1c7e97c57041a258`；L1：`0adae988aa519bc2ab6c0ae256150d0da9b071dc0c30dba887d5a04c01145907`。
- tasks/worlds/fixtures 的 JSON 按相对 evals 路径默认字典序排序，逐个输入 `path + NUL + rawBytes + NUL` 的聚合 SHA256：`974c857bb041d23d9065ed912781c8294a4938fc203c8b9de190a9ce5ad81bc7`。独立审核从原始 JSON 补 split、递归排序对象键并与 loader hash 交叉核对，上述身份一致。

输入阻塞已解除，[开发基线](../eval-development-baseline.md) 开放待认领，仍须冻结用途清单、运行完整 k=3 并导出有效开发包。旧 A 与历史诊断分数没有补发资格；eval-skills 保留 owner，等待协调者安排新共同版本和窗口。当前没有自进化控制器交付，不宣称自进化已经开始运行。
