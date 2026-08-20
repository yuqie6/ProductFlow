# 素材库转型发布与验收计划

Last reviewed against the current working tree on 2026-08-19.

## 1. 范围

本计划把当前依附 ImageSession 的收藏画廊迁移为全局长期素材库，并先修复其依赖的 canonical media 与 durable async 缺陷。

范围包括：

- `ImageSessionAsset` 媒体 owner 修复和 schema constraint 收敛。
- `ImageSessionGenerationTask` / `WorkflowNodeRun` attempt fencing。
- database-to-Redis transactional delivery intent 和单一持续恢复 owner。
- `MediaLibraryAsset`、有界 provenance、归档/恢复。
- 从素材库显式收录到 `ProductImageAsset`。
- 一层文件夹、标签、分页、搜索和批量组织。
- 旧 `ImageGalleryEntry` 回填、API/UI cutover 和有界退休。
- 素材库 Agent 整理 Draft、明确 revision 确认和原子应用。

不包括：

- V1 source/archive 清理；其 owner 仍是 `docs/rollout/workflow-v2-cutover.md` 与 `docs/operations/legacy-v1-cutover.md`。
- 普通用户 hard delete、媒体 retention、嵌套文件夹、多租户。
- 把 `ProductImageAsset` 改成 nullable product scope，或让工作流直接引用全局素材。
- 回退或覆盖当前工作树中的 Agent workbench/tool-step 用户改动。

## 2. Authority 和验收状态

目标 authority：

- `MediaObject`：不可变媒体 bytes 和核验元数据。
- `MediaLibraryAsset`：全局素材身份、provenance、revision、组织和归档。
- `ProductImageAsset`：商品作用域图片身份与所有工作流引用。
- PostgreSQL：业务状态、delivery intent、attempt token 和整理 Draft。
- Redis/Dramatiq：at-least-once delivery，不是任务 authority。
- main Pi adapter session 文件：交互式 Turn 和 transcript；ProductFlow PostgreSQL event store：tool event 和 cursor；`exp` 保留 durable runtime 研究实现。

发布状态：

| 状态 | 条件 |
|---|---|
| `planned` | PRD、Design、ADR、实施计划和本验收方案通过文档门禁 |
| `bug_repair_ready` | canonical media audit、attempt race 和 queue crash-window 回归存在 |
| `bug_repaired` | Phase 1-3 自动化和 live PostgreSQL/Redis 证据通过 |
| `library_core_ready` | 新 schema、回填、archive/restore 和 product collection 通过 |
| `cutover_ready` | 旧 Gallery 与新 Library 对账为零 blocker，frontend 候选通过 |
| `serving_library` | 真实浏览器和部署 smoke 通过，只有新 owner 在线读写 |
| `cleanup_eligible` | 观察窗结束，旧表零在线引用，备份恢复已验证，并取得单独 destructive confirmation |

代码合并、Alembic 到达 head 或 unit tests 通过，均不能单独提升发布状态。

## 3. 工作区和发布隔离

当前工作树存在用户维护的 Agent workbench/tool-step 变更。实现和提交必须遵守：

- 不回退、改写或 broad-stage 用户文件。
- 需要触碰同一 Agent 文件时，先读取最新 diff 并在现状上增量衔接。
- backend media/library、migration 和 web Gallery 改动按独立 slice 验证。
- 每个 migration slice 记录开始 revision、结束 revision 和涉及表。
- 生产发布候选必须来自可重现 commit；不得直接发布混合脏工作树。

## 4. Phase 0：只读预检

在任何数据 mutation 前输出版本化 JSON report，至少包含：

