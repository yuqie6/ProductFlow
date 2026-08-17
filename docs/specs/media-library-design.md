# 素材库转型设计

## 1. 状态与边界

- 状态：Approved for implementation
- 批准依据：Accepted `docs/adr/0006-media-library-authority.md`；该 ADR 记录仓库 owner 在 2026-08-16 的直接决策。
- Product baseline：`docs/specs/media-library-prd.md`
- Architecture decision：`docs/adr/0006-media-library-authority.md`
- Rollout/acceptance：`docs/rollout/media-library-transition.md`
- 当前运行事实：`docs/ARCHITECTURE.md`、代码、迁移和测试

本设计分成可独立验证的阶段。canonical media repair、attempt fencing、durable delivery 和旧收藏条目删除一致性是基础修复；全局图库、工作流子图库同步、组织、Gallery cutover 和 Agent Draft 是目标能力。旧表/列物理删除不在本实施授权内。全局图库是目标态的主图片身份，工作流子图库是关联和使用层；现有商品图片身份只能作为工作流侧兼容引用，不能继续作为并行全局 owner。

## 2. 当前实现与缺陷

### 2.1 媒体 owner 漂移

`MediaObject` 在 `infrastructure/db/models.py` 中拥有 path/MIME/verification metadata；`ProductImageAsset` 已通过关系读取它。`ImageSessionAsset` 同时保存 `storage_path`、`mime_type` 和 required ORM `media_object_id`。

当前在线读者包括：

- provider references：`application/image_sessions.py` 的 `_build_generation_references`。
- reference delete：`delete_image_session_reference_image`。
- HTTP content：`presentation/routes/image_sessions.py`。
- session serialization：`presentation/schemas/image_sessions.py`。

migration `20260811_0031` 只把 `media_object_id` 添加为 nullable 并回填，没有 alter to NOT NULL。当前 fresh-head regression 记录了该 schema 状态，而 ORM metadata 声明 non-null；Phase 1 新增 repair revision 后必须同步更新 head expectation。

### 2.2 旧 Gallery 依附 Session

`ImageGalleryEntry` 通过 `ON DELETE CASCADE` 引用 `ImageSessionAsset`，并通过 nullable round FK 动态读取 prompt/provider/candidate metadata。`application/gallery.py` 无界返回全部条目；`GalleryPage.tsx` 一次读取并在展示型页面中渲染。

该模型没有独立名称、revision、archive、canonical content URL 或来源快照。旧收藏的目标生命周期跟随 ImageSession，但应用删除路径必须显式删除旧条目，不能依赖 SQLite 默认关闭的外键级联。已保存到全局图库的资产不受旧条目删除影响。

### 2.3 异步 crash window 和 stale attempt

业务 task 先 commit PostgreSQL，再 enqueue Redis；caught enqueue error 可以标记失败，但 commit 后进程 crash、enqueue 前 crash 只能等待 startup scan。API 和 Dramatiq CLI 都执行 recovery，且没有 delivery lease。

Rendition 已有 `active_attempt_id` 和 compare-and-set success/failure。ImageSession task 与 WorkflowNodeRun 只有 queued->running status claim；stale reset 后旧 worker 仍可能写 progress、artifact、asset、history 或最终状态。

### 2.4 跨语言合同

FastAPI/Pydantic 拥有 ProductFlow HTTP schema；Go 手写 internal API DTO、tool names/limits；Go Agent/harness 拥有 SSE event。当前工作树正在增加 `tool.step`，前端消费仍需与事件 owner 同步。

生成合同只用于真实 wire 边界。SQLAlchemy models、Python application dataclasses 和 provider 内部 DTO 不进入统一生成计划。

## 3. 目标组件图

```text
ImageSession / workflow result / user-selected image
           │ explicit save
           ▼
Global Media Library
  ├─ MediaLibraryAsset + provenance
  ├─ folders / tags / archive
  ├─ workflow sub-library sync/associations
  └─ organization Draft materialization
           │ shared logical asset
           ├──────────────► Workflow sub-library A
           ├──────────────► Workflow sub-library B
           └──────────────► Workflow sub-library C
                                  │ stable workflow reference
                                  ▼
                         node / cover / lineage / rendition

MediaObject ───── immutable bytes shared by the global asset and references

PostgreSQL business task + AsyncDispatch (same transaction)
           │ leased dispatcher
           ▼
Redis/Dramatiq at-least-once message
           │ atomic claim + active_attempt_id
           ▼
provider/storage effect + fenced persistence
```

浏览器只通过 FastAPI public API；Go Agent 只通过 internal Agent contract。没有 frontend/Go 对数据库或 storage path 的直接依赖。

