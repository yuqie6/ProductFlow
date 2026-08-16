# 素材库转型 PRD

## 1. 状态

- 文档状态：Approved for implementation
- 批准依据：Accepted `docs/adr/0006-media-library-authority.md`；该 ADR 记录仓库 owner 在 2026-08-16 的直接决策。
- 产品范围：单管理员、单商家 ProductFlow 工作区
- 当前实现声明：本文定义目标合同；当前 `/gallery` 仍是依附 `ImageSessionAsset` 的收藏画廊，不能据此宣称素材库已经交付。
- 当前事实仍由 `docs/PRD.md`、`docs/ARCHITECTURE.md`、代码、迁移和测试共同定义。

## 2. 问题

ProductFlow 已用 `MediaObject` 统一不可变媒体字节，并用 `ProductImageAsset` 表达商品作用域图片身份。当前全局 `/gallery` 只保存 `ImageGalleryEntry -> ImageSessionAsset` 引用：

- 收藏会随 ImageSession 删除而消失。
- 下载、MIME 和路径仍经过 session 资产的重复字段。
- 列表一次加载全部条目，没有面向长期积累的搜索、分页和组织合同。
- 商品只能直接从 ImageSession 附加图片，无法从一个长期素材池显式收录。
- Agent 只能整理某个商品的图片库，没有全局素材整理的可确认业务对象。

用户需要一个跨会话、跨商品长期积累的素材库。素材库保存用户明确选中的图片资产，并允许商品显式收录；它不自动吸纳所有商品图片，也不替代商品作用域身份。

## 3. 目标

1. 建立全局、长期、可归档的 `MediaLibraryAsset` 业务身份。
2. 保持 `MediaObject`、`MediaLibraryAsset`、`ProductImageAsset` 三种身份各自只有一个 owner。
3. 删除来源 ImageSession 后，已保存素材及其有界 provenance 仍可使用。
4. 用户从素材库显式收录到商品时创建 `ProductImageAsset`，复用同一个 `MediaObject`，不复制 bytes。
5. 提供适合长期增长的有界分页、搜索、文件夹、标签、归档和恢复能力。
6. Agent 只提交结构化整理 Draft；用户确认明确版本后才原子应用。
7. 在素材库上线前修复 canonical media 和异步执行中已确认的可靠性缺陷。

## 4. 非目标

- 多租户、团队权限、计费和跨商家共享。
- 工作流节点、封面或历史记录直接引用 `MediaLibraryAsset`。
- 自动把全部上传图、工作流结果或 ImageSession 结果加入素材库。
- 按 SHA 或视觉相似度自动合并不同逻辑资产。
- 嵌套文件夹、自动标签本体或数字资产管理审批流。
- 普通用户永久删除媒体 bytes；本期删除语义只有归档。
- 一次把整个素材库、完整 provenance 或媒体 bytes 送入 Agent 文本上下文。
- 为旧 `/api/gallery` 建立长期兼容 API 或并行写入 owner。

## 5. 核心对象与关系

- `MediaObject`：不可变 bytes、storage path、MIME、尺寸、字节数、hash 和核验状态。
- `MediaLibraryAsset`：全局素材身份、显示名、来源快照、revision 和归档状态；一个素材引用一个 `MediaObject`。
- `ProductImageAsset`：商品命名空间中的图片身份；从素材库收录时引用同一个 `MediaObject`，并记录 nullable、不可变的 `source_library_asset_id`。
- `ImageSessionAsset`：ImageSession 内的图片身份；保存到素材库后只作为可失效来源链接，不继续拥有素材生命周期。
- `MediaLibraryFolder`：一层用户文件夹；删除只解除组织关系。
- `MediaLibraryTag`：全局规范化标签；同一素材可有多个标签。
- `LibraryOrganizationDraft` / `LibraryOrganizationDraftRevision`：Agent 提出的可审阅整理计划及不可变版本。

同一 `MediaObject` 可以被多个不同的 `MediaLibraryAsset` 或 `ProductImageAsset` 引用。字节相同不等于用户语义、来源和命名相同。

## 6. 用户流程

### 6.1 保存到素材库

