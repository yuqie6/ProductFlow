# 任务：补齐 Agent 素材整理的生产可见事实

状态：完成
类型：实现
认领者：主代理-eval-quality-0905-1713
认领于：2026-09-05T17:13:05+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：维护者独立更新观察夹具并冻结完整测评，再安排 eval-skills A/B

遵循 [Issue 协议](../README.md)。取得确认认领后调查和实现；原发布不授予执行所有权。

## 问题来源

[测量合同审计](eval-observable-input-contract.md) 发现生产 `ListLibraryAssets` / `InspectLibraryAssets` 返回 `AssetMetadata`，没有素材 revision、tag_names、is_archived。目录名称无法发现目标目录 ID；已归档素材被读取接口排除。`propose_global_draft` 的整理操作要求 expected_revision、before 及目标身份，模型不能从实际可见事实构造可靠请求。旧桩返回内部字段掩盖了问题。

## 做成什么样

商家请求改名、移动、标签、归档、恢复或关联时，Agent 能通过受权限约束的生产读取取得必要身份、当前值和 revision，再提交草案；不会猜 revision、默认 before 或伪造目录 ID。未授权或不存在的资源仍明确失败。

## 前置与并行

- 前置：本单使用已确认的生产字段缺口，可独立修复；上游测量归档给出回归及观察边界。不得在测量任务仍占用夹具时修改评测输入。
- 冻结输入：`.pi/skills/`、`agent-service/evals/` 的题目、grader、world、fixtures、split 与旧 A 产物全部只读。行为修复与评分修订分版本审核；本单不能清掉 observability_blocker 让自身过题。
- 并行：与 eval-skills 涉及同一生产面时串行；当前该任务仍阻塞并保留认领，维护者须确认允许的生产路径。PG 使用独立测试库，不调整共享 provider/worker/浏览器或图片池。

## 只改这些文件

- `go/internal/agent/tools_assets.go`、`dto.go` 及对应 Agent 内部读取路由/DTO/聚焦测试。修改共享 AssetMetadata 前扫描商品图库和全局图库所有读写者。
- 沿真实调用链确需的 `go/internal/library/` 现有查询，不新增平行整理状态或绕开现有权限。
- 认领后范围裁定：`drafts.go` 确认路径未核对 before/工作流预期状态，直接影响本单错误输入配对验收；允许在该事务边界补齐校验与相邻测试。读取增加独立全局素材 DTO，商品图库合同不变；目录与目标工作流关联仅返回有界查询结果。
- 若读取 wire 必须扩展，维护者核对后允许 `agent-service/src/productflow.ts`、对应 tool schema/工具适配及生成合同工件；冻结 Skill 与评测不动。
- 本文件、父账本和必要活文档；若根因超出范围，交维护者调整任务，不绕过当前所有权。

## 现在代码在哪

- `go/internal/agent/tools_assets.go`：`libraryMeta`、`ListLibraryAssets`、`InspectLibraryAssets`；`go/internal/agent/dto.go`：`AssetMetadata`。
- `agent-service/.pi/skills/media-library-organization/SKILL.md`：读取 before/revision/目录要求；`go/internal/agent/global_draft_schema.json`：草案格式。
- `go/internal/library/`：资产查询、目录与组织草案应用；`go/internal/agent/eval_user_sim_host_test.go`、`agent-service/evals/go-world.test.ts` 已证实 Agent 读缺字段与独立合法草案确认可持久化，两者不等价。

## 合同

全局 scope、资源身份、乐观并发、确认后写入及已归档素材权限沿用生产合同。读模型不得泄漏 bytes、内部密钥或无关资源；不要用 expected_revision=0、空 before、直接 Service 绕过 wire 验证。发现资源与用户授权修改资源分开。

## 怎么验收

- 真实 Go HTTP + 隔离 PG：从 Agent 实际读返回构造 schema 合法草案，分别覆盖改名、移入现有目录、标签、归档后读取并恢复、关联工作流；确认前不写，确认后复读值和 revision 正确。
- 错 revision、错误 before、错误 scope、不存在目录、非目标资产与未确认的配对失败；商品图库投影回归。
- `just agent-service-test`、`pnpm --dir agent-service build`、受影响 Go packages 聚焦与合同测试、`just docs-check`、`git diff --check`。
- 完成生产读取回归不等于 A/B 能力通过。独立评测 owner 再决定观测阻塞解除、更新 Go 生成夹具及冻结输入；禁止本单改题自证。

## 阻塞与交接

- 跟进者：协调主代理。发布前已检索现有 board，无独立素材读字段修复单；eval-skills 仍保留占用，认领本单前由维护者明确串行窗口与写入路径。
- 认领确认：用户授权本协调主代理继续质量任务；2026-09-05T17:13:05+08:00 核对任务、看板和当前 diff 后登记。eval-skills 的 A 已结束、B 阻塞，保留其 Skill 与固定副本；本单独占生产 Agent/library 读取及紧邻测试，不碰评测文件。画布交付使用独立栈，图片池 provider/worker/浏览器不动。无认领提交。

