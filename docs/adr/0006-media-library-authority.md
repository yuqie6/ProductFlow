# ADR 0006: 全局素材身份、商品收录与来源生命周期

## 状态

Accepted

Decision owner：ProductFlow repository owner。2026-08-16 在本任务中确认显式商品收录、全局只归档以及 Agent 命名/文件夹/标签整理，并要求按该决策实施。

目标行为尚在实施中。当前运行事实仍见 `docs/PRD.md`、`docs/ARCHITECTURE.md` 和 `docs/rollout/media-library-transition.md`。

## 背景

ADR 0002 已确定：`MediaObject` 拥有不可变媒体字节，`ProductImageAsset` 拥有商品命名空间内的图片身份，工作流和封面只引用 `ProductImageAsset`。

当前 `/gallery` 的 `ImageGalleryEntry` 直接引用 `ImageSessionAsset`，并随 ImageSession 级联删除。它适合表达“某次连续生图会话中的收藏”，不能表达跨会话、跨商品长期保存的素材资产。与此同时，`ImageSessionAsset` 仍重复保存并读取 storage path/MIME，且数据库 migration 没有真正落实 ORM 声明的非空 `media_object_id`。

用户需要把明确选择的图片积累为全局素材库，让不同商品显式收录，并允许用户和 Agent 按名称、文件夹和标签整理。该能力不能把商品身份、全局素材身份和底层 bytes 再次合并成一个对象。

## 决策

### 1. 三层身份

- `MediaObject` 继续唯一拥有不可变 bytes、storage path、MIME、尺寸、字节数、hash 和核验状态。
- 新增 `MediaLibraryAsset`，拥有全局素材显示名、provenance、revision、组织和归档状态。
- `ProductImageAsset` 继续拥有商品作用域身份、商品内显示名、目录、类型、派生和工作流绑定。
- `ImageSessionAsset` 只拥有 ImageSession 内身份，不拥有已保存素材的生命周期。

一个 `MediaObject` 可以被多个 `MediaLibraryAsset` 和 `ProductImageAsset` 引用。`MediaLibraryAsset.media_object_id` 不唯一；bytes 相同不证明用户语义、来源或命名相同。

### 2. 显式保存与显式收录

- 只有用户明确执行“保存到素材库”的图片才创建 `MediaLibraryAsset`。
- 同一 `ImageSessionAsset` 的保存以 source id 幂等，不按 media hash 自动合并。
- 从素材库收录到商品时创建一个 `ProductImageAsset`，复用来源素材的 `MediaObject`。
- `(product_id, source_library_asset_id)` 唯一；`source_library_asset_id` 对素材库收录资产使用 `ON DELETE RESTRICT`，创建后不可改绑。
- 工作流 reference bindings、封面、generation reference lineage 和 rendition source/result 继续只引用 `ProductImageAsset`；视觉体系和 prompt artifact 沿用现有版本化合同，不直接引用 `MediaLibraryAsset`。
- 不允许工作流直接跨作用域引用 `MediaLibraryAsset`。

### 3. 独立 provenance

- 素材保存时生成严格、版本化、限长、可 hash 的 immutable provenance snapshot。
- ImageSession/ProductImageAsset 等来源 FK 可以在来源删除后变为 null，但 snapshot 不重写；商品收录的 `source_library_asset_id` 属于受限 lineage，使用 RESTRICT，不在此规则内。
- snapshot 不复制完整 provider request/output、storage path、媒体 bytes、密钥或原始 Agent 工具 payload。
- 无法证明的来源字段保持 null/unknown，不通过文件名或数组位置推断。

### 4. 归档而非普通硬删除

- 用户“删除”全局素材的产品语义是可恢复归档。
- 归档默认隐藏并禁止新增商品收录，但不改变已有商品资产、历史引用、provenance 或 bytes。
- 第一版没有 hard-delete API、自动 retention 或媒体垃圾回收调度。
- 未来物理清理必须检查 session、library、product 和历史受限引用，并经过单独 ADR/操作确认。

