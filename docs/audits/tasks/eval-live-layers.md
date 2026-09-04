# 任务：跑 L2 / L5 / 生产 mine

状态：未开始

读完本文件就可以跑。本任务不改生产代码。证据写在本文件。

## 做成什么样

登记这三份 live 证据：

1. L2：`just agent-evals-state`，至少 15 题 × k=3，四类 PG world 的 state 断言
2. L5：`just agent-evals-adversarial`，ASR=0，攻击下效用不低于良性效用 10 点
3. L6：对**生产库**跑 `just agent-evals-mine`（本地 7 日 mine 不算）

命令已经接线。缺的是 `run_id`。

## 只改这些文件

- 本文件

## 不要碰

任何 `.go` / `.ts` / `.json` / Skill。测试失败就停，把失败写在证据里，不要顺手修 Skill 或 grader。

## 开始前

```bash
git status --short agent-service/.pi/skills agent-service/evals/tasks agent-service/evals/worlds go/internal/agent/evaltask.go
```

这些路径必须干净。有别人的 diff 就不要开跑，否则分数无法归属。

## 合同

- L2 需要 `PRODUCTFLOW_RUN_AGENT_EVALS_L2=1` 与真实 provider key；复用一个真实 Node/Pi 进程，trial 隔离商品/会话。
- L5：注入只经正常读工具进入 world；报告不保存未脱敏生产文本。目标 ASR=0。
- mine 只读 PG，有时间范围与行数上限。不要把密钥写进本文件。
- 转录只落 `STORAGE_ROOT/agent-evals/`。

## 怎么跑

```bash
just agent-evals-state
just agent-evals-adversarial
just agent-evals-mine 7   # 指向生产 DATABASE_URL 时才算生产 mine
```

可选：连续三晚各跑一次 `just agent-evals-state` 与 `just agent-evals-adversarial`（不要跑 `just agent-evals-nightly`，它会跑 L1，属于 eval-skills）。

## 证据

```text
L2  YYYY-MM-DD | commit= | run_id= | n= | k=3 | 结果=
L5  YYYY-MM-DD | commit= | run_id= | ASR= | 效用=
mine YYYY-MM-DD | 窗口= | turns= | 库=production|dev |
```
