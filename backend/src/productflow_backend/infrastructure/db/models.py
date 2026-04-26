from __future__ import annotations

from datetime import UTC, datetime
from decimal import Decimal
from typing import Any
from uuid import uuid4

from sqlalchemy import JSON, Boolean, DateTime, ForeignKey, Index, Integer, Numeric, String, Text, text
from sqlalchemy import Enum as SqlEnum
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship

from productflow_backend.domain.enums import (
    CopyStatus,
    ImageSessionAssetKind,
    JobKind,
    JobStatus,
    PosterKind,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)


def utcnow() -> datetime:
    return datetime.now(UTC)


def new_id() -> str:
    return str(uuid4())


def enum_value_column(enum_cls: type) -> SqlEnum:
    return SqlEnum(
        enum_cls,
        name=enum_cls.__name__.lower(),
        values_callable=lambda members: [member.value for member in members],
        validate_strings=True,
    )


class Base(DeclarativeBase):
    pass


class TimestampMixin:
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=utcnow,
        onupdate=utcnow,
    )


class AppSetting(Base, TimestampMixin):
    """运行时配置的键值存储（可在运行时覆盖环境变量配置）。"""

    __tablename__ = "app_settings"

    key: Mapped[str] = mapped_column(String(120), primary_key=True)
    value: Mapped[str] = mapped_column(Text)


class Product(Base, TimestampMixin):
    __tablename__ = "products"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    name: Mapped[str] = mapped_column(String(255))
    category: Mapped[str | None] = mapped_column(String(120), nullable=True)
    price: Mapped[Decimal | None] = mapped_column(Numeric(10, 2), nullable=True)
    source_note: Mapped[str | None] = mapped_column(Text, nullable=True)
    current_confirmed_copy_set_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "copy_sets.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_products_current_confirmed_copy_set_id",
        ),
        nullable=True,
    )

    source_assets: Mapped[list[SourceAsset]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="SourceAsset.product_id",
    )
    creative_briefs: Mapped[list[CreativeBrief]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
    )
    copy_sets: Mapped[list[CopySet]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="CopySet.product_id",
    )
    confirmed_copy_set: Mapped[CopySet | None] = relationship(
        foreign_keys=[current_confirmed_copy_set_id],
        post_update=True,
    )
    poster_variants: Mapped[list[PosterVariant]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
    )
    job_runs: Mapped[list[JobRun]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
    )
    image_sessions: Mapped[list[ImageSession]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
    )
    workflows: Mapped[list[ProductWorkflow]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
    )


class ProductWorkflow(Base, TimestampMixin):
    """商品创意工作流：一个商品可以保留多个历史 DAG，当前使用 active=True 的工作流。"""

    __tablename__ = "product_workflows"
    __table_args__ = (
        Index(
            "uq_product_workflows_one_active_per_product",
            "product_id",
            unique=True,
            postgresql_where=text("active = true"),
            sqlite_where=text("active = 1"),
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    title: Mapped[str] = mapped_column(String(255), default="商品创意工作流")
    active: Mapped[bool] = mapped_column(Boolean, default=True)

    product: Mapped[Product] = relationship(back_populates="workflows")
    nodes: Mapped[list[WorkflowNode]] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
    )
    edges: Mapped[list[WorkflowEdge]] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowEdge.workflow_id",
    )
    runs: Mapped[list[WorkflowRun]] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
    )


class WorkflowNode(Base, TimestampMixin):
    """工作流节点配置与最近一次输出。"""

    __tablename__ = "workflow_nodes"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(String(36), ForeignKey("product_workflows.id", ondelete="CASCADE"))
    node_type: Mapped[WorkflowNodeType] = mapped_column(enum_value_column(WorkflowNodeType))
    title: Mapped[str] = mapped_column(String(255))
    position_x: Mapped[int] = mapped_column(default=0)
    position_y: Mapped[int] = mapped_column(default=0)
    config_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    status: Mapped[WorkflowNodeStatus] = mapped_column(
        enum_value_column(WorkflowNodeStatus),
        default=WorkflowNodeStatus.IDLE,
    )
    output_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    last_run_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="nodes")
    outgoing_edges: Mapped[list[WorkflowEdge]] = relationship(
        back_populates="source_node",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowEdge.source_node_id",
    )
    incoming_edges: Mapped[list[WorkflowEdge]] = relationship(
        back_populates="target_node",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowEdge.target_node_id",
    )
    node_runs: Mapped[list[WorkflowNodeRun]] = relationship(back_populates="node")


