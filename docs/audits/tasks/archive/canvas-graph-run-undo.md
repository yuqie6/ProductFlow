# 任务：整图跑中途撤销不得盖掉撤销后的 live 文稿

状态：完成
类型：实现
认领者：主代理-0905-1423
认领于：2026-09-05T14:25:00+08:00
业务组：工作流体验
父账本：canvas-test-system.md
完成后可拆：无（浏览器侧整图跑中途撤销由 [canvas-c4-remainder.md](../canvas-c4-remainder.md) 单独验收）

本任务已关闭。认领与归档步骤见 [Issue 协议](../README.md)，业务组结论见[父账本](../../canvas-test-system.md)。

## 问题来源

父账本 [真实用法矩阵](../../canvas-test-system.md#真实用法矩阵)：整图跑中途撤销未单独覆盖。现有具名钉死 [TestAdoptSkipsOverwriteWhenUndoDuringBriefCook](../../../../go/internal/graph/authority_inject_test.go) 走 `scope=node` + `force` + `document_action=rewrite`，不是用户点工具栏整图跑（`scope=graph`）。C2 搜索器含 `undo`，不代替本组合的具名合同测试。C-03 验收原文已写「整图跑中途 … / undo」，证据名单缺少对应 `scope=graph` 测试。

## 做成什么样

`just go-test` 中增加一条贴近合同的具名测试：用户先留下可撤销的文稿编辑，再 `POST .../runs` 且 body 为 `{"scope":"graph"}`（禁止改成 `scope=node` 或加 `force` 来躲开 frozen/409）。在该次 GraphRun 仍由 `executeLocally` 执行、供应商回调尚未返回时，对同一图 `POST .../undo` 一次且不重试。跑完后：

- live `config_json` 仍是撤销后的文稿，不得被 mock provider 结果盖掉（O2/O3）
- 撤销请求必须实际成功（HTTP 200，`undone=true`）；失败不得改断言或改成跑完再 undo
- 若该节点仍被 cook，生成进 `pending_candidate`，`disposition` 不得把用户撤销结果写成 live

裁判是 live `config_json` / `document_origin` / `pending_candidate_artifact_id`，不是文稿文采。

## 前置与并行

- 前置：无。C4 浏览器撤销是另一张 issue，不阻塞本单。
- 冻结输入：本单不改 C2 搜索预算、不改 mock 供应商文案合同。
- 运行资源：包测走 `testdb.Pool` 隔离库与 `executeLocally`，不占用共享 `just dev`、不切 mock provider、不暂停 worker。可与 [image-eval-pool.md](../image-eval-pool.md) 的淘宝采集并行；不得与任何暂停 `productflow-worker` 或切换共享 provider 的窗口抢同一进程。

## 只改这些文件

- `go/internal/graph/authority_inject_test.go`（新增 `scope=graph` 中途 undo 具名测试及必要夹具）
- 若测试证明 cook/adopt 在整图跑中途 undo 后会写 live：仅修复 `go/internal/graph/` 中 adopt / `execute_node` / cook 选择这条因果链上的实现，并在本文件补实际路径
- 本文件

实际修改：仅 `authority_inject_test.go` 与本文件。现有 `adoptGeneratedDocument` 已按 snapshot 分叉拒绝覆盖，无需改 cook/adopt。

## 不要碰

- `web/` 与 `just web-e2e-canvas-document`（留给 canvas-c4-remainder）
- `authority_search_test.go` 与搜索 walk/深度预算
- 检查器草稿版本语义 / AR-01 基线传递（已由 [canvas-inspector-midrun](canvas-inspector-midrun.md) 交付）
- 评测、Agent Skill、图片池

## 现在代码在哪

- 节点 rewrite 中途 undo：`TestAdoptSkipsOverwriteWhenUndoDuringBriefCook` + `midRunUndoEditor.GenerateCreativeBrief` 发一次 HTTP undo。
- 整图跑中途改文稿：`TestAdoptSkipsOverwriteWhenPromptEditedDuringBriefCook`、`TestAdoptSkipsOverwriteWhenVisualEditedDuringGraphRun` 均 `scope=graph`，回调里一次 PATCH，开跑时的 `base_graph_revision`。
- 文稿节点非 `force` 时 `planned_action=frozen`（`select.go`）；先手填再整图跑可能不再回调 brief/prompt。夹具须在仍会执行的供应商回调里 undo（例如仍 generate 的节点），或先构造「快照后 live 因 undo 分叉」的交错，不得把提交改成 `force`。
- `Undo`（`history.go`）不检查 active GraphRun，运行中撤销走普通 WriteTx。
- 测试纪律见父账本：禁止循环重试 409；cook 协程不得 `t.Fatal`。

## 合同

- O2：live 相对 run snapshot 已分叉 → 不改 live config；生成进 `pending_candidate`
- O3：origin 为 `authored|generated|collaborative` 时，无 `force` 的 graph 跑不得把 provider 结果写进 live
- 用户点整图就 `scope=graph`；本单不得用 `scope=node` 代替
- D-01 / D-04：HTTP 夹具 + `executeLocally`，禁止 SQL 改 `config_json`

## 怎么验收

```bash
bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph -count=1 -run "TestAdoptSkipsOverwriteWhenUndoDuringGraphRun|TestAdoptSkipsOverwriteWhenUndoDuringBriefCook|TestAdoptSkipsOverwriteWhenPromptEditedDuringBriefCook" -p 1'
just docs-check
```

新测试名必须含 GraphRun / `scope=graph` 语义（推荐 `TestAdoptSkipsOverwriteWhenUndoDuringGraphRun`）。既有节点 rewrite undo 仍须通过。若无实现改动，Go 包测通过即可；若改了 cook/adopt，按改动边界补回归，不得削弱 O2/O3。

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：主代理-0905-1423
- 交接：无未提交 diff、运行进程或资源占用。不暂停 worker、不改共享 provider。

## 证据

```text
测试：TestAdoptSkipsOverwriteWhenUndoDuringGraphRun
夹具：手填 brief 后 POST scope=graph；midRunGraphUndoEditor / midRunGraphUndoImage 在仍会 cook 的供应商回调里 sync.Once 发一次 HTTP undo，不重试。
断言：undone=true；live brief.goal 回到 seed，不是手填稿，也不是 MOCK-AUTHORITY-BRIEF-GOAL。
实现：未改 cook/adopt；adoptGeneratedDocument 已按 liveDocumentDivergedFromSnapshot 跳过覆盖。
命令：bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph -count=1 -run "TestAdoptSkipsOverwriteWhenUndoDuringGraphRun|TestAdoptSkipsOverwriteWhenUndoDuringBriefCook|TestAdoptSkipsOverwriteWhenPromptEditedDuringBriefCook" -p 1'
日期 / 结果：2026-09-05 ok github.com/yuqie6/productflow/internal/graph 6.290s
just docs-check：pass
审核者 / 结论：主代理-0905-1423 自审通过，非独立审核。
Issue 结果：完成。C3 整图跑中途撤销具名钉死已进 just go-test。
业务门槛结果：工作流体验 C-03 整图 undo HTTP 证据已补；C4 浏览器撤销仍见 canvas-c4-remainder。
```

- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/canvas-graph-run-undo.md` 查询）。