- `image_session_assets` 总数、null `media_object_id` 数、缺失 FK 数。
- session `storage_path` / `mime_type` 与 `MediaObject` mismatch 数和稳定 ID 列表。
- verified/missing/legacy_pending 媒体计数与实际文件缺失数。
- `image_gallery_entries` 总数、孤儿 source/round、重复 source、缺失 media 数。
- 可回填素材数、blocker 数、按 source id 稳定排序的 provenance preview 和 hash。
- queued/running workflow、image-session、Agent-sync、rendition 任务数。
- stale running 与 active attempt 分布。
- 当前 Alembic revision、release commit 和 storage root 指纹。

报告不得包含 storage path、完整 prompt/provider payload、密钥或媒体 bytes。重复执行同一快照必须产生相同 canonical hash；`generated_at` 不参与 hash。

停止条件：

- 任一 ImageSessionAsset 无法确定 MediaObject。
- path/MIME mismatch 无法通过实际 bytes 和核验记录判定。
- Gallery source 缺失且无法构造可信 provenance。
- 发现未知 persisted enum/status/source kind。
- 数据库快照和 storage 快照不是同一时间点。
- 有任务处于无法解释的 `unknown` 或旧 worker 仍可能提交结果。

## 5. Phase 1：canonical media bug repair

交付条件：

1. 所有 ImageSession provider-input、delete、serialize 和 download 路径从 `MediaObject` 读取 path/MIME。
2. 新 head migration 在 backfill assertion 后把 `image_session_assets.media_object_id` 设为 `NOT NULL`。
3. fresh SQLite 和 live PostgreSQL inspector 都证明 ORM/schema 一致。
4. 重复 session path/MIME 字段暂时保留为 compatibility carrier，但新代码不读它们。
5. mismatch audit 为零并完成一个部署观察窗后，才可提出独立 drop migration。
6. session 删除、product attach、library save 的共享 media 矩阵全部通过。

停止条件：任何线上读者仍依赖 session path/MIME；migration 需要 provider/Redis；null/mismatch 被静默修复而没有报告。

## 6. Phase 2：attempt fencing

分别扩展 rendition 已有的 `active_attempt_id` 模式：

- `ImageSessionGenerationTask` claim 设置新 attempt id；progress、candidate asset/round、计数、success、failure、cancel 都校验 active attempt。
- `WorkflowNodeRun` claim 设置新 attempt id；prompt artifact、generation record、ProductImageAsset、binding、cover、rendition enqueue、success/failure 都由 active attempt 保护。
- stale recovery 使旧 attempt 失效；旧 attempt 的数据库写入必须 no-op/conflict。
- losing attempt 只能补偿自己本次新写的 storage 文件，不能删除共享或 winner 文件。
- provider 支持幂等时使用稳定 logical operation key；不支持时明确保留“可能产生外部重复费用，但旧结果不能提交”的合同。

停止条件：只保护最终 status、但 progress/asset/history 可由旧 attempt 写入；stale reset 仍允许两个 attempt 都提交。

## 7. Phase 3：transactional delivery

- 新增由 PostgreSQL 持久化的 delivery intent/outbox owner。
- 业务任务和对应 delivery intent 在同一事务创建或转换。
- dispatcher 通过 lease/`FOR UPDATE SKIP LOCKED` 领取 pending intent，先记录 sent 再 enqueue；crash 可由 stale reconcile 补发并允许重复 delivery。
- worker 先以 consumer lease 原子领取 sent intent，再执行目标并确认 consumed；目标异常会清除 consumer lease，按 attempts 有界回到 pending 或 dead；进程崩溃则由 lease expiry 重新投递。没有有效 consumer lease 的 sent intent 才可重新投递。
- API startup 不再扫描和 enqueue；dispatcher/reconciler 是持续恢复 owner。
- workflow scheduler wake-up、capacity retry、ImageSession、Agent sync 和 rendition 分阶段迁移。
- 全部 writer 迁移前保留现有 recovery 作为 compatibility；最终 residue scan 为零后一起删除直接 enqueue/recovery 分支。

停止条件：只移动 startup recovery owner、但没有 delivery lease/marker；broker 失败仍只能等下次进程启动；outbox 与业务状态可分开提交。