## 4. 数据模型

### 4.1 `MediaLibraryAsset`

建议字段：

| 字段 | 合同 |
|---|---|
| `id` | 36-char opaque id；legacy Gallery backfill 尽量复用 entry id |
| `media_object_id` | required FK `media_objects.id ON DELETE RESTRICT`；不唯一 |
| `source_kind` | v1 只包含 `image_session_generated`、`product_asset`、`legacy_gallery`；direct upload 等真实上传流程获批后再扩展 |
| `source_image_session_asset_id` | nullable FK `ON DELETE SET NULL`；非 null 时唯一 |
| `source_product_image_asset_id` | nullable FK `ON DELETE SET NULL`；非 null 时可用于来源幂等 |
| `display_name` | required，trim 后 1..255 |
| `original_filename` | required，保存来源时的用户可读文件名 |
| `provenance_schema_version` | 当前固定 `1` |
| `provenance_json` | strict bounded snapshot |
| `provenance_hash` | canonical JSON SHA-256，length 64 |
| `revision` | positive integer；每次名称/组织/archive mutation +1 |
| `folder_id` | nullable FK `ON DELETE SET NULL`，组织阶段加入 |
| `archived_at` | nullable timestamp |
| timestamps | created/updated |

约束：

- 不使用 `UNIQUE(media_object_id)`。
- source id 的唯一约束只约束相同业务来源的重复保存。
- source kind 与 source FK 的组合使用 check/application validation；legacy snapshot 可没有存活 FK。
- list order 始终包含 `id` tie-breaker。

### 4.2 provenance v1

Python owner 位于 `application/media_library/contracts.py`，使用 Pydantic `extra="forbid"`，canonical dump 最大 32 KiB。建议 payload：

```json
{
  "schema_version": 1,
  "source_kind": "image_session_generated",
  "captured_at": "2026-08-16T00:00:00Z",
  "source": {
    "image_session_id": "...",
    "image_session_title": "...",
    "image_session_asset_id": "...",
    "image_session_round_id": "..."
  },
  "generation": {
    "prompt": "bounded user prompt or null",
    "requested_size": "1024x1024",
    "actual_size": "1024x1024",
    "provider_name": "...",
    "model_name": "...",
    "prompt_version": "...",
    "generation_group_id": "...",
    "candidate_index": 1,
    "candidate_count": 2
  }
}
```

禁止字段：provider request/output body、storage path、API key/token、cookies、base64/data URL、图片 bytes、raw Agent tool input/result。Prompt 最大长度使用明确常量；未知字段保持 null 或省略，不推断。

### 4.3 工作流子图库关联

全局图库与工作流子图库之间必须有明确的关联身份。关联应满足：

- 一个 `MediaLibraryAsset` 可以关联多个工作流。
- 一个工作流可以使用多个全局图库资产。
- 关联不复制 `MediaObject` bytes。
- 移除单个工作流关联不删除全局资产。
- 工作流节点、封面、参考绑定和交付 lineage 继续使用工作流侧稳定引用，并可追溯到全局图库资产。
- 现有 `ProductImageAsset`/`source_library_asset_id` 可以作为迁移和运行时兼容基础，但不能把每个商品的图片集合继续当成独立的全局图库。

同一事务内：

1. 按 deterministic 顺序锁全局资产和工作流作用域。
2. 校验全局资产 active、MediaObject verified、来源仍然 coherent。
3. 查询既有关联；存在则幂等返回。
4. 创建或更新工作流子图库引用，写入 exact global asset/media identity。
5. commit 后通过 canonical workflow query 返回。

普通 FK 不能证明工作流侧引用和全局资产指向同一个 `MediaObject`；use case、migration audit 和并发 test 必须断言该不变量。关联创建后是否允许改绑必须由工作流边界明确控制，不能通过任意 ID 替换绕过 lineage。

### 4.4 folder/tag

- `MediaLibraryFolder(id, name, normalized_name, sort_order, timestamps)`；`normalized_name` 唯一，folder 一层。
- `MediaLibraryTag(id, name, normalized_key, timestamps)`；`normalized_key` 唯一。
- `MediaLibraryAssetTag(asset_id, tag_id)` composite unique/cascade assignment。
- Folder delete 使用 `SET NULL` 或 stage update 后 delete；不删除素材。
- Tag delete 只 cascade assignment。

名称规范化必须是 portable application function，不依赖 PostgreSQL-only CITEXT；SQLite/PostgreSQL tests 使用相同 vectors。

### 4.5 organization Draft

