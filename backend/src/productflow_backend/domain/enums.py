from __future__ import annotations

from enum import StrEnum


class SourceAssetKind(StrEnum):
    """商品素材类型：原始主图 / 参考图 / 处理后商品图。"""

    ORIGINAL_IMAGE = "original_image"
    REFERENCE_IMAGE = "reference_image"
    PROCESSED_PRODUCT_IMAGE = "processed_product_image"


class ImageSessionAssetKind(StrEnum):
    """生图会话附件：用户上传参考图 / AI 生成图。"""

    REFERENCE_UPLOAD = "reference_upload"
    GENERATED_IMAGE = "generated_image"


class MediaVerificationStatus(StrEnum):
    """媒体对象的实际文件核验状态。"""

    VERIFIED = "verified"
    LEGACY_PENDING = "legacy_pending"
    MISSING = "missing"


class ProductImageOriginType(StrEnum):
    """商品图片进入商品命名空间的来源。"""

    UPLOAD = "upload"
    WORKFLOW_GENERATION = "workflow_generation"
    IMAGE_SESSION_ATTACH = "image_session_attach"
    LEGACY_IMPORT = "legacy_import"


class JobStatus(StrEnum):
    """连续生图任务状态：排队 -> 运行中 -> 成功/失败/取消。"""

    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"


class CopyStatus(StrEnum):
    """文案状态：草稿(可编辑) / 已确认(锁定用于海报)。"""

    DRAFT = "draft"
    CONFIRMED = "confirmed"


class PosterKind(StrEnum):
    """海报品种：商品主图 / 促销海报。"""

    MAIN_IMAGE = "main_image"
    PROMO_POSTER = "promo_poster"


class ProductWorkflowState(StrEnum):
    """商品流程推导状态：素材/文案/海报/失败。"""

    DRAFT = "draft"
    COPY_READY = "copy_ready"
    POSTER_READY = "poster_ready"
    FAILED = "failed"


class WorkflowNodeType(StrEnum):
    """商品工作流节点类型。"""

    PRODUCT_CONTEXT = "product_context"
    REFERENCE_IMAGE = "reference_image"
    COPY_GENERATION = "copy_generation"
    PROMPT_GENERATION = "prompt_generation"
    IMAGE_GENERATION = "image_generation"


class WorkflowDraftStatus(StrEnum):
    """Agent 工作流草案从收集到物化的持久化状态。"""

    COLLECTING = "collecting"
    AWAITING_CONFIRMATION = "awaiting_confirmation"
    CONFIRMED = "confirmed"
    MATERIALIZING = "materializing"
    READY = "ready"
    FAILED = "failed"
    CANCELLED = "cancelled"


class WorkflowRecipeKind(StrEnum):
    """用户主动保存的完整工作流配方或局部片段。"""

    WORKFLOW_RECIPE = "workflow_recipe"
    RECIPE_FRAGMENT = "recipe_fragment"


class AgentConversationStatus(StrEnum):
    """工作流 Agent 会话在 ProductFlow 侧的业务投影状态。"""

    COLLECTING = "collecting"
    AWAITING_CONFIRMATION = "awaiting_confirmation"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELED = "canceled"
    UNKNOWN = "unknown"


class AgentTurnStatus(StrEnum):
    """agent-harness Turn 状态的无损 ProductFlow 投影。"""

    QUEUED = "queued"
    RUNNING = "running"
    REQUIRES_INPUT = "requires_input"
    AWAITING_CONFIRMATION = "awaiting_confirmation"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCEL_REQUESTED = "cancel_requested"
    CANCELED = "canceled"
    UNKNOWN = "unknown"


class AgentToolMutationStatus(StrEnum):
    """Agent 可对账业务副作用在 ProductFlow 侧的持久化结果。"""

    PREPARED = "prepared"
    APPLIED = "applied"
    FAILED = "failed"
    UNKNOWN = "unknown"


class ProductFactStatus(StrEnum):
    """草案中单条商品事实的确认状态。"""

    OBSERVED = "observed"
    USER_DECLARED = "user_declared"
    CONFIRMED = "confirmed"
    CONFLICTED = "conflicted"


class ProductFactSourceType(StrEnum):
    """草案商品事实的来源类型。"""

    USER = "user"
    IMAGE_OBSERVATION = "image_observation"
    AGENT_INFERENCE = "agent_inference"
    LEGACY_PRODUCT = "legacy_product"


class WorkflowRevealEventKind(StrEnum):
    """已物化工作流在前端逐步揭示时使用的只读事件类型。"""

    FOLDER = "folder"
    NODE = "node"
    EDGE = "edge"
    COMPLETED = "completed"


class WorkflowNodeStatus(StrEnum):
    """工作流节点运行状态。"""

    IDLE = "idle"
    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"


class WorkflowRunStatus(StrEnum):
    """工作流运行记录状态。"""

    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"
