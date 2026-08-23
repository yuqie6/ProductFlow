# ADR 0006: 全局图库、工作流子图库与来源生命周期

## 状态

Accepted; amended 2026-08-17. References to schema-v2 as the online workflow describe the decision-time graph and are superseded by ADR 0008; the media identity, library authority, and lifecycle decisions remain in force.

Decision owner：ProductFlow repository owner。2026-08-16 的基础媒体身份、归档和 Agent 整理决策继续有效；2026-08-17 用户确认全局图库是主图片 owner，并要求图片同步/关联到每个工作流子图库。

在线入口和组织能力已交付。当前运行事实见 `docs/PRD.md` 与 `docs/ARCHITECTURE.md`。尚未完成的迁移证据和剩余产品项见 `docs/rollout/media-library-transition.md` 与 `docs/ROADMAP.md`。

## 2026-08-17 Amendment: 全局图库与工作流子图库

本修订覆盖本 ADR 后文中把“商品显式收录”描述成全局图库与工作流之间唯一或长期独立入口的表述。

- `MediaLibraryAsset` 是全局图库中的 canonical 图片身份。
- 用户明确保留的图片进入全局图库并长期积累。
- 全局图库资产按同步规则关联到每个工作流的子图库；同一资产可以被多个工作流使用。
- 工作流子图库是关联和使用层，不复制 `MediaObject` bytes，也不拥有另一套全局图片。
- 现有 `ProductImageAsset` 和 `source_library_asset_id` 可以作为工作流侧稳定引用或迁移适配基础，但不能继续作为平行全局图库 owner。
- 从全局图库到工作流子图库的同步由应用层显式合同负责；Agent 不直接执行同步、工作流图修改或媒体物理删除。
- 删除某个工作流侧关联、删除工作流或删除来源 ImageSession，都不能删除仍被全局图库引用的图片。
- 旧顶层 Gallery 只作为迁移/过渡 owner，最终由全局图库页面和 API 接管；工作流子图库不随旧 Gallery 退休。

本修订不授权 drop 旧表、删除持久化业务行或物理删除共享媒体。上述行为仍需要独立的迁移证据、备份/恢复证据和明确范围确认。

## 背景

ADR 0002 已确定：`MediaObject` 拥有不可变媒体字节，`ProductImageAsset` 拥有商品命名空间内的图片身份，工作流和封面只引用 `ProductImageAsset`。

历史 `/gallery` 的 `ImageGalleryEntry` 直接引用 `ImageSessionAsset`，并随 ImageSession 级联删除。它适合表达“某次连续生图会话中的收藏”，不能表达跨会话、跨商品长期保存的素材资产。旧在线 route、DTO 和 ORM owner 已退休，旧表只由迁移 reader 有界读取。

用户需要把明确选择的图片积累为全局图库，并让这些图片同步/关联到每个工作流的子图库，供实际工作流使用；同一张全局图片可以被多个工作流复用。该能力不能把工作流引用、全局素材身份和底层 bytes 合并成一个不可治理的对象。

## 决策

### 1. 三层身份

- `MediaObject` 继续唯一拥有不可变 bytes、storage path、MIME、尺寸、字节数、hash 和核验状态。
- 新增 `MediaLibraryAsset`，拥有全局素材显示名、provenance、revision、组织和归档状态。
- `ProductImageAsset` 继续拥有商品作用域身份、商品内显示名、目录、类型、派生和工作流绑定。
- `ImageSessionAsset` 只拥有 ImageSession 内身份，不拥有已保存素材的生命周期。

一个 `MediaObject` 可以被多个 `MediaLibraryAsset` 和 `ProductImageAsset` 引用。`MediaLibraryAsset.media_object_id` 不唯一；bytes 相同不证明用户语义、来源或命名相同。

### 2. 显式保存与工作流同步

- 只有用户明确执行“保存到全局图库”的图片才创建 `MediaLibraryAsset`。
- 同一来源的保存以 source id 幂等，不按 media hash 自动合并。
- 保存成功后，应用按同步规则创建或返回全局资产到工作流子图库的关联；同一全局资产可以关联多个工作流。
- 工作流子图库关联不复制 `MediaObject`，移除单个关联不删除全局资产。
- 现有 `ProductImageAsset` 和 `(product_id, source_library_asset_id)` 可以作为工作流侧稳定引用和迁移 lineage 的兼容基础，但不代表另一套全局图库 owner。
- 工作流 reference bindings、封面、generation reference lineage 和 rendition source/result 继续引用稳定的工作流侧身份，并能追溯到 `MediaLibraryAsset`。
- Agent 不直接执行工作流同步或工作流图修改；同步由应用层 command/规则负责。

### 3. 独立 provenance

- 素材保存时生成严格、版本化、限长、可 hash 的 immutable provenance snapshot。
- ImageSession/ProductImageAsset 等来源 FK 可以在来源删除后变为 null，但 snapshot 不重写；商品收录的 `source_library_asset_id` 属于受限 lineage，使用 RESTRICT，不在此规则内。
- snapshot 不复制完整 provider request/output、storage path、媒体 bytes、密钥或原始 Agent 工具 payload。
- 无法证明的来源字段保持 null/unknown，不通过文件名或数组位置推断。

### 4. 归档而非普通硬删除

