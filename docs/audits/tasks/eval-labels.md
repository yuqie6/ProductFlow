# 任务：L4 人工标签与 kappa

状态：开放
认领者：—
认领于：—
父账本：agent-eval-system.md
完成后可拆：无

读完本文件就可以做。认领前不要改「只改这些文件」。认领步骤见 [README.md](README.md)。这是填表 + 导入，几乎不改生产代码。

## 做成什么样

50 条人工标签进仓库，`judge-calibrate` 对每个计入 pass 的维度给出 Cohen kappa。kappa < 0.7 的维度继续标 `uncalibrated`，不得改变 deterministic pass。

模板已经生成：`STORAGE_ROOT/agent-evals/labeling/labels.template.jsonl`（绑定 run `20260904T174405Z-fa2667fa`，200 行、`score=null`）。仓库 `agent-service/evals/labels/` 目前只有 README。

## 只改这些文件

- 新建 `agent-service/evals/labels/*.jsonl`（import 之后的可提交标签）
- 本文件

允许只读：`evals/rubrics/<skill>.md`（评分标准）、模板 jsonl。不要改 rubric 正文来迁就分数。

## 不要碰

- Skill、任务 JSON、grader、judge 计分是否计入 pass 的开关
- 其它账本

## 合同

- 每个维度独立：`{score, reason, unknown}`。
- 未校准、样本不足或 unknown 过多时只报趋势。
- 标签是脱敏的。不要把密钥、URL token、媒体字节写进 jsonl。
- 本任务绑定既有 run `20260904T174405Z-fa2667fa`。不要为了标注去改那个 run 的转录。

## 怎么验收

```bash
# 若模板还在
just agent-evals-export-labels 20260904T174405Z-fa2667fa
# 填写后
just agent-evals-import-labels <filled.jsonl>
just agent-evals-judge-calibrate <human> <judge>
```

把 kappa 表抄到下面。每个打算计入 pass 的维度都要 >= 0.7，否则保持 uncalibrated。

## 证据

- 标签文件路径：
- 条数：
- kappa 按维度：
- 命令与日期：
