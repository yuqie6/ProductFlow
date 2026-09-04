# 验收任务板

组织按内部业务组运转：每个 `docs/audits/*.md` 是一组的章程。组从章程里**发布** issue（本目录一份指导 = 一张 issue）。Agent 接活前必须在本板和任务文件里**认领**，别人才能看见谁在做。一人同一时刻只认领一份。

章程不是 issue。禁止从未认领的任务改它的独占文件。尚未发单的「下一刀」只写在各组流水线里，**不能认领、不能当开工许可**。

用户随口修的小 bug 不走本板，除非明确要求写成 `tasks/` 里的一份指导。

## 业务组

| 组 | 章程 | 组状态 | 当前开放 | 下一刀（未发单） |
|---|---|---|---|---|
| 评测 | [agent-eval-system.md](../agent-eval-system.md) | 开工 | Skill 硬化、L3 用户模拟、L4 标签、Go loader、L2/L5/生产 mine | `eval-sim-live`（user-sim 归档后）；`eval-production-tasks`（生产 mine 已有 Turn）；`eval-nightly`（L2+L5 已有 run_id）。kappa 达标后再由章程发「judge 计入 pass」 |
| 壳进化 | [agent-self-harness.md](../agent-self-harness.md) | 开工 | P1 壳工件 | P1 归档时发 `harness-attribution`（P2）。P2b–P7 等对应阶段完成再拆，禁止提前发单 |
| 画布 | [canvas-test-system.md](../canvas-test-system.md) | 开工 | 运行中检查器打字 | `canvas-c4-remainder`（整图跑中途撤销 + 浏览器文稿 409 即停）。C0–C3、C5、C6 已收工 |
| 生图测评 | [image-quality-eval.md](../image-quality-eval.md) | 开工 | 过线池 + 抽样 live | 合计仍不足则归档时再发一轮扩池。闸门未过不另发刷分任务。ingest/admit/harness 已接线 |
| 性能 | [performance-governance.md](../performance-governance.md) | 开工 | 生图详情有界、PENDING→SENT 时延、admission 指标 | 当前三份都归档后再由章程发 `perf-agent-session-plan`（PERF-12 目标规模 payload/query plan）。SaaS 分租户不走本板 |
| 生产可靠性 | [agent-production-readiness.md](../agent-production-readiness.md) | 值班 | 无 | 不发实现刀。G-06 等评测组登记可采信的 `run_id` |
| 运行时所有权 | [agent-runtime-ownership.md](../agent-runtime-ownership.md) | 关闭 | 无 | 不再发单 |

## 看板

按组排列。认领只看「状态」列。归档后的文件在 [`archive/`](archive/)，不再出现在下表。

### 评测

| 任务 | 状态 | 认领者 | 认领于 |
|---|---|---|---|
| [eval-skills.md](eval-skills.md) | 开放 | — | — |
| [eval-user-sim.md](eval-user-sim.md) | 开放 | — | — |
| [eval-labels.md](eval-labels.md) | 开放 | — | — |
| [eval-go-loader.md](eval-go-loader.md) | 开放 | — | — |
| [eval-live-layers.md](eval-live-layers.md) | 开放 | — | — |

### 壳进化

| 任务 | 状态 | 认领者 | 认领于 |
|---|---|---|---|
| [harness-artifact.md](harness-artifact.md) | 开放 | — | — |

### 画布

| 任务 | 状态 | 认领者 | 认领于 |
|---|---|---|---|
| [canvas-inspector-midrun.md](canvas-inspector-midrun.md) | 开放 | — | — |

### 生图测评

| 任务 | 状态 | 认领者 | 认领于 |
|---|---|---|---|
| [image-eval-pool.md](image-eval-pool.md) | 开放 | — | — |

### 性能

| 任务 | 状态 | 认领者 | 认领于 |
|---|---|---|---|
| [perf-imagesession-detail.md](perf-imagesession-detail.md) | 开放 | — | — |
| [perf-dispatcher-latency.md](perf-dispatcher-latency.md) | 开放 | — | — |
| [perf-capacity-metrics.md](perf-capacity-metrics.md) | 开放 | — | — |

状态只有：`开放`（可认领）、`认领`（有人在做）、`完成`（已提交、待归档）。

## 状态机

```text
开放 --认领并提交--> 认领 --做完自审提交--> 完成 --归档并拆下一步--> archive/ ，新任务为开放
                \--放弃并提交--> 开放
```

## 接任务（认领）

1. 读本看板。只能挑 `开放`。你已经有一份 `认领` 就不要再接。
2. 对照该任务「只改这些文件」：与任何已 `认领` 任务的独占文件相交则不准接。
3. 只读**这一份**任务文件。不要读总账本开工，也不要去做「下一刀（未发单）」。
4. 在任务文件写：`状态：认领`、`认领者：<短名>`、`认领于：<ISO 时间>`。短名用 `主代理` 或 `子代理-<任务文件名>`。
5. 同步改本看板对应行。
6. **先提交认领**，暂存区只能有该任务文件和本 README：

```text
chore: 认领 tasks/<文件名>
```

7. 然后才改「只改这些文件」里的代码。未完成步骤 6 不准开工。

放弃：状态改回 `开放`，认领者/认领于清空，看板同步，提交 `chore: 放弃认领 tasks/<文件名>`。不要留下半截独占 diff。

## 做完

1. 按任务文件验收。只审本任务 `git diff`。行为符合指导、没改别人的文件、无密钥与调试残留。
2. 任务文件 `状态：完成`，证据写在该文件里。
3. 提交功能（任务文件 + 独占代码 + 本任务要求的活文档）。message 写 why。不 push。暂存区不得包含他人认领路径或未归档的下一刀。
4. **归档**：`git mv` 该文件到 `archive/`。看板删掉那一行。在父账本把「当前开工」从这份文件改成下一份（若有）或删掉指针。
5. **拆下一步**：看任务文件「完成后可拆」。不是「无」则按 [`_template.md`](_template.md) **各新建一份** `开放` 任务（合同从父账本抄进新文件，让下一任只读新文件）。写入对应组的看板。不要认领它们。组流水线里尚未写进「完成后可拆」的刀，留给主代理按章程发单，组员不得私自发。
6. 提交 `docs: 归档 tasks/<旧>，开放 <新或无>`。
7. **停下等分配。** 禁止接着做刚拆出来的下一份，也禁止再认领第二份。

并行时同一时刻只有一个 git 写者；认领提交、功能提交、归档提交都排队。子代理若不能碰 index，把认领/完成写进文件后交给当前 git 写者马上提交认领或功能，自己仍不准开第二份任务。

## 新任务从哪来

只从该**业务组章程**（父账本）的未完成阶段/缺口拆，不要发明新方向，也不要把 ROADMAP 产品项塞进本板。拆出来的文件必须自洽：做成什么样、只改这些、不要碰、合同、怎么验收、证据栏。下一任 agent 不得被要求再去读总账本才能开工。