class WorkflowEdge(Base):
    """工作流有向边，表达节点间数据依赖。"""

    __tablename__ = "workflow_edges"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(String(36), ForeignKey("product_workflows.id", ondelete="CASCADE"))
    source_node_id: Mapped[str] = mapped_column(String(36), ForeignKey("workflow_nodes.id", ondelete="CASCADE"))
    target_node_id: Mapped[str] = mapped_column(String(36), ForeignKey("workflow_nodes.id", ondelete="CASCADE"))
    source_handle: Mapped[str | None] = mapped_column(String(80), nullable=True)
    target_handle: Mapped[str | None] = mapped_column(String(80), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="edges", foreign_keys=[workflow_id])
    source_node: Mapped[WorkflowNode] = relationship(back_populates="outgoing_edges", foreign_keys=[source_node_id])
    target_node: Mapped[WorkflowNode] = relationship(back_populates="incoming_edges", foreign_keys=[target_node_id])


class WorkflowRun(Base):
    """一次工作流执行记录。"""

    __tablename__ = "workflow_runs"
    __table_args__ = (
        Index(
            "uq_workflow_runs_one_running_per_workflow",
            "workflow_id",
            unique=True,
            postgresql_where=text("status = 'running'"),
            sqlite_where=text("status = 'running'"),
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(String(36), ForeignKey("product_workflows.id", ondelete="CASCADE"))
    status: Mapped[WorkflowRunStatus] = mapped_column(
        enum_value_column(WorkflowRunStatus),
        default=WorkflowRunStatus.RUNNING,
    )
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="runs")
    node_runs: Mapped[list[WorkflowNodeRun]] = relationship(
        back_populates="workflow_run",
        cascade="all, delete-orphan",
    )


class WorkflowNodeRun(Base):
    """一次运行内单个节点的输出与关联产物。"""

    __tablename__ = "workflow_node_runs"

    __table_args__ = (Index("ix_workflow_node_runs_run_node", "workflow_run_id", "node_id"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_run_id: Mapped[str] = mapped_column(String(36), ForeignKey("workflow_runs.id", ondelete="CASCADE"))
    node_id: Mapped[str] = mapped_column(String(36), ForeignKey("workflow_nodes.id", ondelete="CASCADE"))
    status: Mapped[WorkflowNodeStatus] = mapped_column(enum_value_column(WorkflowNodeStatus))
    output_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    copy_set_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("copy_sets.id", ondelete="SET NULL"),
        nullable=True,
    )
    poster_variant_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("poster_variants.id", ondelete="SET NULL"),
        nullable=True,
    )
    image_session_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("image_session_assets.id", ondelete="SET NULL"),
        nullable=True,
    )
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    workflow_run: Mapped[WorkflowRun] = relationship(back_populates="node_runs")
    node: Mapped[WorkflowNode] = relationship(back_populates="node_runs")