## 交付证据

- 交付定位：随本任务提交，通过本归档 Git 历史定位。执行者及自审：主代理-eval-quality-0905-1713；未冒充独立审核，未修改被测 Skill 或评测评分文件。
- 全局素材使用独立 LibraryAssetMetadata，提供 revision、folder_id/folder_name、tag_names、is_archived；商品图库 AssetMetadata 不变。默认隐藏归档素材，恢复前通过 include_archived 显式读取元数据，已归档原图内容与多模态 inspect 仍按原权限拒绝。
- 目录独立有界分页：folder_query / folders_after_id / folders_next_after_id，复用 library.Folder；工作流按明确 workflow_id 返回 title/revision 及本页每项 linked 真值，未返回的素材不推断未关联。不返回媒体 URL、路径、bytes。
- 确认事务核对 expected_revision 和完整 before，缺失/过期/错误前值失败；移动前锁目标目录，关联先锁工作流并核对标题/revision/expected_linked。工作流、目录、素材遵循现有直接命令的锁顺序，资产按 ID 排序加锁，冲突不部分提交。
- 发现三项旧恢复测试用空 operations 作为成功确认夹具；替换为实际上传素材的合法改名草案，保留并发锁顺序、恢复不重复、任务投影断言。未降低断言或更改 grader。
- `library_read_contract_test.go`：真实内部 HTTP 读取所得事实构造 rename/move/set_tags/archive/restore/link_workflow，走生产 validate、AppendOrganizationDraftRevision 与公开 confirm HTTP，再复读；确认前资产不变、确认后值/revision/关联正确。草案追加使用生产 Service，未声称完整 Pi journal E2E。
- 负例：错误/零 revision、错误 before、把甲的 before 用于乙、不存在目录、工作流标题/revision/关联预期错误均确认失败；三页目录无重复，product scope 不能读取全局库，默认列表不泄露归档项。
- `src/tools.test.ts`：执行真实工具适配和 ProductFlowClient HTTP 编码，字段抵达模型结果；从返回 before/folder/workflow 构造六种草案，通过当前 global draft JSON schema。HTTP 在此 Node 单测为确定性 server，真实数据库结果由上述 Go 测试独立验证，不冒称同一次模型端到端批次。
- 2026-09-05 `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent ./internal/library -count=1 -p 1'`：通过（Agent 53.517s、library 1.375s，隔离 testdb）。未运行真实模型 gate。
- `just agent-service-test`：35 文件通过、1 跳过；289 tests passed、6 skipped。`pnpm --dir agent-service build` 与显式 tools.test.ts strict TypeScript 检查通过。
- 最终构建发现 src 测试静态引用 evals 校验器超出 rootDir；改为使用已有 TypeBox 直接读取生产 JSON Schema，未扩大编译范围或修改冻结评测。修正后 tools.test.ts 26/26 与生产 build 通过。
- 17:47 全量重跑曾在未修改的 `process-restart.e2e.test.ts:208` 失败：SIGKILL 前后确认批次预期 `[[],[1]]`、实际 `[[],[1,2]]`；17:48 单独复跑 4/4 通过，17:49 原全量命令复跑 289 通过、6 跳过。保留这次不稳定失败，不将重跑通过视为该恢复时序问题已修复。
- 自审将标签断言由数量改为排序后精确值比较；最终 `TestLibraryRead` 的 `-race -count=1` 回归通过（3.181s）。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent -run "TestLibraryRead|TestConfirmDraftAndAppendTerminalDoNotDeadlock" -race -count=1'`：通过（3.306s）。`just docs-check` 与 `git diff --check` 通过。
- manifest 变动同步 Go 与 Web 生成 hash；Web `test:run` 91 文件/647 测试通过，lint/build 通过，build 有既有 >500KB chunk 提示。无可视 UI 修改。
- `git diff --name-only -- agent-service/evals agent-service/.pi` 为空。原 12 条不可观测标记和旧 L3 缺 revision 断言仍冻结；本单未运行该旧 opt-in L3 素材断言，也不以它证明新合同通过。
- 自审核对因果范围、字段所有读写者、真实结果与测试边界、无密钥、无 dev/provider 设置写入。Issue 完成只表示生产读取/确认合同修复；测评和自进化基线资格不自动通过。

后续由 [观察刷新与冻结](../eval-library-observation-refresh.md) 独立核对生产读取与桩/host，更新旧缺字段断言并决定哪些 observability_blocker 可解除。随后才能按用途清单采有效开发批次。保留 eval-skills 原 owner 与固定 A，不重用旧诊断成绩。