- `LibraryOrganizationDraft`：status、current_revision_id、Agent scope binding。
- `LibraryOrganizationDraftRevision`：version、payload_json、payload_hash、created_at。
- v1 payload operation union：rename、move、set_tags、archive、restore。工作流子图库关联仍由明确的工作流 command/同步规则负责，不进入 Agent Draft。
- 每项带 asset id、expected revision、before summary、target 和 reason。
- 一个 revision 最多 100 个唯一 asset、256 个 operation、256 KiB canonical JSON；重复或冲突 operation 被拒绝。
- confirm table/字段记录 confirmed revision、idempotency key 和 request hash；materialization result 可重放。

## 5. Application ownership

建议新模块：

```text
application/media_library/
  contracts.py       provenance、list filter、Draft payload
  queries.py         bounded bootstrap/list/detail
  service.py         save/archive/restore/rename
  organization.py    folder/tag mutations
  workflow_sync.py   global asset to workflow sub-library associations
  product_collection.py  transition adapter for existing ProductImageAsset lineage
  drafts.py          append/confirm/materialize
```

规则：

- public command 拥有一次 commit/rollback。
- `stage_*` 只 add/flush，不隐藏 commit。
- storage write 先于 DB 时只补偿本 attempt 新建文件。
- list 不加载 original bytes 或完整 provenance；detail 才返回 bounded snapshot。
- child lookup 按 Library/Product/Conversation scope。
- batch mutation 最多接收 100 个唯一素材，request JSON 最大 256 KiB；重复 asset id 被拒绝。
- batch lock 以 `(entity type, id)` 稳定排序。
- expected revision mismatch 使用 `ConflictError`，不静默 last-write-wins。

现有 `application/gallery.py` 只在迁移窗口服务旧 owner，cutover 后整个模块退休。工作流/商品侧 explorer 继续提供子图库体验，但其图片关系必须由全局图库同步/关联合同驱动，不得与全局 queries 形成第二个全局 owner。

## 6. Public API

建议 prefix：`/api/media-library`。

```text
GET    /bootstrap
GET    /assets
GET    /assets/{asset_id}
GET    /assets/{asset_id}/content?variant=original|preview|thumbnail
POST   /assets/from-image-session
POST   /assets/from-product
POST   /assets/{asset_id}/archive
POST   /assets/{asset_id}/restore
POST   /assets/{asset_id}/rename
POST   /folders
POST   /folders/{folder_id}/rename
DELETE /folders/{folder_id}
POST   /assets/move
POST   /tags
POST   /assets/{asset_id}/tags
GET    /workflows/{workflow_id}/assets
POST   /workflows/{workflow_id}/assets/sync
DELETE /workflows/{workflow_id}/assets/{asset_id}
POST   /assets/collect-to-product  # transition adapter only
```

Mutation contract：

- create/save/collect 使用 `Idempotency-Key` + canonical request hash。
- rename/move/tag/archive/restore 使用 `expected_revision`。
- move/tag/archive/restore/sync 每次最多 100 个唯一素材；request body 最大 256 KiB，在 HTTP parse 和 application command 两层验证。
- list query：`after`、`limit<=100`、`query`、`source_kind`、`folder_id`、`tag_ids`、`archive_state`、`sort`。
- cursor 包含 version、sort、filter hash、sort key、asset id 和需要时的 as-of snapshot。
- archived asset content 默认仍允许历史读取；新增 collect 明确拒绝 archived。
- missing/pending media 不返回 bytes；错误合同与 ProductImageAsset content 对齐。

旧 `/api/gallery` 不作为 fallback。新 frontend 切换后删除 route/schema/client/types；旧表只保留迁移证据。

## 7. ImageSession save

`save_image_session_asset_to_library`：

1. 锁定 scoped ImageSessionAsset，并 eager load MediaObject/round/session。
2. 只允许当前产品合同认可的 generated result；未来 reference save 单独扩展 source kind。
3. 校验 MediaObject verified 和实际来源 round。
4. 以 source asset id 查询已有 library asset。
5. 构造 provenance v1、hash、display/original name。
6. 创建 LibraryAsset；unique race 后 rollback，再查询既有结果。
7. 不复制 storage bytes。

ImageSession 删除：

- library source FK `SET NULL`。
- library MediaObject FK 阻止 prune。
- provenance snapshot 保留。
- `_media_has_references` 包含 LibraryAsset。

