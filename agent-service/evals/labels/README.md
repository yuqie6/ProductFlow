# 人工评审标签

本目录保存可提交的 L4 标签 JSONL。每行：

```json
{"task_id":"run-diagnosis-inspect-failed-node","trial":1,"skill":"run-diagnosis","dimension":"失败节点一致","score":1}
```

用 `just agent-evals-export-labels <run_id>` 从落盘转录生成模板，填写 score 后 `just agent-evals-import-labels <file>`。校准：`just agent-evals-judge-calibrate <human.jsonl> <judge-scores.jsonl>`。未满 50 条或 kappa < 0.7 时，judge 分数只报趋势，不计入 pass。
