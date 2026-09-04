# 任务：运行中检查器打字（画布 C4 尾项）

状态：未开始

读完本文件就可以改代码。不要去改 Go 文稿 cook / 搜索器。

## 做成什么样

`just web-e2e-canvas-document` 覆盖：整图或节点 cook **正在跑**时，用户在检查器里打字保存。保存后的 live 文稿在 adopt 时不得被生成结果盖掉（O2）。

现有 `web/e2e/canvas-document-mock.spec.ts` 已经能在检查器里 `getByLabel` 手填并自动保存，再点改写/补全/替换。缺的是**运行中**再打字。

裁判是 `document_origin` / `pending_candidate_artifact_id` / live `config_json`，不是文稿文采。prompt 与 image 必须是 mock。

## 只改这些文件

- `web/e2e/canvas-document-mock.spec.ts`
- 本文件

检查器字段已经有 label，优先只改 spec。只有在运行中输入被禁用、spec 无法打字时，才允许改 `web/src/pages/workbench/canvas/GraphNodeInspector.tsx` 让运行中仍可编辑当前节点文稿（这是产品合同：Turn/run 不得锁画布）。不要改 Go。

## 不要碰

- `go/internal/graph/` 权威测试与 cook 实现（C0–C3、C6 已完成）
- `just web-e2e-live-graph`（真模型整图，不覆盖 rewrite）
- Agent 评测、Skill

## 合同

- `complete|rewrite|replace` 必须 `scope=node`+`force`；apply 前 live 不变。
- live 相对 run snapshot 已分叉 → 生成进 `pending_candidate`，不改 live config。
- 供应商绑定必须是 mock。需要已起的 `just dev`。

```bash
just web-e2e-canvas-document
```

## 证据

- spec 用例名：
- 是否改了 GraphNodeInspector：是/否
- 日期 / 结果：