## 8. Phase 4：素材库 core 和回填

新 schema 最少包含：

- `MediaLibraryAsset`：media FK、source kind/FK、display/original name、provenance version/JSON/hash、revision、archived_at、timestamps。
- source ImageSession FK nullable `SET NULL`；对非 null source 建幂等唯一约束。
- `media_object_id` 非唯一。
- provenance 使用 strict schema v1，限长并排除秘密字段。

回填按 maintenance freeze -> expand -> explicit backfill -> reconcile -> cutover 执行：

1. 在 preflight 前进入维护窗口，停止旧 backend、worker 和 Agent mutation ingress；旧 Gallery 写入与来源 ImageSession 删除必须不可达。冻结持续到最终零 delta 事务和新 owner 部署完成。
2. 生产迁移必须记录 PostgreSQL WAL LSN 锚点（单调、可比对）、Gallery/Session high-watermark、row counts 和稳定 source hash；每个 report 都携带这些字段。当前命令的 `--snapshot-file` 已冻结 source row mapping/hash、PostgreSQL WAL LSN 锚点、storage snapshot identity（`--verify`/reconcile 会重算比对）和完整 durable blocker report；要作为 cutover evidence 仍必须叠加维护窗口、真实备份恢复验证和观察窗证据。
3. Alembic revision 只创建素材库 schema，不读取 storage，不调用 provider/Redis，也不假定文件系统已挂载。
4. 显式运行可重入 preflight command，读取 database + storage，按 source id 分页输出可回填数、blocker、文件可读性和 stable report hash；此步骤默认 dry-run。
5. 对已通过 preflight 的 frozen snapshot 运行 bounded `--apply`：每个合法 `ImageGalleryEntry` 创建一个素材，尽量复用 entry id；media id 来自 source `ImageSessionAsset.media_object_id`。
6. source FK、round FK 只作为 lineage；有界 snapshot 独立保存。database facts 无法构造可信 provenance 时停止，不填充猜测值。
7. 相同 MediaObject 的不同 Gallery source 不自动合并。
8. apply 后运行 post-reconciliation，核对 old/new count、id、source id、media id、provenance hash 和文件可读性；报告 hash 必须绑定同一 frozen snapshot。
9. 最终事务锁定 Gallery/Session source tables 进行稳定读，重算 high-watermark/count/hash，证明 source delta 为零，并记录 cutover-ready evidence。
10. dry-run/apply/reconcile 都必须幂等；异常条目进入 blocker report，不静默跳过。

当前 `backfill_media_library` command 已将坏文件和无效 source 以 entry id/code 写入 `summary.blockers`，存在 blocker 时返回退出码 `2`；`--snapshot-file` 会同时保存 PostgreSQL WAL LSN 锚点、storage snapshot identity（`--verify` 重算比对）和完整 durable blocker report；`--verify` 还会输出包含 workflow 子图库关联和商品收录引用计数的 `reconciliation_report_sha256`。reconcile 也会拒绝 canonical 侧多出的 `legacy_gallery` mapping。这些快照字段仍不能替代真实维护窗口、备份恢复验证和观察窗证据。

### 4.1 旧 Canvas revision 的 Gallery 兼容桥接

若 source schema profile 为 `legacy_canvas_agent_20260518_0032`，该数据库没有当前素材库回填所需的 `media_objects`、`image_session_assets.media_object_id` 和 `media_library_assets` 目标结构。`backfill_media_library` 仍面向 current schema，遇到该 source 会返回退出码 `2`。旧库的 Gallery 迁移使用独立的 Gallery-only bridge，保留旧库的 Agent 表、线程、run、计划和时间线，不改变它们的迁移归属。

bridge 的边界固定为：

