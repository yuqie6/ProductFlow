# 任务：L4 人工标签与 kappa

状态：阻塞
类型：证据
认领者：—
认领于：—
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：无（kappa 达标后由评测组章程发「judge 计入 pass」刀）

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](README.md)。执行前读取适用仓库规则、当前实现、测试和 diff。

## 前置与并行

- 前置：由人类完成 50 条样本的逐维度标签，提供填好的 jsonl。Agent 可以导出、检查和导入，不代替人类标注。
- 冻结输入：绑定 run 的转录、评分 rubric 与 judge 配置，采证时记录版本；不得改写旧 run。
- 运行资源：使用独立标注产物；不得覆盖其他人的 filled 文件。

## 阻塞与交接

- 原因：2026-09-05 当前仓库 `agent-service/evals/labels/` 只有 README，尚无提交的人工标签，也未提供已填写输入。
- 解除条件：用户提供或指定人类已完成的标签文件，维护者确认与绑定 run 匹配后恢复开放。
- 跟进者：主代理向用户收集人工标注输入。
- 交接：尚未认领；本 issue 没有执行中的进程或待移交实现 diff。

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

完成条件：真实人工标签按约定范围导入、有效校准报告与逐维度判读齐全。kappa 未达标可关闭本次采证 issue，但父章程继续标未校准，不能开启 judge 计入 pass。

## 证据

- 标签文件路径：
- 条数：
- kappa 按维度：
- 命令与日期：
