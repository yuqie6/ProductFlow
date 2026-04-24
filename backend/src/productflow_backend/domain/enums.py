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


class JobKind(StrEnum):
    """异步任务类型：文案生成 / 海报生成。"""

    COPY_GENERATION = "copy_generation"
    POSTER_GENERATION = "poster_generation"


class JobStatus(StrEnum):
    """异步任务状态：排队 -> 运行中 -> 成功/失败。"""

    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"


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
    IMAGE_GENERATION = "image_generation"


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