1. source 只允许 revision `20260518_0032`，只读读取 `image_gallery_entries`、`image_session_assets` 和对应文件。未知 revision、缺少表/列、非法路径、缺失文件、非图片文件、声明 MIME 与内容不一致都会进入 blocker。
2. `export_legacy_gallery_bridge` 生成版本化 manifest。manifest 同时记录 source report hash、包含文件 hash 的 snapshot token、Gallery entry/source asset 身份、文件元数据和 blocker；导出连接强制只读。
3. `backfill_legacy_gallery_bridge` 在 current schema 中重新核验 source 文件，把文件复制到新的 `media/<uuid>/` 命名空间，创建已验证的 `MediaObject` 与 `source_type=legacy_gallery` 的 `MediaLibraryAsset`。目标 id 按 source entry 稳定生成，重复执行只接受一致映射。
4. `--verify` 会重读 source 文件和目标文件，核对 byte hash、尺寸、MIME、provenance 和 target mapping，并输出 `target_reconciliation_sha256`。任何 blocker 或 mapping drift 都不能生成物理退休批准证据。
5. `approve_legacy_gallery_bridge` 把 source manifest、target reconciliation、备份恢复验证时间和 zero-delta 观察时间绑定成独立 approval。`retire_legacy_gallery_source` 在旧库事务中重新锁表、重算 manifest hash，再删除旧 `image_gallery_entries`；目标素材、共享媒体和工作流引用不受删除影响。

这一条 bridge 不迁移旧 Canvas Agent archive，也不解除 `audit_legacy_retirement` 对 Agent source 的 `migration_bridge_required` 判断。Agent archive 的去留、映射和新 Agent 决策继续独立评审。Gallery bridge 完成后，旧库仍可保留其它旧表；这里只退休 Gallery 物理 owner。

## 9. Phase 5：商品收录、组织与归档

- `ProductImageAsset.source_library_asset_id` 只对非素材库收录资产为 null；收录后使用 `ON DELETE RESTRICT` 且不可改绑。
- `(product_id, source_library_asset_id)` 唯一。
- `origin_type` 从素材 provenance 映射到既有 upload/workflow/ImageSession 来源；收录 lineage 只由 `source_library_asset_id` 表达。
- 收录事务锁定 Product 与 LibraryAsset，拒绝 archived/missing/pending，复制 display/original name 并复用 exact media id。
- 单次收录最多 100 个唯一素材，请求最大 256 KiB，锁按 asset id 稳定排序；并发重复收录返回同一个 `ProductImageAsset`。`POST /api/media-library/collect` 要求 product-scoped `Idempotency-Key`，并把 canonical 请求 hash 持久化到 `media_library_collection_keys`；同 key 不同参数返回冲突，重试返回原收录结果。
- Library rename/tag/folder/archive 不传播到已有商品资产。
- `MediaLibraryFolder` 一层；删除时素材回到 unorganized。
- `MediaLibraryTag.normalized_key` 全局唯一；关联表防重复。
- archive 可恢复；默认列表、选择器和新增收录排除 archived。
- `_media_has_references` 纳入 library owner；本阶段不提供 hard delete。

## 10. Phase 6：API、前端与旧 Gallery cutover

新 API 使用独立 `/api/media-library` owner，包含：

- bounded bootstrap/list/detail。
- save-from-session / save-from-product。
- archive / restore / rename / move / tag mutations，均带 expected revision。
- collect-to-product，带 idempotency key + request hash。
- canonical original/preview/thumbnail content。

前端：

- `/gallery` 路由保留用户入口，但页面改为素材管理工作面，不保留 marketing hero。
- TanStack Query identity 包含 cursor/filter/search/sort/archive state。
- 提供 grid/list、搜索、来源、文件夹、标签、归档、详情抽屉、批量选择和显式商品收录。
- ImageChat 的“保存画廊”改为“保存到素材库”。
- 商品 explorer 提供显式“从素材库收录”入口；工作流引用仍提交 ProductImageAsset id。
- 所有 wire JSON 使用 runtime parser；不增加 unchecked cast。

cutover 条件：

