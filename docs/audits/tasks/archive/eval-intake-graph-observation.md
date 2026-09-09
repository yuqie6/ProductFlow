# 任务：创建场景中的跨工具操作由真实 Go 观察

状态：完成
类型：实现
认领者：account_backend
认领于：2026-09-08T08:58:59+08:00
业务组：Agent 质量
父账本：agent-eval-system.md
完成后可拆：按新提交冻结完整开发基线

遵循 [Issue 协议](../README.md)。

## 问题来源与边界

第五批开发 run `20260908T003528Z-e48b5db4` 的 `product-intake-negative-delete-images` 两次真实调用 propose_graph_change_set_v1 不可观察。326bf59a 已覆盖 context/node/finalize，但创建场景中跨工具调用仍落回不可测路径。详见 [开发基线](eval-development-baseline.md)。

本任务只修改五个 eval 文件：agent-service/evals/go-world.ts、go-world.test.ts，go/internal/agent/eval_user_sim_host_test.go、evalworld_test.go、eval_persisted_writes_test.go。题集、world JSON、评分规则、Skill 与生产 runtime 不变；Go 写后观察的目标身份修正属于本次修改，不能表述为评分代码完全未动。真实4xx/409/成功/未知各自保留，不人工编造拒绝，不启动新付费225批次。

## 已交付行为

root 对照 src/tools.ts 的实际工具工厂核对：商品场景的 context、node、run list/detail、product image list/inspect/content、finalize、apply/propose/discard、cancel/focus 均接 Go；商家跨商品场景的 product list/inspect、workflow context/runs/detail、media list/inspect/content、workspace、draft、prepare/execute run 均有对应 Go 调用。共享 load_skill 与 ask_user 由本地 harness 负责。接线覆盖不代表所有未来 world 自动有效。

两条 r5 原始提案参数各自在独立 Go world 回放：真实接受 pending proposal，live 节点/边/分组/revision不变，撤销后pending清空。同宿主互斥冲突另测409。原题只禁止finalize/create workspace，未禁止propose；不得按负例名称扩大禁止项。

工作区创建观察绑定实际创建商品，保留原种子商品身份；运行请求复用生产 payload，保留workflow/product/source step/task/source run与幂等key。取消及聚焦使用原用例与回读。运行种子显式读取当前商家下的live revision，读取失败不回退为1。

审阅发现 global-two-workflow-runs 只物化一个工作流，追加真实商品/active graph与run种子，按world声明ID维护可逆映射，failed/recent记录按声明复用。原题run-diagnosis-global-multiple-workflows回归检查两工作流及各自run detail的ID/status/revision；不使用实际只有一个工作流的run-from-global冒充此证据。

## 所有权与审核

eval_baseline完成原五文件实现、自审并交回；root接管后审阅全部diff，补真实revision回归并核对两条r5转录。account_backend只读复核后指出多工作流缺口，随后获独占三个Go eval文件修正；root持有两个Node文件及文档/Git并补跨语言回归。account_backend交付后停写；root最终审阅新增种子、映射及测试。原执行者数据库中断报告经root实际连接复查，数据库可用且其独立前缀库为空，无需重启共享栈。

## 验收证据

- root最终隔离PG `go test -C go ./internal/agent -run '^TestEval' -count=1 -p 1` 通过；日志 `.debug/eval-intake-graph-root-20260908/accepted-go-eval-final.log`。默认opt-in live gate不构成已执行。
- `PRODUCTFLOW_RUN_AGENT_EVALS_GOPG=1 pnpm -C agent-service exec vitest run evals/go-world.test.ts` 最终12/12通过；日志同目录 `accepted-go-world-final.log`。实际调用Go/PG，无模型调用。
- Agent离线全量315通过/17按条件跳过；日志 `node-suite-review.log`。Node tsc通过 `node-tsc-review.log`；生成合同/Node build/Go agent build日志保存在 `storage-dev/eval-intake-graph-observation-0908/evidence/`，相应执行代码未变时复用，Go后续种子修改由最终Go门验证。
- root逐名清理自身无连接测试库；执行者pf_eval_ig_review_0908前缀实际查询为空。最终五文件指纹及清理证据在 `.debug/eval-intake-graph-root-20260908/source.sha256`、`cleanup-final.txt`。共享开发栈、历史库与其他会话改动保留。
- r5停止后 `post-stop-fingerprint-rerun.json` 经root读取，all_match=true：固定commit/题集/world/Skill/harness/lock和133条原始记录未变。旧run没有summary/development导出，不续接、不改分母。
- `git diff --check` 与 `just docs-check` 通过。交付随本任务提交；通过归档文件Git历史定位。

完整开发包、Agent行为达标和图片生成质量均未由本任务证明。新批次必须独立冻结；D-02仍为部分完成。
