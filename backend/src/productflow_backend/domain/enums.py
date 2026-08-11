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