1. 用户在 ImageSession 生成结果或商品图片详情中执行“保存到素材库”。
2. 系统校验来源作用域、媒体核验状态和来源幂等身份。
3. 系统创建或返回同一来源已有的 `MediaLibraryAsset`，复用 `MediaObject`。
4. 系统保存有界、版本化、无秘密信息的 provenance snapshot。
5. 用户可在 `/gallery` 的素材库界面搜索和管理该素材。

### 6.2 收录到商品

1. 用户在素材库选择一张或多张活跃素材并选择目标商品。
2. 系统锁定目标商品和素材，校验素材未归档且媒体已核验。
3. 每个素材创建一个商品作用域 `ProductImageAsset`，复用同一个 `MediaObject`。
4. 重复收录同一素材到同一商品返回已有商品资产，不创建重复项。
5. 后续工作流、封面、目录和派生关系只引用 `ProductImageAsset`。

### 6.3 归档和恢复

- 归档素材后，默认素材库、选择器和新增商品收录不再展示或接受该素材。
- 归档不影响现有商品资产、工作流、封面、生成历史、provenance 或 bytes。
- 恢复沿用原素材 ID、revision 历史和来源快照。
- 来源 ImageSession 删除不改变素材的 active/archive 状态。

### 6.4 组织素材

- 用户可重命名素材、移动到一个一层文件夹并增删多个标签。
- 文件夹和标签筛选与列表使用相同过滤条件和有界 cursor pagination。
- 删除文件夹只把素材移到未整理状态；删除标签只解除标签关系。
- 组织变化不修改 `MediaObject`、商品图片名称或历史 provenance。

### 6.5 Agent 整理

1. 用户在素材库打开独立的 Agent 整理入口，并选择范围或输入整理目标。
2. Agent 读取有界元数据，并只检查明确选择的图片。
3. Agent 发布一个版本化 `LibraryOrganizationDraftRevision`，包含目标 asset、expected revision、目标名称/文件夹/标签/归档状态和理由。
4. 发布 Draft 不产生业务副作用。
5. 用户确认明确 revision 后，ProductFlow 在一个事务中校验全部 expected revision 并原子应用；任何冲突导致整批拒绝。

## 7. 产品需求

### ML-001 Canonical 媒体修复

- 所有在线 ImageSession 路径和 MIME 读取必须来自 `MediaObject`。
- `ImageSessionAsset.media_object_id` 在 ORM 和真实数据库中均为非空。
- 重复的 session `storage_path` / `mime_type` 只允许作为有界迁移 carrier，并在零读写、零 mismatch 后退休。

### ML-002 全局素材身份

- 只有用户明确保存的图片进入素材库。
- 同一 `ImageSessionAsset` 的重复保存幂等返回同一素材。
- 不对 `media_object_id` 设置唯一约束；不同来源可共享 bytes 而保持不同逻辑身份。
- 素材列表和详情始终通过 `MediaObject` 提供预览、下载和媒体元数据。
- 把 `ProductImageAsset` 保存到素材库不会授权删除源商品资产，也不能绕过现有 `require_deletion_enabled` 门禁；后续单独授权删除源商品资产时，素材与共享 `MediaObject` 仍可读取，nullable source-product FK 可清空，immutable provenance 不变。

### ML-003 Session-independent provenance

- provenance 使用严格、版本化、限长合同和 canonical hash。
- 最少保留 source kind、可用 source ids、来源时间、生成模型/provider 标识、候选/组标识、生成规格摘要和实测尺寸。
- 不保存完整 provider request/output、storage path、密钥、图片 bytes 或原始工具 payload。
- 来源被删除后，FK 可变为 null，但 snapshot 不重写。

### ML-004 显式商品收录

- 收录使用唯一约束 `(product_id, source_library_asset_id)`。
- `ProductImageAsset.media_object_id` 必须等于来源素材的 `media_object_id`；由锁定事务、应用校验和迁移审计保证。
- `source_library_asset_id` 只对非素材库收录的商品资产为 null；收录后不可改绑，并使用 `ON DELETE RESTRICT` 保留幂等 lineage。
- 来源素材未来归档不改变已收录商品资产；归档素材不能新收录，必须先恢复。
- 收录后的 `origin_type` 延续 provenance 对应的上传、工作流生成或 ImageSession 来源分类；`source_library_asset_id` 单独表达收录 lineage。