class SourceAsset(Base):
    """商品源素材（原始图/参考图），一个商品最多一张原始图。"""

    __tablename__ = "source_assets"
    __table_args__ = (
        Index(
            "uq_source_assets_one_original_per_product",
            "product_id",
            unique=True,
            postgresql_where=text("kind = 'original_image'"),
            sqlite_where=text("kind = 'original_image'"),
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    kind: Mapped[SourceAssetKind] = mapped_column(enum_value_column(SourceAssetKind))
    original_filename: Mapped[str] = mapped_column(String(255))
    mime_type: Mapped[str] = mapped_column(String(100))
    storage_path: Mapped[str] = mapped_column(String(500))
    source_poster_variant_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(back_populates="source_assets", foreign_keys=[product_id])


class CreativeBrief(Base):
    """AI 对商品的理解结果：定位/受众/卖点/禁忌词。"""

    __tablename__ = "creative_briefs"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    payload: Mapped[dict[str, Any]] = mapped_column(JSON)
    provider_name: Mapped[str] = mapped_column(String(50))
    model_name: Mapped[str] = mapped_column(String(100))
    prompt_version: Mapped[str] = mapped_column(String(32))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(back_populates="creative_briefs")
    copy_sets: Mapped[list[CopySet]] = relationship(back_populates="creative_brief")


class CopySet(Base, TimestampMixin):
    """文案版本，记录 AI 原始输出与人工编辑历史。"""

    __tablename__ = "copy_sets"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    creative_brief_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("creative_briefs.id", ondelete="SET NULL"),
        nullable=True,
    )
    status: Mapped[CopyStatus] = mapped_column(enum_value_column(CopyStatus), default=CopyStatus.DRAFT)

    title: Mapped[str] = mapped_column(Text)
    selling_points: Mapped[list[str]] = mapped_column(JSON)
    poster_headline: Mapped[str] = mapped_column(Text)
    cta: Mapped[str] = mapped_column(Text)

    model_title: Mapped[str] = mapped_column(Text)
    model_selling_points: Mapped[list[str]] = mapped_column(JSON)
    model_poster_headline: Mapped[str] = mapped_column(Text)
    model_cta: Mapped[str] = mapped_column(Text)

    provider_name: Mapped[str] = mapped_column(String(50))
    model_name: Mapped[str] = mapped_column(String(100))
    prompt_version: Mapped[str] = mapped_column(String(32))
    edited_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    confirmed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    product: Mapped[Product] = relationship(
        back_populates="copy_sets",
        foreign_keys=[product_id],
    )
    creative_brief: Mapped[CreativeBrief | None] = relationship(back_populates="copy_sets")
    poster_variants: Mapped[list[PosterVariant]] = relationship(back_populates="copy_set")


class PosterVariant(Base):
    """已生成的海报变体，关联文案和存储路径。"""

    __tablename__ = "poster_variants"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    copy_set_id: Mapped[str] = mapped_column(String(36), ForeignKey("copy_sets.id", ondelete="CASCADE"))
    kind: Mapped[PosterKind] = mapped_column(enum_value_column(PosterKind))
    template_name: Mapped[str] = mapped_column(String(100))
    mime_type: Mapped[str] = mapped_column(String(50), default="image/png")
    storage_path: Mapped[str] = mapped_column(String(500))
    width: Mapped[int] = mapped_column()
    height: Mapped[int] = mapped_column()
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(back_populates="poster_variants")
    copy_set: Mapped[CopySet] = relationship(back_populates="poster_variants")


class JobRun(Base):
    """异步任务记录，支持幂等去重和自动重试。"""

    __tablename__ = "job_runs"
    __table_args__ = (
        Index(
            "uq_job_runs_one_active_per_product_kind",
            "product_id",
            "kind",
            unique=True,
            postgresql_where=text("status IN ('queued', 'running')"),
            sqlite_where=text("status IN ('queued', 'running')"),
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    kind: Mapped[JobKind] = mapped_column(enum_value_column(JobKind))
    status: Mapped[JobStatus] = mapped_column(enum_value_column(JobStatus), default=JobStatus.QUEUED)
    target_poster_kind: Mapped[PosterKind | None] = mapped_column(
        enum_value_column(PosterKind),
        nullable=True,
    )
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    copy_set_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("copy_sets.id", ondelete="SET NULL"),
        nullable=True,
    )
    poster_variant_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("poster_variants.id", ondelete="SET NULL"),
        nullable=True,
    )
    attempts: Mapped[int] = mapped_column(default=0)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)

    product: Mapped[Product] = relationship(back_populates="job_runs")


class ImageSession(Base, TimestampMixin):
    """连续生图会话，含多轮对话历史与生成结果。"""

    __tablename__ = "image_sessions"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE"),
        nullable=True,
    )
    title: Mapped[str] = mapped_column(String(255))

    product: Mapped[Product | None] = relationship(back_populates="image_sessions")
    assets: Mapped[list[ImageSessionAsset]] = relationship(
        back_populates="session",
        cascade="all, delete-orphan",
    )
    rounds: Mapped[list[ImageSessionRound]] = relationship(
        back_populates="session",
        cascade="all, delete-orphan",
        order_by="ImageSessionRound.created_at",
    )
    generation_tasks: Mapped[list[ImageSessionGenerationTask]] = relationship(
        back_populates="session",
        cascade="all, delete-orphan",
        order_by="ImageSessionGenerationTask.created_at",
    )