- 新 frontend 和 API 已不读取 `/api/gallery`。
- current-schema backfill 或 Gallery bridge report 为零 blocker，并且 verify 已生成稳定 target reconciliation hash。
- `ImageGalleryEntry` runtime model、旧 route、schema、api methods 和旧客户端调用已退休；迁移测试保留 source reader、backfill 和 retired-route 404 回归。
- 旧物理表继续只读保留，直到独立素材库 cutover gate 进入 `ready_for_cleanup`。

## 11. Phase 7：Agent 整理 Draft

- `AgentConversation` 或后继通用 binding 明确支持 `media_library` scope，Product/WorkflowDraft 字段不得以伪对象填充。
- `LibraryOrganizationDraftRevision` v1 operation union 只有 rename、move、set-tags、archive 和 restore；每项包含 asset id、expected revision、target 和 reason，商品收录保持显式用户 command。
- 一个 revision 最多 100 个唯一素材、256 个 operation、256 KiB canonical JSON；重复/冲突 operation 在合同边界拒绝。
- Agent contract 只允许 bounded list、selected inspect 和 publish-draft；没有直接 archive/move/tag/hard-delete 工具。
- 用户确认明确 revision 后，单一事务按 asset id 稳定锁定全部 target、验证 hash/idempotency/expected revision 并原子应用。
- TypeScript/Go experimental adapters 对新增 contract 使用 boundary-owned schema/fixtures；tool-step projection 只暴露有界摘要和业务引用。

停止条件：Agent 可绕过 Draft 直接修改；素材库 scope 复用假的 Product；一次向模型发送全库元数据或 bytes。

## 12. 自动化验收矩阵

| ID | 范围 | 必须证明 |
|---|---|---|
| A01 | migration fresh SQLite | head 可升级；media FK 非空；FK/index/check 正确 |
| A02 | migration/live backfill PostgreSQL | 真实 schema upgrade 不依赖 storage；显式 preflight/apply/reconcile、constraint 和 rollback-stop 行为正确 |
| A03 | media owner | 所有 session path/MIME reader 已退休；共享 media 删除矩阵正确 |
| A04 | image attempt | stale worker 的 progress/result/failure/file cleanup 全被拒绝 |
| A05 | workflow attempt | stale node 不得写 artifact/asset/binding/history/status |
| A06 | outbox | commit-before-send、crash-before-send、crash-after-send、Redis outage、重复 delivery 均收敛 |
| A07 | library backfill | count/id/source/media/provenance hash 对账和幂等通过 |
| A08 | library lifecycle | save、archive、restore、session delete、product delete、media pruning 通过；save-from-product 不绕过 `require_deletion_enabled`，授权删除后 library/media 可读且 provenance 不变 |
| A09 | product collection | 并发幂等、RESTRICT lineage、origin mapping、100-asset/256-KiB 上限、media coherence、archived rejection、scope isolation 通过 |
| A10 | organization | folder/tag/rename expected-revision、100-asset/256-KiB 上限和 count/filter 一致 |
| A11 | API | cursor/filter hash、bounded payload、runtime schema 和错误合同通过 |
| A12 | frontend | query identity、分页、选择、archive、收录、错误恢复通过 |
| A13 | Agent Draft | publish 无副作用、operation scope、100 assets/256 operations/256 KiB 上限、明确 revision 确认、冲突整批拒绝、replay 通过 |
| A14 | residue | 旧 Gallery route/DTO/client/model 无在线引用；旧 table 仅迁移证据 |
| A15 | full gates | Ruff、backend full、Pi service tests/build、web tests/lint/build、docs-check 全通过 |

标准命令：

```bash
uv run --directory backend ruff check .
uv run --directory backend pytest
just agent-service-test
pnpm --dir web test:run
pnpm --dir web lint
pnpm --dir web build
just docs-check
git diff --check
```