- 用户“删除”全局素材的产品语义是可恢复归档。
- 归档默认隐藏并禁止新增工作流关联，但不改变已有工作流引用、历史记录、provenance 或 bytes。
- 第一版没有 hard-delete API、自动 retention 或媒体垃圾回收调度。
- 未来物理清理必须检查 session、library、product 和历史受限引用，并经过单独 ADR/操作确认。

### 5. 一层文件夹和多标签

- 素材最多属于一个一层 `MediaLibraryFolder`；删除文件夹只解除组织关系。
- 素材可以关联多个规范化 `MediaLibraryTag`；删除标签只解除关系。
- 文件夹和标签是素材库组织，不改变 `MediaObject`、商品内名称或历史来源。
- 第一版不支持嵌套文件夹和自动标签本体。

### 6. Agent 只发布整理 Draft

- 素材库 Agent 使用独立业务 scope，不伪造 Product 或 WorkflowDraft。
- main 的 Node.js/Pi adapter session 文件保存交互式 Turn transcript，ProductFlow PostgreSQL event store 保存 tool event 和 cursor；PostgreSQL 拥有素材、整理 Draft revision 和确认副作用。后台 durable runtime 的完整恢复语义仍需单独验收。
- Agent 读取有界素材元数据，并只检查明确选择的图片。
- Agent 发布 `LibraryOrganizationDraftRevision`，v1 operation 只包含 rename、move、set-tags、archive 和 restore，不直接批量修改业务状态，也不承担商品收录。
- 一个 revision 最多涉及 100 个唯一素材、256 个 operation、256 KiB canonical JSON；重复或冲突 operation 被拒绝。
- 用户确认明确 revision 后，ProductFlow 按 expected asset revision 在一个事务中原子应用；冲突整批拒绝。

### 7. 有界迁移和旧 owner 退休

- canonical media bug repair 必须先于素材库 schema。
- 新表从 `ImageGalleryEntry` 可审计回填，尽量保留 entry ID，并核对 source/media/provenance/count。
- cutover 后 `MediaLibraryAsset` 是唯一在线全局素材 owner；旧 Gallery 表只读保留有界窗口，不长期双写。
- 旧 API、DTO、前端调用、测试和 runtime model 一起退休。
- drop 旧表属于持久化 destructive cleanup，必须在备份、恢复和对账证据齐全后单独确认。current schema 使用 `media_library_cutover_gates` 记录独立证据；旧 `legacy_canvas_agent_20260518_0032` source 使用 Gallery-only manifest/approval artifact。`retire_legacy_gallery` 或 `retire_legacy_gallery_source` 都会在删除前重新锁定 source table、核对 source/reconciliation hash 后才删除旧表，不删除全局素材、共享媒体或工作流子图库关联；source bridge 不处理 Agent archive。

## 与现有 ADR 的关系

- ADR 0001 的业务 authority split 不变：main Pi adapter 的运行时 session/event state 与 PostgreSQL 业务状态分离；不可证明的后台效果继续保留 unknown。
- ADR 0002 不变：`MediaObject` 与 `ProductImageAsset` 的职责继续成立；`ProductImageAsset`/工作流侧稳定引用现在作为全局图库到工作流子图库的关联层使用。全局图片身份由本 ADR 的 `MediaLibraryAsset` 拥有，工作流不得绕过子图库关联直接持有未治理的全局引用。
- ADR 0003 的 GenerationSpec / DeliverySpec / 一层分组仍然有效。在线图权威见 ADR 0008。
- ADR 0004 不变：V1 archive/cutover 不作为素材库运行时 fallback。
- ADR 0005 的有界 tool-step projection 可供未来素材库 Agent UI 复用，但不能携带完整素材或工具 payload。

## 后果

- 删除 ImageSession 不再删除已保存素材。
- 一个全局素材可以被多个工作流使用，工作流子图库的局部组织不改变全局素材身份。
- 素材归档不破坏已经存在的工作流引用、历史 lineage 或运行记录。
- 媒体清理必须把全局图库和所有工作流侧引用纳入 owner 检查。
- 新增了全局素材与工作流侧图片之间的一致性约束；普通 FK 不能证明两个 `media_object_id` 相等，必须由锁定 application transaction、同步关联约束、迁移审计和并发测试共同保证。
- 第一版没有素材 hard delete；解除工作流关联不会删除全局素材或共享媒体。
- 迁移窗口必须核对旧 `ImageGalleryEntry` 的 source、media、provenance 和文件可读性；旧表清理不能删除已保存的全局资产。
- Gallery cutover、workflow sync、attempt fencing、transactional delivery 和 Agent 整理是独立可验证阶段，不能打包成无法定位故障的一次切换。

## 排除方案

- 让 `ProductImageAsset.product_id` nullable 并同时承担全局素材身份。
- 让工作流绕过子图库关联直接引用未治理的 `MediaLibraryAsset`。
- 用 storage path、filename、SHA 或数组位置充当素材业务身份。
- 对 `MediaLibraryAsset.media_object_id` 设置唯一约束并自动合并不同来源。
- 长期双写 `ImageGalleryEntry` 和 `MediaLibraryAsset`。
- 在删除来源会话时级联删除已保存素材。
- 让 Agent 直接执行无确认的全局批量整理或物理删除。