class ImageSessionAsset(Base):
    __tablename__ = "image_session_assets"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    session_id: Mapped[str] = mapped_column(String(36), ForeignKey("image_sessions.id", ondelete="CASCADE"))
    kind: Mapped[ImageSessionAssetKind] = mapped_column(enum_value_column(ImageSessionAssetKind))
    original_filename: Mapped[str] = mapped_column(String(255))
    mime_type: Mapped[str] = mapped_column(String(100))
    storage_path: Mapped[str] = mapped_column(String(500))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    session: Mapped[ImageSession] = relationship(back_populates="assets")
    generated_in_round: Mapped[ImageSessionRound | None] = relationship(
        back_populates="generated_asset",
        foreign_keys="ImageSessionRound.generated_asset_id",
    )


class ImageSessionRound(Base):
    __tablename__ = "image_session_rounds"
    __table_args__ = (
        Index("uq_image_session_rounds_generated_asset_id", "generated_asset_id", unique=True),
        Index("ix_image_session_rounds_generation_group_id", "generation_group_id"),
        Index("ix_image_session_rounds_base_asset_id", "base_asset_id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    session_id: Mapped[str] = mapped_column(String(36), ForeignKey("image_sessions.id", ondelete="CASCADE"))
    prompt: Mapped[str] = mapped_column(Text)
    assistant_message: Mapped[str] = mapped_column(Text)
    size: Mapped[str] = mapped_column(String(32))
    model_name: Mapped[str] = mapped_column(String(100))
    provider_name: Mapped[str] = mapped_column(String(50))
    prompt_version: Mapped[str] = mapped_column(String(32))
    provider_response_id: Mapped[str | None] = mapped_column(String(128), nullable=True)
    previous_response_id: Mapped[str | None] = mapped_column(String(128), nullable=True)
    image_generation_call_id: Mapped[str | None] = mapped_column(String(128), nullable=True)
    provider_request_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    provider_output_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    generation_group_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    candidate_index: Mapped[int] = mapped_column(Integer, default=1)
    candidate_count: Mapped[int] = mapped_column(Integer, default=1)
    base_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("image_session_assets.id", ondelete="SET NULL", name="fk_image_session_rounds_base_asset_id"),
        nullable=True,
    )
    selected_reference_asset_ids: Mapped[list[str] | None] = mapped_column(JSON, nullable=True)
    generated_asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("image_session_assets.id", ondelete="CASCADE"),
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    session: Mapped[ImageSession] = relationship(back_populates="rounds")
    generated_asset: Mapped[ImageSessionAsset] = relationship(
        back_populates="generated_in_round",
        foreign_keys=[generated_asset_id],
    )
    base_asset: Mapped[ImageSessionAsset | None] = relationship(foreign_keys=[base_asset_id])


class ImageSessionGenerationTask(Base):
    """连续生图 durable 后台任务记录，数据库是 authoritative state。"""

    __tablename__ = "image_session_generation_tasks"
    __table_args__ = (
        Index("ix_image_session_generation_tasks_session_id", "session_id"),
        Index("ix_image_session_generation_tasks_status", "status"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    session_id: Mapped[str] = mapped_column(String(36), ForeignKey("image_sessions.id", ondelete="CASCADE"))
    status: Mapped[JobStatus] = mapped_column(enum_value_column(JobStatus), default=JobStatus.QUEUED)
    prompt: Mapped[str] = mapped_column(Text)
    size: Mapped[str] = mapped_column(String(32))
    base_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "image_session_assets.id",
            ondelete="SET NULL",
            name="fk_image_session_generation_tasks_base_asset_id",
        ),
        nullable=True,
    )
    selected_reference_asset_ids: Mapped[list[str] | None] = mapped_column(JSON, nullable=True)
    generation_count: Mapped[int] = mapped_column(Integer, default=1)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    result_generation_group_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    attempts: Mapped[int] = mapped_column(Integer, default=0)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)

    session: Mapped[ImageSession] = relationship(back_populates="generation_tasks")
    base_asset: Mapped[ImageSessionAsset | None] = relationship(foreign_keys=[base_asset_id])