涉及 PostgreSQL advisory lock、SKIP LOCKED、migration、Redis 和 storage race 的 case 必须运行 justfile 中对应 live gate；SQLite unit test 不能替代。

## 13. Live API 和故障注入验收

至少验证：

1. 从生产恢复副本执行所有新 migration，preflight/backfill 重跑 hash 稳定。
2. Redis 在 DB commit 后不可用，intent 保持 pending，恢复后自动投递。
3. dispatcher 发送后 crash、ack 前 crash和重复消息不会产生重复业务结果。
4. 阻塞 provider，触发 stale reset，再释放旧/新 worker；只有当前 attempt 可提交。
5. ImageSession 删除后，保存素材仍可预览/下载，provenance snapshot 不变。
6. 同一素材并发收录到同一商品只产生一个 ProductImageAsset，收录到不同商品各自独立。
7. 归档素材阻止新增收录；已有商品工作流继续读取其 ProductImageAsset。
8. old/new Gallery 对账通过后，调用旧 API 得到明确 retired response 或路由不存在，不发生 fallback。
9. Agent Draft replay、断线、重复确认和 expected revision 冲突均收敛到 PostgreSQL 事实。

## 14. 浏览器验收

视口：1440x900、1024x768、390x844；中文/英文；light/dark；reduced motion。

必须覆盖：

- 首屏是实际素材管理界面；无 hero 占据主要工作区域。
- 文件夹、标签、搜索、来源、归档、grid/list 和详情抽屉。
- cursor load-more 前后无重复/遗漏，切换 identity 清理 stale selection/page。
- ImageChat 保存到素材库、重复保存、来源会话删除后的素材读取。
- 素材批量收录到商品、重复收录、跨商品收录和归档拒绝。
- 商品 explorer 只展示 ProductImageAsset，工作流绑定提交明确商品 asset id。
- Agent 整理 Draft 审阅、确认、冲突和无投影降级。
- 无 overlap、横向溢出、不可见 focus、hover-only critical action 或按钮尺寸跳动。
- console error、failed request 和 HTTP 5xx 为零；图片 original/preview/thumbnail 非空且 MIME 正确。
- 记录 `window.innerWidth`、关键容器 `clientWidth`、bounding boxes 和截图。

## 15. 回滚

- schema expand、代码切换和 destructive cleanup 分离。
- cutover 前失败：停止新写入，修复并重跑；尚未上线的新表可保留用于诊断。
- cutover 后但未 cleanup：回退应用到读取旧 Gallery 需要经过明确评审；优先向前修复。新 Library 写入出现后，不允许依靠 Alembic downgrade 删除新表。
- 无法证明数据一致时，恢复同一时间点的 database 与 storage 备份。Redis/Dramatiq 不作为权威备份恢复；清空或隔离旧 broker state 后，由 PostgreSQL delivery intent、attempt token 和 reconciler 重建待投递消息。
- 每次恢复后重跑 media、Gallery/Library 和 task delivery 对账；旧 hash/approval 作废。

## 16. Destructive cleanup guard

本计划不授权通过 Alembic、应用启动或普通用户 API drop `image_gallery_entries`、删除 duplicate session path/MIME 列、物理删除 archive/library media 或修改 V1 source/archive 表。代码已经提供独立的素材库证据闸门和一次性退休命令，但命令默认只做 dry-run，部署证据不齐时会拒绝执行。

未来清理必须满足：

1. current schema 的独立 `media_library_cutover_gates`，或旧 Canvas source bridge 的独立 approval artifact；两者都需要实施计划、人工评审和明确 scoped confirmation。
2. 至少一个已验证部署观察窗内旧 reader/writer/count 为零。
3. 数据库和 storage backup restore 有真实证据。
4. dry-run 输出精确 rows/columns/files 和稳定 hash。
5. 清理事务开始时重新验证 cutover evidence；任何 hash 漂移立即停止。
6. database commit 后才执行精确 storage cleanup；失败持久化为可重试清单。
7. residue scan 覆盖代码、测试、配置、docs、migration readers 和浏览器 client。

