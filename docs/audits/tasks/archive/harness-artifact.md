# 任务：可哈希的领域壳工件（Self-Harness P1）

状态：完成
类型：实现
认领者：主代理-agent-0905-0458
认领于：2026-09-05T05:03:00+08:00
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：harness-attribution.md（P2 归因；合同从父账本 P2 抄齐）。P2b–P7 不要在本任务归档时一起发

本文件限定交付范围；认领、阻塞、审核与关闭见 [Issue 协议](../README.md)。执行前读取适用仓库规则、当前实现、调用链、测试和 diff。本阶段完成前不实施 P2–P7。

## 前置与并行

- 前置：无；P1 不以评测分数达标作为开工条件。
- 冻结输入：本任务改动被测 runtime/policy，不能与共享该 checkout 的 Skill/L2/L5/user-sim live 同时执行。
- 运行资源：单测使用隔离夹具；`docs/ARCHITECTURE.md` 属共享文档，变更交 Git 写者排队整合，保留已有用户 diff。
- 本轮占用：主代理-agent-0905-0458 确认 eval-skills 无源码改动与运行资源并释放后认领 P1；图片采集继续独占其浏览器与样本池。P1 只运行隔离确定性测试，不触碰共享 dev/provider。ARCHITECTURE 等共享文档待现有交付完成后逐段整合，不暂存他人改动。

## 做成什么样

出现一份可哈希的 `h_t`：

- 新目录 `agent-service/harness/` 装壳工件（指令段、以后 overlay 的位置、仅执行行为配置的 runtime-control 占位）
- `go/prompts/agent/runtime-policy.md` 拆成权限/确认（冻结）与行为段（可编）
- `agent-service/src/pi-runtime.ts` 改为读入该工件，而不是只拼散落 Skill + 整份 policy
- 测试钉住规范哈希：改可编辑段则 hash 变，改冻结段的测试应失败或拒绝

生产 loop 仍是 Pi。不要复活 Go `agent-harness`，不要第二套执行器，不要提案器、Steer、playbook、热切。

## 只改这些文件

- 新建 `agent-service/harness/`（及该目录测试）
- `agent-service/src/harness.ts`：工件加载与规范哈希；替换仅拼旧整份 policy 的 `runtime-policy.ts` 与对应旧测试。
- `agent-service/vitest.config.ts`：使 harness 目录测试进入默认 gate。
- `agent-service/Dockerfile`：复制工件与 contract check 所需输入，不改运行协议或镜像依赖。
- `go/prompts/agent/runtime-policy.md`
- 打包/生成它的现有脚本（agent-service 里引用 runtime-policy 的 generate 路径）
- `agent-service/src/pi-runtime.ts` 的**读入壳**部分；不要借机拆其它职责
- `agent-service/src/pi-runtime.test.ts` 里与读入/hash 相关的用例
- `agent-service/src/pi-runtime-harness.test.ts`：生产 Pi 装配边界的隔离测试。
- `agent-service/src/pi-runtime.e2e.test.ts`：只修默认验证中复现的终态/lease release 观察竞态；沿用已有 `expect.poll`，不改任何生产 lease/journal 行为。
- `docs/ARCHITECTURE.md` 里「壳是版本化对象、pi-runtime 读 harness 工件」那一两句
- `docs/ARCHITECTURE.en.md` 对应翻译；`docs/audits/agent-self-harness.md` 的 P1 事实及归档链接由维护者整合。
- 本文件

## 不要碰

- `agent-service/.pi/skills/` 正文（本阶段不写 overlay）
- `evals/` grader、任务、runner
- Go `agent_model_invocations` 新列（那是 P2）
- journal / lease / SSE / 确认协议
- Steer 钩子、playbook、Promoter

## 合同

- 三层：Pi 仍是 L1 loop；本任务只把 L2 壳收成可哈希对象。
- 执行时 `h_t` 冻结。本阶段还没有热切。
- 权限与确认段保持「禁止代点确认 / 物化；page snapshot 不是授权」。
- 工具 JSON schema 与 Graph Command 不动。
- 明确可演化文件 / 字段与冻结段的边界。`runtime-control` 仅允许声明的执行行为配置；控制器预算、接受规则、评分 / 记录 / 谱系逻辑不属于可演化工件。P1 不实现控制器配置或候选校验器，后续阶段不得把它们塞进可编辑占位。
- hash 算法写进测试；规范序列化，不要随 JSON 键序漂移。