### ML-005 归档生命周期

- 普通删除等价于可恢复归档。
- 默认查询排除归档素材；归档视图可单独浏览和恢复。
- 本期没有 hard-delete API、自动 retention 或物理清理调度。
- `MediaObject` 清理判断必须包含 session、library 和 product 三类引用。

### ML-006 有界浏览和组织

- 列表支持 cursor pagination、名称搜索、来源、文件夹、标签、归档状态和创建时间排序。
- 列表只返回 thumbnail/preview URL 与有界摘要，不返回原图 bytes 或完整 provenance JSON。
- cursor 必须绑定过滤条件，并使用稳定 ID 作为排序 tie-breaker。
- 文件夹一层；标签名使用可移植的规范化 key 唯一约束。
- 单次 move/tag/archive/restore/collect 最多包含 100 个唯一素材；request JSON 最大 256 KiB，重复 asset id 被拒绝，锁按 asset id 稳定排序。

### ML-007 Agent 确认式整理

- 素材库 Agent 与商品 Workflow Agent 使用不同业务 scope，不伪造 Product 或 WorkflowDraft。
- Go journal 继续拥有 durable Turn、transcript、工具事件和 cursor；PostgreSQL 拥有整理 Draft 和业务变更。
- v1 Draft operation 仅限 `rename | move | set_tags | archive | restore`；商品收录保留为 collect dialog 中的显式用户命令。
- 一个 Draft revision 最多涉及 100 个唯一素材、包含 256 个 operation，JSON 最大 256 KiB；同一 Draft 对同一素材的冲突 operation 被拒绝。
- Agent 只能发布整理 Draft，不能直接 hard delete 或绕过用户确认。
- 确认使用 idempotency key、request hash、明确 revision 和 expected asset revision，并按 asset id 稳定加锁。

### ML-008 Gallery 迁移与退休

- 现有 `ImageGalleryEntry` 可审计回填为素材，并尽量保留原 entry ID。
- 显式 preflight/backfill/post-reconcile 必须核对 count、source id、media id、provenance hash、文件存在性和重复来源；Alembic migration 本身不读取 storage。
- cutover 后只有 `MediaLibraryAsset` 是在线 owner；旧表只读保留有界窗口。
- 旧 API、DTO、前端调用和运行时模型一起退休；旧表 drop 必须单独确认并有备份/恢复证据。

### ML-009 异步可靠性先决条件

- ImageSession task 和 WorkflowNodeRun 使用 active attempt fencing；旧 attempt 的 progress、结果、失败和媒体写入不能提交。
- 数据库业务状态与 broker delivery intent 在同一事务持久化。
- Redis/Dramatiq 维持 at-least-once delivery；重复消息由 durable identity、atomic claim 和 attempt fence 收敛。
- API startup 不承担最终恢复 owner；dispatcher/reconciler 持续恢复 queued delivery。

## 8. 成功标准

- 删除来源 ImageSession 后，已保存素材、provenance、预览和下载 100% 可解析。
- 同一来源重复保存和同一商品重复收录均不产生重复逻辑记录。
- 商品收录复用同一 `MediaObject`，不复制原图、preview 或 thumbnail。
- 归档/恢复不改变素材 ID，不破坏任何既有商品和历史引用。
- 纳入迁移的旧 Gallery 条目 100% 映射成功或进入明确异常报告，无静默丢失。
- 默认列表、搜索和 Agent 读取均有明确上限，不存在全库 bytes/JSON 加载路径。
- Redis 故障、重复 delivery、stale worker 和进程重启均产生可解释、可恢复的数据库状态。
- Agent 整理变更中未经用户确认直接生效的比例为 0%。

## 9. 发布边界

本文要求按 `canonical media repair -> attempt fencing -> durable delivery -> library core -> product collection -> organization -> Gallery cutover -> Agent curation` 顺序交付。每阶段必须独立验证、可停止，并遵循 `docs/rollout/media-library-transition.md` 的证据和停止条件。