违反任一条件时，不允许用 `alembic stamp`、手工 SQL、force flag 或 fallback reader 绕过。

退休前先在受限证据目录保存回填快照和报告，然后批准闸门：

```bash
python -m productflow_backend.commands.backfill_media_library \
  --snapshot-file "$EVIDENCE_DIR/gallery-snapshot.json" \
  --apply --verify > "$EVIDENCE_DIR/gallery-backfill.json"
python -m productflow_backend.commands.manage_media_library_cutover_gate approve \
  --source-snapshot-token '<snapshot JSON 的 database_snapshot_token 字段（PostgreSQL 为 WAL LSN 锚点）>' \
  --source-report-sha256 '<source hash>' \
  --reconciliation-report-sha256 '<reconciliation hash>' \
  --backup-restore-verified-at '<UTC ISO-8601 timestamp>' \
  --zero-delta-observed-at '<UTC ISO-8601 timestamp>'
```

批准后仍需在同一候选版本执行 dry-run；只有重新锁定 source table、source hash 和 reconciliation hash 全部一致时，才允许带确认字符串执行：

```bash
python -m productflow_backend.commands.retire_legacy_gallery
python -m productflow_backend.commands.retire_legacy_gallery \
  --apply --confirm RETIRE_LEGACY_GALLERY
```

旧 Canvas source 使用下面的独立 bridge 流程；`--apply` 之前必须保存 manifest、bridge report、target reconciliation 和 approval：

```bash
python -m productflow_backend.commands.export_legacy_gallery_bridge \
  --database-url '<legacy database url>' \
  --source-storage-root '<legacy storage root>' \
  --output "$EVIDENCE_DIR/legacy-gallery-manifest.json"
python -m productflow_backend.commands.backfill_legacy_gallery_bridge \
  --input "$EVIDENCE_DIR/legacy-gallery-manifest.json" \
  --source-storage-root '<legacy storage root>' \
  --expected-source-report-sha256 '<manifest source_report_sha256>' \
  --apply --verify \
  --output "$EVIDENCE_DIR/legacy-gallery-bridge.json"
python -m productflow_backend.commands.approve_legacy_gallery_bridge \
  --manifest "$EVIDENCE_DIR/legacy-gallery-manifest.json" \
  --target-reconciliation-sha256 '<bridge report target_reconciliation_sha256>' \
  --backup-restore-verified-at '<UTC ISO-8601 timestamp>' \
  --zero-delta-observed-at '<UTC ISO-8601 timestamp>' \
  --output "$EVIDENCE_DIR/legacy-gallery-approval.json"
python -m productflow_backend.commands.retire_legacy_gallery_source \
  --database-url '<legacy database url>' \
  --source-storage-root '<legacy storage root>' \
  --manifest "$EVIDENCE_DIR/legacy-gallery-manifest.json" \
  --approval "$EVIDENCE_DIR/legacy-gallery-approval.json"
python -m productflow_backend.commands.retire_legacy_gallery_source \
  --database-url '<legacy database url>' \
  --source-storage-root '<legacy storage root>' \
  --manifest "$EVIDENCE_DIR/legacy-gallery-manifest.json" \
  --approval "$EVIDENCE_DIR/legacy-gallery-approval.json" \
  --apply --confirm RETIRE_LEGACY_GALLERY
```

current-schema `retire_legacy_gallery` 只删除 `image_gallery_entries`，保留 `MediaLibraryAsset`、`MediaObject`、工作流子图库关联、商品图片和历史引用；数据库 drop 与 gate 标记在同一事务内完成。旧 Canvas source 的 `retire_legacy_gallery_source` 只删除 source Gallery 表，不删除 Agent archive 或其它旧表。命令执行成功后相应 owner 进入 retired/cleaned，不能重新批准同一份 evidence。