## 怎么验收

```bash
just agent-service-test
just docs-check
```

断言：同一工件两次 hash 相同；改行为段 hash 变；生产路径加载 harness 目录；`runtime-control` 占位不包含控制器预算或接受规则。

## 证据

- 2026-09-05 现场因果确认：现有 `runtime-policy.ts` 只读取 generated 常量，`runtimePolicyPath` 不参与加载；新工件需要独立加载 owner。`tsconfig` 的 rootDir 为 src，加载器放 src，harness 仅存工件与测试；默认 Vitest 需增加 harness 测试路径。Docker build 尚未复制 prebuild 使用的 scripts/go prompt/生成物输入，P1 将同步复制必要输入，避免发布时缺工件。
- 设计参考：[AHE v4](https://arxiv.org/abs/2604.25850v4) 的组件文件化与可归因修改；[ACE v3](https://arxiv.org/abs/2510.04618v3) 的结构化增量更新仅用于后续 overlay 设计，不提前实现控制器。Pi 继续使用现有 SDK `DefaultResourceLoader` 的 system prompt 装配；不升级依赖。
- 默认 gate 基线：独立 checkout `/tmp/productflow-agent-p1-baseline` 固定 `6b3e2885`，无 P1 代码，全量测试 205 passed / 1 failed / 2 skipped；`pi-runtime.e2e.test.ts:247` release 列表为空。候选两次全量分别在 timeout/reset 同一 release 断言失败。夹具 `runProviderFailureScenario` 在终态可见时复制列表，生产 release 在随后 finally 中完成；改为观察释放完成后再快照，另一个流丢失用例也采用文件已有的 `expect.poll`。不以重复运行偶然通过验收。

- 2026-09-05：`agent-service/harness/manifest.json` + 四段 instructions；`src/harness.ts` 为唯一工件加载 owner，`pi-runtime.ts` 模块启动时加载冻结对象；旧 `runtime-policy.ts` 及其失真的路径测试已删除，无 fallback。
- 哈希：SHA-256(canonicalJSON(artifact))，当前 `13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`；manifest 键序/绝对位置不参与身份，四段正文逐字节参与。基础 Skill 仍独立记 `skills.hash`。
- 权限：manifest 固定 `ee8a8b0c490bf90b54465afafb69f2cd7e8f9461f202167f9a1e7b326a415558`，匹配生成后的冻结 policy；错 pin、未知字段、非空 runtime-control/overlay、空或缺失指令均拒绝。无运行时热切或演化入口。
- 回归：`pnpm --dir agent-service exec vitest run harness/harness.test.ts src/pi-runtime-harness.test.ts` 14 passed；默认 `just agent-service-test` 218 passed / 2 skipped（已有 opt-in 用例）；`pnpm --dir agent-service build` 通过；`just agent-evals-coverage` 83 tasks、24/24 tools、12/12 ops；`just docs-check` 归档前通过，最终提交前复核。
- 部署检查：Dockerfile 已复制工件与 prebuild contract-check 所需输入。默认 Docker credential helper 缺失，临时独立 `--config /tmp/productflow-agent-p1-docker` 避开宿主配置问题；完整 build 在 pnpm 下载处 ETIMEDOUT，host network 重试仍失败。未修改宿主 Docker 配置，不宣称完整镜像 build 通过。
- 容器证据：`node:22.20.0-alpine` 使用 `--network none`，只读挂载编译 dist、harness 和依赖，不挂 Go prompt 源码，执行 `loadHarness()` 得到相同 hash、四段名称和 `frozen=true`。宿主编译产物加载也相同。该证据验证打包路径与 bundled 权限，不替代完整镜像 build。
- 活文档：ARCHITECTURE 中英文记录当前工件事实；ROADMAP 移除 P1 未实现描述，保留 P2–P7。
- 归档复核：2026-09-05 第二次修正后 `just agent-service-test` 同样为 218 passed / 2 skipped；`just docs-check` 与 `git diff --check` 通过。
- 审核：主代理-agent-0905-0458 自审；变更限定于壳工件、装配、必要打包与贴近 gate 的两行夹具修正；未改生产 Skill、题库/grader、Graph、lease/journal 或确认合同。P1 条款通过，G1/G2 均未完成。
- 交付定位：随本任务提交，通过本归档文件 Git 历史定位。剩余：P2 归因按阶段门另发；eval-skills 仍等待评测合同校正；完整镜像构建需可用 npm 网络。