保存工作流侧图片到全局图库复用相同的 media 和 source-idempotency 模式。保存动作完成后按同步规则建立全局资产与工作流子图库的关联。该流程不调用、放宽或暗示通过工作流/商品图片删除门禁；删除某个工作流侧引用只解除关联，LibraryAsset、MediaObject 和 immutable provenance 保持可读。现有 `save_product_image_asset_to_library`/商品收录逻辑只能作为过渡适配器，不能替代全局到工作流的同步合同。

## 8. Archive 和 media prune

Archive 是可逆 visibility state；Archive/restore 锁 asset、校验 expected revision、更新 timestamp/revision。

本期不调用 `prune_unreferenced_media_objects` 删除 LibraryAsset。未来 hard delete 必须：

- 独立命令和 explicit confirmation。
- 检查 ProductImageAsset、ImageSessionAsset、MediaLibraryAsset 及全部历史 RESTRICT references。
- 先 DB transaction 形成精确 cleanup list，再 best-effort/retry storage delete。
- 不删除 archive 作为“无引用”。

## 9. Attempt fencing

### 9.1 ImageSession

`ImageSessionGenerationTask` 增加 active attempt id/attempt count。Claim CAS queued->running 并写 attempt。下列写入带 task id + active attempt predicate：

- heartbeat/progress phase/completed candidates。
- generation group/candidate round 和 MediaObject/ImageSessionAsset persistence。
- result ids、success/failure/cancel。

候选 storage 写入在 attempt-local compensation scope 中。旧 attempt CAS 失败后删除自己尚未被 winner/shared row 引用的新文件。

### 9.2 Workflow

`WorkflowNodeRun` 增加 active attempt id/attempt count。Prompt artifact、generation record、result ProductImageAsset、node binding、cover proposal、rendition intent 和 status 在一个 attempt-owned transaction 中持久化，或每个阶段都校验 active token。

如果现有 provider call 不能事务包围，先 stage external output，再在 fenced transaction 内 persist；loser 只清理自己的 staged files。

## 10. Transactional delivery

新增 `AsyncDispatch` 或等价 infrastructure model：

- actor/operation kind、aggregate id、payload JSON。
- unique delivery key。
- pending/sent/consumed/dead 状态。
- available_at、attempt_count、lease owner/token/expiry、last error、sent/consumed timestamps。

Business transition 和 pending intent 同事务。独立 dispatcher service/process：

1. `FOR UPDATE SKIP LOCKED` lease available intents。
2. dispatcher 将 intent 标记 sent 后 enqueue Dramatiq message，payload 含 dispatch id 和 aggregate id；crash 会由 stale reconcile 补发。
3. worker 先以 consumer lease 原子领取 sent intent，再执行目标；重复消息不能抢走已有 consumer lease。
4. worker atomic business claim 后 mark consumed。
5. sent but unconsumed 且没有有效 consumer lease 的 intent 可重新 pending；长任务的有效 lease 不被 stale reconcile 打断。
6. Redis outage 更新 last error/backoff；达到尝试上限的 dead intent 冷却后自动回到 pending，不伪造业务 failure。

迁移顺序：ImageSession -> Workflow scheduler/node -> Agent sync -> rendition。全部迁移后删除 API lifespan recovery 和 Dramatiq import-time scans；dispatcher/reconciler 是单一 owner。

## 11. Agent scope 和 Draft

不建立 fake Product。目标结构允许 Agent conversation contract 指明 `scope_kind=product_workflow|media_library`。实现可以演化现有 `AgentConversation` 为 scope-aware aggregate，但必须保持现有 product/workflow rows 的 non-null/check 行为。

Media-library Agent tools：

- bounded list metadata。
- inspect explicit assets。
- publish organization Draft revision。

没有直接 rename/move/tag/archive/collect side-effect tool。ProductFlow internal API 返回 scope-specific contract；Go manager 依据 contract 注册对应 tools。Tool names/limits/version 由 ProductFlow contract 与 Go safety ceiling 分开命名并测试。

确认 use case 属于 PostgreSQL application；Go journal 不保存第二份 Draft authority。

## 12. Frontend

`GalleryPage` 改为 operational library page，建议拆分：

```text
pages/media-library/
  MediaLibraryPage.tsx
  MediaLibraryCommandBar.tsx
  MediaLibraryDirectoryTree.tsx
  MediaLibraryGrid.tsx
  MediaLibraryDetails.tsx
  MediaLibraryCollectDialog.tsx
  useMediaLibrary.ts
  libraryState.ts
```