### 5. 一层文件夹和多标签

- 素材最多属于一个一层 `MediaLibraryFolder`；删除文件夹只解除组织关系。
- 素材可以关联多个规范化 `MediaLibraryTag`；删除标签只解除关系。
- 文件夹和标签是素材库组织，不改变 `MediaObject`、商品内名称或历史来源。
- 第一版不支持嵌套文件夹和自动标签本体。

### 6. Agent 只发布整理 Draft

- 素材库 Agent 使用独立业务 scope，不伪造 Product 或 WorkflowDraft。
- Go Agent journal 继续拥有 durable Turn、transcript、tool event 和 cursor；PostgreSQL 拥有素材、整理 Draft revision 和确认副作用。
- Agent 读取有界素材元数据，并只检查明确选择的图片。
- Agent 发布 `LibraryOrganizationDraftRevision`，v1 operation 只包含 rename、move、set-tags、archive 和 restore，不直接批量修改业务状态，也不承担商品收录。
- 一个 revision 最多涉及 100 个唯一素材、256 个 operation、256 KiB canonical JSON；重复或冲突 operation 被拒绝。
- 用户确认明确 revision 后，ProductFlow 按 expected asset revision 在一个事务中原子应用；冲突整批拒绝。

### 7. 有界迁移和旧 owner 退休

- canonical media bug repair 必须先于素材库 schema。
- 新表从 `ImageGalleryEntry` 可审计回填，尽量保留 entry ID，并核对 source/media/provenance/count。
- cutover 后 `MediaLibraryAsset` 是唯一在线全局素材 owner；旧 Gallery 表只读保留有界窗口，不长期双写。
- 旧 API、DTO、前端调用、测试和 runtime model 一起退休。
- drop 旧表属于持久化 destructive cleanup，必须在备份、恢复和对账证据齐全后单独确认。

## 与现有 ADR 的关系

- ADR 0001 不变：Go Agent journal 与 PostgreSQL 业务状态的 authority split 继续成立。
- ADR 0002 不变：`MediaObject` 与 `ProductImageAsset` 的职责继续成立；其中“图库引用 ProductImageAsset”限定为商品图片库。全局素材库由本 ADR 新增的 `MediaLibraryAsset` 拥有。
- ADR 0003 不变：在线工作流仍只有 schema-v2。
- ADR 0004 不变：V1 archive/cutover 不作为素材库运行时 fallback。
- ADR 0005 的有界 tool-step projection 可供未来素材库 Agent UI 复用，但不能携带完整素材或工具 payload。

## 后果

- 删除 ImageSession 不再删除已保存素材。
- 一个素材可显式收录到多个商品，而商品内命名和组织互不影响。
- 素材归档不破坏已经存在的商品工作流和历史 lineage。
- 媒体清理必须把素材库引用纳入 owner 检查。
- 新增了全局素材与商品图片之间的一致性约束；普通 FK 不能证明两个 `media_object_id` 相等，必须由锁定 application transaction、RESTRICT 且不可改绑的 lineage、迁移审计和并发测试共同保证。
- 第一版没有素材 hard delete，因此已收录商品资产会阻止来源素材被删除；未来 hard-delete 设计必须先保存独立 immutable source-library identity snapshot，才能评估放宽该 FK。
- Gallery cutover、attempt fencing、transactional delivery 和 Agent 整理是独立可验证阶段，不能打包成无法定位故障的一次切换。

## 排除方案

- 让 `ProductImageAsset.product_id` nullable 并同时承担全局素材身份。
- 让商品工作流直接引用 `MediaLibraryAsset`。
- 用 storage path、filename、SHA 或数组位置充当素材业务身份。
- 对 `MediaLibraryAsset.media_object_id` 设置唯一约束并自动合并不同来源。
- 长期双写 `ImageGalleryEntry` 和 `MediaLibraryAsset`。
- 在删除来源会话时级联删除已保存素材。
- 让 Agent 直接执行无确认的全局批量整理或物理删除。