- TanStack Query 管 server state；React 只保存 selection、view、drawer、local filters。
- 页面首屏直接提供搜索、目录、grid/list 和批量操作。
- 卡片尺寸稳定，名称可换行/截断，状态不只靠颜色。
- 关键操作使用 icon + tooltip；archive/collect 有明确文本 command。
- 详情使用现有 preview/dialog patterns，不复用 product-specific query owner。
- desktop/narrow/mobile 使用真实容器宽度；无营销 hero。
- `api.ts` 仍是 HTTP entry，但新 wire response 必须 runtime parse；逐步拆 API/type owner 的重构另行处理。

## 13. Contract generation policy

- FastAPI OpenAPI/Pydantic 是 ProductFlow public/internal HTTP schema owner；为稳定跨 Go/TS DTO 生成或检查 artifact。
- Go Agent/harness 是 SSE event schema owner；导出 discriminated event schema/fixtures给 TypeScript parser。
- unknown additive event 使用显式 policy：安全忽略并保留 cursor，不使用 unchecked cast。
- provider request/result、SQLAlchemy model、Python-only provenance dataclass 不生成跨语言 contract。
- CI regeneration 必须 deterministic，dirty diff 失败。

## 14. Migration sequence

历史 revision 不修改。建议后续 head revisions 与显式数据命令按职责拆分：

1. enforce ImageSession media authority。
2. add generation attempt fencing。
3. add async dispatch。
4. 进入维护窗口，停止旧 backend/worker/Agent mutation ingress，冻结 Gallery 写入和来源 ImageSession 删除；记录 PostgreSQL snapshot token、source high-watermark/count/hash。
5. expand media library core schema；Alembic 只创建 schema，不读取 storage。
6. 运行可重入 `audit/backfill_media_library` command：每页校验 frozen snapshot token/high-watermark，先 dry-run 读取 database + storage，再 bounded apply，最后 post-reconcile 文件可读性和 old/new hash。最终事务锁定 source tables 并证明 source delta 为零，随后部署只使用新 owner 的代码并解除维护窗口。
7. add product collection lineage。
8. add library organization tables。
9. add media-library Agent scope 和 organization Draft tables。
10. cleanup revision：只有独立确认后 drop duplicate session columns/old Gallery table。

每个 revision：

- fresh SQLite upgrade head。
- 从前一 revision 的 fixture upgrade。
- 受 PostgreSQL enum/lock/FK/index 影响时运行 live test。
- migration 不调用 provider、Redis、不读取文件，也不假定 storage mounted。
- migration 只执行数据库可确定转换；database facts 无法构造可信 provenance 时停止并要求先处理 preflight blocker。
- explicit backfill command 负责 storage readability、bounded apply、stable report 和幂等复跑；unknown source 不跳过。

## 15. Observability 和安全

- log 使用 request/task/attempt/dispatch correlation ids。
- 不记录 prompt 全文、provenance JSON、provider body、storage path、bytes/base64 或 secret。
- durable rows 记录 last delivery error、attempt id 和 conflict reason；logs 只是诊断。
- API 错误通过 domain errors，不暴露 filesystem/provider traceback。
- Agent runtime secret DTO 继续只在 internal authenticated route 使用，不加入 browser schema。

## 16. Alternatives

### 复用 `ProductImageAsset` 作为全局身份

拒绝。它要求 nullable product scope，破坏商品 FK、工作流 ownership 和现有 query invariants。

### 让 LibraryAsset media id 唯一

拒绝。相同 bytes 可以有不同来源和用户语义；来源幂等应按 source id。

### 原地改名 `ImageGalleryEntry`

拒绝。旧表 wire/schema/session cascade 过度绑定，clean new table + bounded backfill 更易对账和回滚。

### 长期 dual-write old/new Gallery

拒绝。会产生两个在线 owner。允许 maintenance-window backfill 和短期只读旧表，不允许双主写入。

### 只加周期 startup recovery

拒绝。不能关闭 commit->enqueue crash window，也不能 fence stale external work。

### PostgreSQL 直接替换 Dramatiq

暂缓。transactional delivery intent + existing broker 的改动更小，并保留当前 worker/runtime investment。

## 17. Architecture review gates

每阶段检查：

1. Ownership：bytes/library/product/delivery/Agent 各有一个 owner。
2. Dependency：presentation -> application -> ports/infrastructure；provider mapping 不进入 route。
3. Contract：wire/schema/version/unknown policy 同步。
4. Cascade：archive/folder/tag/session delete 不误删共享媒体。
5. Retirement：旧 reader/writer/fallback 有明确 residue scan 和触发器。
6. Complexity：不把 global queries 并入已有大型 product gallery module；不新增无证据 fallback。
7. Evidence：unit、migration、live PostgreSQL/Redis/storage、browser 与部署 report 覆盖实际 claim。
