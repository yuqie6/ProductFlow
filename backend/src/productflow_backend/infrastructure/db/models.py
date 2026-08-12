from __future__ import annotations

from datetime import UTC, datetime
from decimal import Decimal
from typing import Any
from uuid import uuid4

from sqlalchemy import (
    JSON,
    BigInteger,
    Boolean,
    CheckConstraint,
    DateTime,
    ForeignKey,
    Index,
    Integer,
    Numeric,
    String,
    Text,
    UniqueConstraint,
    text,
)
from sqlalchemy import Enum as SqlEnum
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship

from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentToolMutationStatus,
    AgentTurnStatus,
    CopyStatus,
    ImageSessionAssetKind,
    JobStatus,
    MediaVerificationStatus,
    PosterKind,
    ProductImageOriginType,
    SourceAssetKind,
    WorkflowDraftStatus,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRevealEventKind,
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


class ProviderProfile(Base, TimestampMixin):
    """统一供应商档案，持有连接信息和可用能力。"""

    __tablename__ = "provider_profiles"
    __table_args__ = (
        Index("ix_provider_profiles_enabled", "enabled"),
        Index("ix_provider_profiles_archived_at", "archived_at"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    name: Mapped[str] = mapped_column(String(120))
    provider_type: Mapped[str] = mapped_column(String(40), default="openai_compatible")
    base_url: Mapped[str | None] = mapped_column(Text, nullable=True)
    api_key: Mapped[str | None] = mapped_column(Text, nullable=True)
    capabilities_json: Mapped[list[str]] = mapped_column(JSON, default=list)
    default_models_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    config_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    archived_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)


class ProviderBinding(Base, TimestampMixin):
    """用途绑定，表达文案/图片当前使用哪个供应商和接口。"""

    __tablename__ = "provider_bindings"
    __table_args__ = (Index("uq_provider_bindings_purpose", "purpose", unique=True),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    purpose: Mapped[str] = mapped_column(String(40), nullable=False)
    provider_kind: Mapped[str] = mapped_column(String(40), nullable=False)
    provider_profile_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("provider_profiles.id", ondelete="SET NULL"),
        nullable=True,
    )
    model_settings_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    config_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)

    provider_profile: Mapped[ProviderProfile | None] = relationship()


class UserCanvasTemplate(Base, TimestampMixin):
    """用户保存的可复用画布节点组模板。"""

    __tablename__ = "user_canvas_templates"
    __table_args__ = (Index("ix_user_canvas_templates_archived_at", "archived_at"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    key: Mapped[str] = mapped_column(String(80), unique=True, nullable=False)
    title: Mapped[str] = mapped_column(String(255))
    description: Mapped[str | None] = mapped_column(Text, nullable=True)
    kind: Mapped[str] = mapped_column(String(40), default="node_group")
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    template_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    archived_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)


class MediaObject(Base):
    """不可变的实际图片文件及其核验元数据。"""

    __tablename__ = "media_objects"
    __table_args__ = (
        UniqueConstraint("storage_path", name="uq_media_objects_storage_path"),
        CheckConstraint(
            "verification_status != 'verified' OR "
            "(byte_size > 0 AND width > 0 AND height > 0 AND sha256 IS NOT NULL "
            "AND length(sha256) = 64 AND verified_at IS NOT NULL)",
            name="ck_media_objects_verified_metadata",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    storage_path: Mapped[str] = mapped_column(String(500))
    mime_type: Mapped[str] = mapped_column(String(100))
    byte_size: Mapped[int | None] = mapped_column(BigInteger, nullable=True)
    width: Mapped[int | None] = mapped_column(Integer, nullable=True)
    height: Mapped[int | None] = mapped_column(Integer, nullable=True)
    sha256: Mapped[str | None] = mapped_column(String(64), nullable=True)
    verification_status: Mapped[MediaVerificationStatus] = mapped_column(
        enum_value_column(MediaVerificationStatus)
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    verified_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    product_assets: Mapped[list[ProductImageAsset]] = relationship(back_populates="media_object")
    image_session_assets: Mapped[list[ImageSessionAsset]] = relationship(back_populates="media_object")


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
    cover_image_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_products_cover_image_asset_id",
        ),
        nullable=True,
    )
    current_fact_set_version_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_fact_set_versions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_products_current_fact_set_version_id",
        ),
        nullable=True,
    )

    source_assets: Mapped[list[SourceAsset]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="SourceAsset.product_id",
    )
    image_assets: Mapped[list[ProductImageAsset]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="ProductImageAsset.product_id",
    )
    asset_folders: Mapped[list[ProductAssetFolder]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        order_by="ProductAssetFolder.sort_order, ProductAssetFolder.name, ProductAssetFolder.id",
    )
    cover_image_asset: Mapped[ProductImageAsset | None] = relationship(
        foreign_keys=[cover_image_asset_id],
        post_update=True,
    )
    fact_set_versions: Mapped[list[ProductFactSetVersion]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="ProductFactSetVersion.product_id",
    )
    current_fact_set_version: Mapped[ProductFactSetVersion | None] = relationship(
        foreign_keys=[current_fact_set_version_id],
        post_update=True,
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
    workflows: Mapped[list[ProductWorkflow]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
    )
    workflow_drafts: Mapped[list[WorkflowDraft]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowDraft.product_id",
    )
    agent_conversations: Mapped[list[AgentConversation]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="AgentConversation.product_id",
    )


class ProductAssetFolder(Base, TimestampMixin):
    """商品图库中的一层用户文件夹。"""

    __tablename__ = "product_asset_folders"
    __table_args__ = (
        UniqueConstraint("product_id", "name", name="uq_product_asset_folders_product_name"),
        CheckConstraint("sort_order >= 0", name="ck_product_asset_folders_non_negative_sort_order"),
        Index("ix_product_asset_folders_product_sort", "product_id", "sort_order", "name", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_product_asset_folders_product_id"),
    )
    name: Mapped[str] = mapped_column(String(120))
    sort_order: Mapped[int] = mapped_column(Integer)

    product: Mapped[Product] = relationship(back_populates="asset_folders")
    assets: Mapped[list[ProductImageAsset]] = relationship(back_populates="user_folder")


class ProductImageAsset(Base, TimestampMixin):
    """商品作用域内的逻辑图片；实际文件由 MediaObject 持有。"""

    __tablename__ = "product_image_assets"
    __table_args__ = (
        Index("ix_product_image_assets_product_created", "product_id", "created_at", "id"),
        Index(
            "ix_product_image_assets_product_folder_created",
            "product_id",
            "user_folder_id",
            "created_at",
            "id",
        ),
        Index(
            "ix_product_image_assets_product_type_created",
            "product_id",
            "image_type_key",
            "created_at",
            "id",
        ),
        Index(
            "ix_product_image_assets_product_origin_created",
            "product_id",
            "origin_type",
            "created_at",
            "id",
        ),
        Index("ix_product_image_assets_media_object_id", "media_object_id"),
        Index("ix_product_image_assets_parent_asset_id", "parent_asset_id"),
        Index("ix_product_image_assets_source_image_session_asset_id", "source_image_session_asset_id"),
        Index(
            "uq_product_image_assets_product_session_asset",
            "product_id",
            "source_image_session_asset_id",
            unique=True,
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_product_image_assets_product_id"),
    )
    media_object_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("media_objects.id", ondelete="RESTRICT", name="fk_product_image_assets_media_object_id"),
    )
    origin_type: Mapped[ProductImageOriginType] = mapped_column(enum_value_column(ProductImageOriginType))
    display_name: Mapped[str] = mapped_column(String(255))
    original_filename: Mapped[str] = mapped_column(String(255))
    image_type_key: Mapped[str | None] = mapped_column(String(80), nullable=True)
    user_folder_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_asset_folders.id",
            ondelete="SET NULL",
            name="fk_product_image_assets_user_folder_id",
        ),
        nullable=True,
    )
    parent_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_product_image_assets_parent_asset_id",
        ),
        nullable=True,
    )
    source_image_session_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "image_session_assets.id",
            ondelete="SET NULL",
            name="fk_product_image_assets_source_image_session_asset_id",
        ),
        nullable=True,
    )

    product: Mapped[Product] = relationship(back_populates="image_assets", foreign_keys=[product_id])
    user_folder: Mapped[ProductAssetFolder | None] = relationship(back_populates="assets")
    media_object: Mapped[MediaObject] = relationship(back_populates="product_assets")
    parent_asset: Mapped[ProductImageAsset | None] = relationship(
        back_populates="child_assets",
        foreign_keys=[parent_asset_id],
        remote_side=[id],
    )
    child_assets: Mapped[list[ProductImageAsset]] = relationship(
        back_populates="parent_asset",
        foreign_keys=[parent_asset_id],
    )
    source_image_session_asset: Mapped[ImageSessionAsset | None] = relationship(
        foreign_keys=[source_image_session_asset_id]
    )


class VisualSystem(Base, TimestampMixin):
    """用户保存的视觉体系稳定身份。"""

    __tablename__ = "visual_systems"
    __table_args__ = (Index("ix_visual_systems_archived_at", "archived_at"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    name: Mapped[str] = mapped_column(String(255))
    archived_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    versions: Mapped[list[VisualSystemVersion]] = relationship(
        back_populates="visual_system",
        cascade="all, delete-orphan",
        order_by="VisualSystemVersion.version",
    )


class VisualSystemVersion(Base):
    """视觉体系的不可变结构化版本。"""

    __tablename__ = "visual_system_versions"
    __table_args__ = (
        UniqueConstraint("visual_system_id", "version", name="uq_visual_system_versions_system_version"),
        UniqueConstraint(
            "source_draft_revision_id",
            name="uq_visual_system_versions_source_draft_revision_id",
        ),
        CheckConstraint("version > 0", name="ck_visual_system_versions_positive_version"),
        CheckConstraint("schema_version = 1", name="ck_visual_system_versions_schema_version"),
        CheckConstraint("length(payload_hash) = 64", name="ck_visual_system_versions_payload_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    visual_system_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("visual_systems.id", ondelete="CASCADE", name="fk_visual_system_versions_system_id"),
    )
    version: Mapped[int] = mapped_column(Integer)
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    payload_hash: Mapped[str] = mapped_column(String(64))
    source_markdown: Mapped[str | None] = mapped_column(Text, nullable=True)
    source_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_visual_system_versions_source_draft_revision_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    visual_system: Mapped[VisualSystem] = relationship(back_populates="versions")
    references: Mapped[list[VisualSystemVersionReference]] = relationship(
        back_populates="visual_system_version",
        cascade="all, delete-orphan",
        order_by="VisualSystemVersionReference.position",
    )


class VisualSystemVersionReference(Base):
    """视觉体系版本明确选入的商品图片。"""

    __tablename__ = "visual_system_version_references"
    __table_args__ = (
        UniqueConstraint(
            "visual_system_version_id",
            "position",
            name="uq_visual_system_version_references_position",
        ),
        UniqueConstraint(
            "visual_system_version_id",
            "asset_id",
            "role",
            name="uq_visual_system_version_references_asset_role",
        ),
        CheckConstraint("position >= 0", name="ck_visual_system_version_references_non_negative_position"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    visual_system_version_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "visual_system_versions.id",
            ondelete="CASCADE",
            name="fk_visual_system_version_references_version_id",
        ),
    )
    asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_visual_system_version_references_asset_id",
        ),
    )
    role: Mapped[str] = mapped_column(String(120))
    label: Mapped[str] = mapped_column(String(255))
    position: Mapped[int] = mapped_column(Integer)

    visual_system_version: Mapped[VisualSystemVersion] = relationship(back_populates="references")
    asset: Mapped[ProductImageAsset] = relationship()


class ProductFactSetVersion(Base):
    """用户确认后的不可变商品事实版本。"""

    __tablename__ = "product_fact_set_versions"
    __table_args__ = (
        UniqueConstraint("product_id", "version", name="uq_product_fact_set_versions_product_version"),
        UniqueConstraint(
            "source_draft_revision_id",
            name="uq_product_fact_set_versions_source_draft_revision_id",
        ),
        CheckConstraint("version > 0", name="ck_product_fact_set_versions_positive_version"),
        CheckConstraint("length(payload_hash) = 64", name="ck_product_fact_set_versions_payload_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_product_fact_set_versions_product_id"),
    )
    version: Mapped[int] = mapped_column(Integer)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    payload_hash: Mapped[str] = mapped_column(String(64))
    source_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_product_fact_set_versions_source_draft_revision_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(back_populates="fact_set_versions", foreign_keys=[product_id])
    source_draft_revision: Mapped[WorkflowDraftRevision | None] = relationship(
        back_populates="fact_set_version",
        foreign_keys=[source_draft_revision_id],
    )


class WorkflowDraft(Base, TimestampMixin):
    """Agent 工作流草案的稳定身份和当前状态。"""

    __tablename__ = "workflow_drafts"
    __table_args__ = (Index("ix_workflow_drafts_product_status", "product_id", "status"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_workflow_drafts_product_id"),
    )
    status: Mapped[WorkflowDraftStatus] = mapped_column(
        enum_value_column(WorkflowDraftStatus),
        default=WorkflowDraftStatus.COLLECTING,
    )
    current_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_workflow_drafts_current_revision_id",
        ),
        nullable=True,
    )
    final_workflow_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_workflows.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_workflow_drafts_final_workflow_id",
        ),
        nullable=True,
    )

    product: Mapped[Product] = relationship(back_populates="workflow_drafts", foreign_keys=[product_id])
    revisions: Mapped[list[WorkflowDraftRevision]] = relationship(
        back_populates="draft",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowDraftRevision.draft_id",
        order_by="WorkflowDraftRevision.version",
    )
    current_revision: Mapped[WorkflowDraftRevision | None] = relationship(
        foreign_keys=[current_revision_id],
        post_update=True,
    )
    final_workflow: Mapped[ProductWorkflow | None] = relationship(
        foreign_keys=[final_workflow_id],
        post_update=True,
    )
    agent_conversation: Mapped[AgentConversation | None] = relationship(
        back_populates="workflow_draft",
        foreign_keys="AgentConversation.workflow_draft_id",
        uselist=False,
    )


class WorkflowDraftRevision(Base):
    """WorkflowDraft 的 append-only 完整 artifact 快照。"""

    __tablename__ = "workflow_draft_revisions"
    __table_args__ = (
        UniqueConstraint("draft_id", "version", name="uq_workflow_draft_revisions_draft_version"),
        UniqueConstraint(
            "draft_id",
            "source_turn_id",
            "source_artifact_step_id",
            name="uq_workflow_draft_revisions_artifact_origin",
        ),
        CheckConstraint("version > 0", name="ck_workflow_draft_revisions_positive_version"),
        CheckConstraint("schema_version = 1", name="ck_workflow_draft_revisions_schema_version"),
        CheckConstraint("length(payload_hash) = 64", name="ck_workflow_draft_revisions_payload_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    draft_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_drafts.id", ondelete="CASCADE", name="fk_workflow_draft_revisions_draft_id"),
    )
    version: Mapped[int] = mapped_column(Integer)
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    payload_hash: Mapped[str] = mapped_column(String(64))
    source_turn_id: Mapped[str | None] = mapped_column(String(120), nullable=True)
    source_artifact_step_id: Mapped[str | None] = mapped_column(String(120), nullable=True)
    visual_system_version_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "visual_system_versions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_workflow_draft_revisions_visual_system_version_id",
        ),
        nullable=True,
    )
    confirmed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    draft: Mapped[WorkflowDraft] = relationship(back_populates="revisions", foreign_keys=[draft_id])
    fact_set_version: Mapped[ProductFactSetVersion | None] = relationship(
        back_populates="source_draft_revision",
        foreign_keys="ProductFactSetVersion.source_draft_revision_id",
        uselist=False,
    )
    visual_system_version: Mapped[VisualSystemVersion | None] = relationship(
        foreign_keys=[visual_system_version_id]
    )
    agent_turn_projection: Mapped[AgentTurnProjection | None] = relationship(
        back_populates="workflow_draft_revision",
        foreign_keys="AgentTurnProjection.workflow_draft_revision_id",
        uselist=False,
    )


class AgentConversation(Base, TimestampMixin):
    """ProductFlow 对一个隔离 agent-harness run 的业务作用域绑定。"""

    __tablename__ = "agent_conversations"
    __table_args__ = (
        UniqueConstraint("workflow_draft_id", name="uq_agent_conversations_workflow_draft_id"),
        UniqueConstraint("harness_run_id", name="uq_agent_conversations_harness_run_id"),
        Index("ix_agent_conversations_product_status", "product_id", "status"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_agent_conversations_product_id"),
    )
    workflow_draft_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_drafts.id",
            ondelete="CASCADE",
            name="fk_agent_conversations_workflow_draft_id",
        ),
    )
    harness_run_id: Mapped[str] = mapped_column(String(120))
    status: Mapped[AgentConversationStatus] = mapped_column(
        enum_value_column(AgentConversationStatus),
        default=AgentConversationStatus.COLLECTING,
    )

    product: Mapped[Product] = relationship(
        back_populates="agent_conversations",
        foreign_keys=[product_id],
    )
    workflow_draft: Mapped[WorkflowDraft] = relationship(
        back_populates="agent_conversation",
        foreign_keys=[workflow_draft_id],
    )
    turns: Mapped[list[AgentTurnProjection]] = relationship(
        back_populates="conversation",
        cascade="all, delete-orphan",
        order_by="AgentTurnProjection.created_at",
    )
    tool_mutations: Mapped[list[AgentToolMutation]] = relationship(
        back_populates="conversation",
        cascade="all, delete-orphan",
    )


class AgentTurnProjection(Base, TimestampMixin):
    """浏览器和 worker 使用的 harness Turn 有界投影，不保存 transcript。"""

    __tablename__ = "agent_turn_projections"
    __table_args__ = (
        UniqueConstraint(
            "conversation_id",
            "idempotency_key",
            name="uq_agent_turn_projections_conversation_key",
        ),
        UniqueConstraint("harness_turn_id", name="uq_agent_turn_projections_harness_turn_id"),
        UniqueConstraint(
            "workflow_draft_revision_id",
            name="uq_agent_turn_projections_workflow_draft_revision_id",
        ),
        CheckConstraint("length(request_hash) = 64", name="ck_agent_turn_projections_request_hash"),
        Index(
            "ix_agent_turn_projections_conversation_created",
            "conversation_id",
            "created_at",
            "id",
        ),
        Index("ix_agent_turn_projections_status", "status"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    conversation_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_conversations.id",
            ondelete="CASCADE",
            name="fk_agent_turn_projections_conversation_id",
        ),
    )
    harness_turn_id: Mapped[str | None] = mapped_column(String(120), nullable=True)
    idempotency_key: Mapped[str] = mapped_column(String(200))
    request_hash: Mapped[str] = mapped_column(String(64))
    input_text: Mapped[str] = mapped_column(Text)
    input_asset_ids_json: Mapped[list[str]] = mapped_column(JSON, default=list)
    status: Mapped[AgentTurnStatus] = mapped_column(
        enum_value_column(AgentTurnStatus),
        default=AgentTurnStatus.QUEUED,
    )
    resume_required: Mapped[bool] = mapped_column(Boolean, default=False)
    output_text: Mapped[str | None] = mapped_column(Text, nullable=True)
    error_text: Mapped[str | None] = mapped_column(Text, nullable=True)
    question_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    artifact_name: Mapped[str | None] = mapped_column(String(120), nullable=True)
    artifact_step_id: Mapped[str | None] = mapped_column(String(120), nullable=True)
    workflow_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            name="fk_agent_turn_projections_workflow_draft_revision_id",
        ),
        nullable=True,
    )
    sync_error: Mapped[str | None] = mapped_column(Text, nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    conversation: Mapped[AgentConversation] = relationship(back_populates="turns")
    workflow_draft_revision: Mapped[WorkflowDraftRevision | None] = relationship(
        back_populates="agent_turn_projection",
        foreign_keys=[workflow_draft_revision_id],
    )


class AgentToolMutation(Base, TimestampMixin):
    """ProductFlow 内部工具副作用的幂等账本。"""

    __tablename__ = "agent_tool_mutations"
    __table_args__ = (
        UniqueConstraint(
            "conversation_id",
            "tool_name",
            "idempotency_key",
            name="uq_agent_tool_mutations_conversation_tool_key",
        ),
        CheckConstraint("length(request_hash) = 64", name="ck_agent_tool_mutations_request_hash"),
        Index("ix_agent_tool_mutations_asset_id", "asset_id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    conversation_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_conversations.id",
            ondelete="CASCADE",
            name="fk_agent_tool_mutations_conversation_id",
        ),
    )
    tool_name: Mapped[str] = mapped_column(String(120))
    idempotency_key: Mapped[str] = mapped_column(String(200))
    request_hash: Mapped[str] = mapped_column(String(64))
    asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="SET NULL",
            name="fk_agent_tool_mutations_asset_id",
        ),
        nullable=True,
    )
    expected_display_name: Mapped[str | None] = mapped_column(String(255), nullable=True)
    target_display_name: Mapped[str | None] = mapped_column(String(255), nullable=True)
    prepared_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    status: Mapped[AgentToolMutationStatus] = mapped_column(
        enum_value_column(AgentToolMutationStatus),
        default=AgentToolMutationStatus.PREPARED,
    )
    result_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)

    conversation: Mapped[AgentConversation] = relationship(back_populates="tool_mutations")
    asset: Mapped[ProductImageAsset | None] = relationship()


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
        Index(
            "uq_product_workflows_product_v2_revision",
            "product_id",
            "revision",
            unique=True,
            postgresql_where=text("schema_version = 2"),
            sqlite_where=text("schema_version = 2"),
        ),
        CheckConstraint("schema_version IN (1, 2)", name="ck_product_workflows_schema_version"),
        CheckConstraint("revision > 0", name="ck_product_workflows_positive_revision"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    title: Mapped[str] = mapped_column(String(255), default="商品创意工作流")
    active: Mapped[bool] = mapped_column(Boolean, default=True)
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    revision: Mapped[int] = mapped_column(Integer, default=1)
    source_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            name="fk_product_workflows_source_draft_revision_id",
        ),
        nullable=True,
    )
    visual_system_version_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "visual_system_versions.id",
            ondelete="RESTRICT",
            name="fk_product_workflows_visual_system_version_id",
        ),
        nullable=True,
    )

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
    folders: Mapped[list[WorkflowFolder]] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
        order_by="WorkflowFolder.sort_order",
    )
    source_draft_revision: Mapped[WorkflowDraftRevision | None] = relationship(
        foreign_keys=[source_draft_revision_id]
    )
    visual_system_version: Mapped[VisualSystemVersion | None] = relationship(
        foreign_keys=[visual_system_version_id]
    )
    prompt_artifacts: Mapped[list[ImagePromptArtifact]] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
    )
    visual_exceptions: Mapped[list[VisualException]] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
    )
    image_generation_records: Mapped[list[WorkflowImageGenerationRecord]] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
    )
    materialization: Mapped[WorkflowMaterialization | None] = relationship(
        back_populates="workflow",
        cascade="all, delete-orphan",
        uselist=False,
    )


class WorkflowFolder(Base, TimestampMixin):
    """schema-v2 画布的一层文件夹。"""

    __tablename__ = "workflow_folders"
    __table_args__ = (
        UniqueConstraint("workflow_id", "folder_key", name="uq_workflow_folders_workflow_key"),
        CheckConstraint("sort_order >= 0", name="ck_workflow_folders_non_negative_order"),
        CheckConstraint("width > 0 AND height > 0", name="ck_workflow_folders_positive_size"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("product_workflows.id", ondelete="CASCADE", name="fk_workflow_folders_workflow_id"),
    )
    folder_key: Mapped[str] = mapped_column(String(80))
    title: Mapped[str] = mapped_column(String(255))
    sort_order: Mapped[int] = mapped_column(Integer)
    position_x: Mapped[int] = mapped_column(Integer, default=0)
    position_y: Mapped[int] = mapped_column(Integer, default=0)
    width: Mapped[int] = mapped_column(Integer)
    height: Mapped[int] = mapped_column(Integer)
    config_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="folders")
    nodes: Mapped[list[WorkflowNode]] = relationship(back_populates="folder")


class WorkflowNode(Base, TimestampMixin):
    """工作流节点配置与最近一次输出。"""

    __tablename__ = "workflow_nodes"
    __table_args__ = (
        UniqueConstraint("workflow_id", "node_key", name="uq_workflow_nodes_workflow_key"),
        CheckConstraint("schema_version IN (1, 2)", name="ck_workflow_nodes_schema_version"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(String(36), ForeignKey("product_workflows.id", ondelete="CASCADE"))
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    node_key: Mapped[str | None] = mapped_column(String(80), nullable=True)
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
    folder_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("workflow_folders.id", ondelete="SET NULL", name="fk_workflow_nodes_folder_id"),
        nullable=True,
    )
    bound_image_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_workflow_nodes_bound_image_asset_id",
        ),
        nullable=True,
    )
    current_prompt_artifact_version_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "image_prompt_artifact_versions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_workflow_nodes_current_prompt_artifact_version_id",
        ),
        nullable=True,
    )

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="nodes")
    folder: Mapped[WorkflowFolder | None] = relationship(back_populates="nodes")
    bound_image_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[bound_image_asset_id])
    current_prompt_artifact_version: Mapped[ImagePromptArtifactVersion | None] = relationship(
        foreign_keys=[current_prompt_artifact_version_id]
    )
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
    image_generation_records: Mapped[list[WorkflowImageGenerationRecord]] = relationship(
        back_populates="node"
    )


class WorkflowEdge(Base):
    """工作流有向边，表达节点间数据依赖。"""

    __tablename__ = "workflow_edges"
    __table_args__ = (UniqueConstraint("workflow_id", "edge_key", name="uq_workflow_edges_workflow_key"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(String(36), ForeignKey("product_workflows.id", ondelete="CASCADE"))
    edge_key: Mapped[str | None] = mapped_column(String(80), nullable=True)
    source_node_id: Mapped[str] = mapped_column(String(36), ForeignKey("workflow_nodes.id", ondelete="CASCADE"))
    target_node_id: Mapped[str] = mapped_column(String(36), ForeignKey("workflow_nodes.id", ondelete="CASCADE"))
    source_handle: Mapped[str | None] = mapped_column(String(80), nullable=True)
    target_handle: Mapped[str | None] = mapped_column(String(80), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="edges", foreign_keys=[workflow_id])
    source_node: Mapped[WorkflowNode] = relationship(back_populates="outgoing_edges", foreign_keys=[source_node_id])
    target_node: Mapped[WorkflowNode] = relationship(back_populates="incoming_edges", foreign_keys=[target_node_id])


class WorkflowMaterialization(Base):
    """confirmed Draft revision 到完整 v2 workflow 的幂等映射。"""

    __tablename__ = "workflow_materializations"
    __table_args__ = (
        UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_materializations_product_idempotency",
        ),
        UniqueConstraint("draft_revision_id", name="uq_workflow_materializations_draft_revision_id"),
        UniqueConstraint("workflow_id", name="uq_workflow_materializations_workflow_id"),
        CheckConstraint("length(request_hash) = 64", name="ck_workflow_materializations_request_hash"),
        CheckConstraint(
            "expected_draft_version > 0 AND expected_workflow_revision >= 0",
            name="ck_workflow_materializations_expected_versions",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_workflow_materializations_product_id"),
    )
    draft_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_drafts.id", ondelete="CASCADE", name="fk_workflow_materializations_draft_id"),
    )
    draft_revision_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="CASCADE",
            name="fk_workflow_materializations_draft_revision_id",
        ),
    )
    workflow_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("product_workflows.id", ondelete="CASCADE", name="fk_workflow_materializations_workflow_id"),
    )
    idempotency_key: Mapped[str] = mapped_column(String(120))
    request_hash: Mapped[str] = mapped_column(String(64))
    expected_draft_version: Mapped[int] = mapped_column(Integer)
    expected_workflow_revision: Mapped[int] = mapped_column(Integer)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="materialization")
    reveal_events: Mapped[list[WorkflowRevealEvent]] = relationship(
        back_populates="materialization",
        cascade="all, delete-orphan",
        order_by="WorkflowRevealEvent.sequence",
    )
    idempotency_keys: Mapped[list[WorkflowMaterializationKey]] = relationship(
        back_populates="materialization",
        cascade="all, delete-orphan",
    )


class WorkflowMaterializationKey(Base):
    """每个成功物化请求使用过的 product-scoped idempotency key。"""

    __tablename__ = "workflow_materialization_keys"
    __table_args__ = (
        UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_materialization_keys_product_key",
        ),
        CheckConstraint("length(request_hash) = 64", name="ck_workflow_materialization_keys_request_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_workflow_materialization_keys_product_id"),
    )
    materialization_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_materializations.id",
            ondelete="CASCADE",
            name="fk_workflow_materialization_keys_materialization_id",
        ),
    )
    idempotency_key: Mapped[str] = mapped_column(String(120))
    request_hash: Mapped[str] = mapped_column(String(64))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    materialization: Mapped[WorkflowMaterialization] = relationship(back_populates="idempotency_keys")


class WorkflowRevealEvent(Base):
    """物化完成后供画布只读重放的有序揭示事件。"""

    __tablename__ = "workflow_reveal_events"
    __table_args__ = (
        UniqueConstraint(
            "materialization_id",
            "sequence",
            name="uq_workflow_reveal_events_materialization_sequence",
        ),
        CheckConstraint("sequence > 0", name="ck_workflow_reveal_events_positive_sequence"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    materialization_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_materializations.id",
            ondelete="CASCADE",
            name="fk_workflow_reveal_events_materialization_id",
        ),
    )
    sequence: Mapped[int] = mapped_column(Integer)
    kind: Mapped[WorkflowRevealEventKind] = mapped_column(enum_value_column(WorkflowRevealEventKind))
    entity_type: Mapped[str | None] = mapped_column(String(40), nullable=True)
    entity_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    materialization: Mapped[WorkflowMaterialization] = relationship(back_populates="reveal_events")


class WorkflowRun(Base):
    """一次工作流执行记录。"""

    __tablename__ = "workflow_runs"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(String(36), ForeignKey("product_workflows.id", ondelete="CASCADE"))
    status: Mapped[WorkflowRunStatus] = mapped_column(
        enum_value_column(WorkflowRunStatus),
        default=WorkflowRunStatus.RUNNING,
    )
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)
    progress_metadata: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="runs")
    node_runs: Mapped[list[WorkflowNodeRun]] = relationship(
        back_populates="workflow_run",
        cascade="all, delete-orphan",
    )


class WorkflowNodeRun(Base):
    """一次运行内单个节点的输出与关联产物。"""

    __tablename__ = "workflow_node_runs"

    __table_args__ = (
        Index("ix_workflow_node_runs_run_node", "workflow_run_id", "node_id"),
        Index(
            "uq_workflow_node_runs_one_active_per_node",
            "node_id",
            unique=True,
            postgresql_where=text("status IN ('queued', 'running')"),
            sqlite_where=text("status IN ('queued', 'running')"),
        ),
    )

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
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    workflow_run: Mapped[WorkflowRun] = relationship(back_populates="node_runs")
    node: Mapped[WorkflowNode] = relationship(back_populates="node_runs")
    prompt_artifact_version: Mapped[ImagePromptArtifactVersion | None] = relationship(
        back_populates="source_node_run",
        foreign_keys="ImagePromptArtifactVersion.source_node_run_id",
        uselist=False,
    )
    image_generation_record: Mapped[WorkflowImageGenerationRecord | None] = relationship(
        back_populates="node_run",
        cascade="all, delete-orphan",
        uselist=False,
    )


class ImagePromptArtifact(Base, TimestampMixin):
    """workflow 内一个图片类型的提示词稳定身份。"""

    __tablename__ = "image_prompt_artifacts"
    __table_args__ = (
        UniqueConstraint("workflow_id", "image_type_key", name="uq_image_prompt_artifacts_workflow_type"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("product_workflows.id", ondelete="CASCADE", name="fk_image_prompt_artifacts_workflow_id"),
    )
    image_type_key: Mapped[str] = mapped_column(String(80))
    title: Mapped[str] = mapped_column(String(255))

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="prompt_artifacts")
    versions: Mapped[list[ImagePromptArtifactVersion]] = relationship(
        back_populates="artifact",
        cascade="all, delete-orphan",
        order_by="ImagePromptArtifactVersion.version",
    )


class ImagePromptArtifactVersion(Base):
    """提示词制品的不可变版本。"""

    __tablename__ = "image_prompt_artifact_versions"
    __table_args__ = (
        UniqueConstraint("artifact_id", "version", name="uq_image_prompt_artifact_versions_artifact_version"),
        UniqueConstraint(
            "source_node_run_id",
            name="uq_image_prompt_artifact_versions_source_node_run_id",
        ),
        CheckConstraint("version > 0", name="ck_image_prompt_artifact_versions_positive_version"),
        CheckConstraint("schema_version = 1", name="ck_image_prompt_artifact_versions_schema_version"),
        CheckConstraint("length(payload_hash) = 64", name="ck_image_prompt_artifact_versions_payload_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    artifact_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "image_prompt_artifacts.id",
            ondelete="CASCADE",
            name="fk_image_prompt_artifact_versions_artifact_id",
        ),
    )
    version: Mapped[int] = mapped_column(Integer)
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    payload_hash: Mapped[str] = mapped_column(String(64))
    source_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            name="fk_image_prompt_artifact_versions_source_draft_revision_id",
        ),
        nullable=True,
    )
    source_node_run_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_node_runs.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_image_prompt_artifact_versions_source_node_run_id",
        ),
        nullable=True,
    )
    provider_name: Mapped[str | None] = mapped_column(String(80), nullable=True)
    provider_model: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_response_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    artifact: Mapped[ImagePromptArtifact] = relationship(back_populates="versions")
    source_node_run: Mapped[WorkflowNodeRun | None] = relationship(
        back_populates="prompt_artifact_version",
        foreign_keys=[source_node_run_id],
    )
    references: Mapped[list[ImagePromptArtifactVersionReference]] = relationship(
        back_populates="prompt_artifact_version",
        cascade="all, delete-orphan",
        order_by="ImagePromptArtifactVersionReference.position",
    )


class ImagePromptArtifactVersionReference(Base):
    """提示词版本实际绑定的图片证据。"""

    __tablename__ = "image_prompt_artifact_version_references"
    __table_args__ = (
        UniqueConstraint(
            "prompt_artifact_version_id",
            "position",
            name="uq_image_prompt_artifact_version_references_position",
        ),
        UniqueConstraint(
            "prompt_artifact_version_id",
            "asset_id",
            "purpose",
            name="uq_image_prompt_artifact_version_references_asset_purpose",
        ),
        CheckConstraint(
            "position >= 0",
            name="ck_prompt_artifact_version_references_non_negative_position",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    prompt_artifact_version_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "image_prompt_artifact_versions.id",
            ondelete="CASCADE",
            name="fk_image_prompt_artifact_version_references_version_id",
        ),
    )
    asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_image_prompt_artifact_version_references_asset_id",
        ),
    )
    purpose: Mapped[str] = mapped_column(String(120))
    position: Mapped[int] = mapped_column(Integer)

    prompt_artifact_version: Mapped[ImagePromptArtifactVersion] = relationship(back_populates="references")
    asset: Mapped[ProductImageAsset] = relationship()


class VisualException(Base):
    """确认后绑定到 workflow 的结构化视觉例外。"""

    __tablename__ = "visual_exceptions"
    __table_args__ = (
        UniqueConstraint("workflow_id", "exception_key", name="uq_visual_exceptions_workflow_key"),
        CheckConstraint(
            "(scope_type = 'workflow' AND scope_key IS NULL) OR "
            "(scope_type IN ('image_type', 'image_plan') AND scope_key IS NOT NULL)",
            name="ck_visual_exceptions_scope",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("product_workflows.id", ondelete="CASCADE", name="fk_visual_exceptions_workflow_id"),
    )
    source_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            name="fk_visual_exceptions_source_draft_revision_id",
        ),
        nullable=True,
    )
    exception_key: Mapped[str] = mapped_column(String(80))
    scope_type: Mapped[str] = mapped_column(String(40))
    scope_key: Mapped[str | None] = mapped_column(String(80), nullable=True)
    overrides_json: Mapped[list[dict[str, Any]]] = mapped_column(JSON)
    reason: Mapped[str] = mapped_column(Text)
    confirmed_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    workflow: Mapped[ProductWorkflow] = relationship(back_populates="visual_exceptions")


class WorkflowImageGenerationRecord(Base):
    """一次成功 v2 image node run 的不可变生成证据。"""

    __tablename__ = "workflow_image_generation_records"
    __table_args__ = (
        UniqueConstraint(
            "workflow_node_run_id",
            name="uq_workflow_image_generation_records_node_run_id",
        ),
        UniqueConstraint(
            "result_asset_id",
            name="uq_workflow_image_generation_records_result_asset_id",
        ),
        CheckConstraint(
            "length(compiled_prompt_hash) = 64",
            name="ck_workflow_image_generation_records_prompt_hash",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_node_run_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_node_runs.id",
            ondelete="CASCADE",
            name="fk_workflow_image_generation_records_node_run_id",
        ),
    )
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_workflow_image_generation_records_product_id"),
    )
    workflow_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_workflows.id",
            ondelete="CASCADE",
            name="fk_workflow_image_generation_records_workflow_id",
        ),
    )
    node_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_nodes.id",
            ondelete="CASCADE",
            name="fk_workflow_image_generation_records_node_id",
        ),
    )
    result_asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_workflow_image_generation_records_result_asset_id",
        ),
    )
    visual_system_version_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "visual_system_versions.id",
            ondelete="RESTRICT",
            name="fk_workflow_image_generation_records_visual_system_version_id",
        ),
    )
    prompt_artifact_version_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "image_prompt_artifact_versions.id",
            ondelete="RESTRICT",
            name="fk_workflow_image_generation_records_prompt_artifact_version_id",
        ),
    )
    requested_spec_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    effective_parameters_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    actual_media_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    compiled_prompt: Mapped[str] = mapped_column(Text)
    compiled_prompt_hash: Mapped[str] = mapped_column(String(64))
    provider_name: Mapped[str] = mapped_column(String(80))
    provider_model: Mapped[str] = mapped_column(String(255))
    provider_response_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_status: Mapped[str] = mapped_column(String(80))
    provider_request_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    provider_output_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    node_run: Mapped[WorkflowNodeRun] = relationship(back_populates="image_generation_record")
    workflow: Mapped[ProductWorkflow] = relationship(back_populates="image_generation_records")
    node: Mapped[WorkflowNode] = relationship(back_populates="image_generation_records")
    result_asset: Mapped[ProductImageAsset] = relationship(foreign_keys=[result_asset_id])
    visual_system_version: Mapped[VisualSystemVersion] = relationship(
        foreign_keys=[visual_system_version_id]
    )
    prompt_artifact_version: Mapped[ImagePromptArtifactVersion] = relationship(
        foreign_keys=[prompt_artifact_version_id]
    )
    references: Mapped[list[WorkflowImageGenerationReference]] = relationship(
        back_populates="generation_record",
        cascade="all, delete-orphan",
        order_by="WorkflowImageGenerationReference.position",
    )


class WorkflowImageGenerationReference(Base):
    """单次图片生成实际读取的参考资产。"""

    __tablename__ = "workflow_image_generation_references"
    __table_args__ = (
        UniqueConstraint(
            "generation_record_id",
            "position",
            name="uq_workflow_image_generation_references_position",
        ),
        UniqueConstraint(
            "generation_record_id",
            "asset_id",
            "role",
            name="uq_workflow_image_generation_references_asset_role",
        ),
        CheckConstraint(
            "position >= 0",
            name="ck_workflow_image_generation_references_non_negative_position",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    generation_record_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_image_generation_records.id",
            ondelete="CASCADE",
            name="fk_workflow_image_generation_references_record_id",
        ),
    )
    asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_workflow_image_generation_references_asset_id",
        ),
    )
    role: Mapped[str] = mapped_column(String(120))
    label: Mapped[str] = mapped_column(String(255))
    position: Mapped[int] = mapped_column(Integer)

    generation_record: Mapped[WorkflowImageGenerationRecord] = relationship(back_populates="references")
    asset: Mapped[ProductImageAsset] = relationship()


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
        Index("ix_source_assets_source_poster_variant_id", "source_poster_variant_id"),
        Index("ix_source_assets_canonical_asset_id", "canonical_asset_id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    kind: Mapped[SourceAssetKind] = mapped_column(enum_value_column(SourceAssetKind))
    original_filename: Mapped[str] = mapped_column(String(255))
    mime_type: Mapped[str] = mapped_column(String(100))
    storage_path: Mapped[str] = mapped_column(String(500))
    source_poster_variant_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "poster_variants.id",
            ondelete="SET NULL",
            name="fk_source_assets_source_poster_variant_id",
        ),
        nullable=True,
    )
    canonical_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_source_assets_canonical_asset_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(back_populates="source_assets", foreign_keys=[product_id])
    canonical_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[canonical_asset_id])


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

    structured_payload: Mapped[dict | None] = mapped_column(JSON, nullable=True)
    model_structured_payload: Mapped[dict | None] = mapped_column(JSON, nullable=True)

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
    __table_args__ = (Index("ix_poster_variants_canonical_asset_id", "canonical_asset_id"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(String(36), ForeignKey("products.id", ondelete="CASCADE"))
    copy_set_id: Mapped[str] = mapped_column(String(36), ForeignKey("copy_sets.id", ondelete="CASCADE"))
    kind: Mapped[PosterKind] = mapped_column(enum_value_column(PosterKind))
    template_name: Mapped[str] = mapped_column(String(100))
    mime_type: Mapped[str] = mapped_column(String(50), default="image/png")
    storage_path: Mapped[str] = mapped_column(String(500))
    width: Mapped[int] = mapped_column()
    height: Mapped[int] = mapped_column()
    canonical_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_poster_variants_canonical_asset_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(back_populates="poster_variants")
    copy_set: Mapped[CopySet] = relationship(back_populates="poster_variants")
    canonical_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[canonical_asset_id])


class ImageSession(Base, TimestampMixin):
    """连续生图会话，含多轮对话历史与生成结果。"""

    __tablename__ = "image_sessions"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    title: Mapped[str] = mapped_column(String(255))

    assets: Mapped[list[ImageSessionAsset]] = relationship(
        back_populates="session",
        cascade="all, delete-orphan",
    )
    rounds: Mapped[list[ImageSessionRound]] = relationship(
        back_populates="session",
        cascade="all, delete-orphan",
        order_by=lambda: (
            ImageSessionRound.created_at,
            ImageSessionRound.candidate_index,
            ImageSessionRound.id,
        ),
    )
    generation_tasks: Mapped[list[ImageSessionGenerationTask]] = relationship(
        back_populates="session",
        cascade="all, delete-orphan",
        order_by="ImageSessionGenerationTask.created_at",
    )


class ImageSessionAsset(Base):
    __tablename__ = "image_session_assets"
    __table_args__ = (Index("ix_image_session_assets_media_object_id", "media_object_id"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    session_id: Mapped[str] = mapped_column(String(36), ForeignKey("image_sessions.id", ondelete="CASCADE"))
    kind: Mapped[ImageSessionAssetKind] = mapped_column(enum_value_column(ImageSessionAssetKind))
    original_filename: Mapped[str] = mapped_column(String(255))
    mime_type: Mapped[str] = mapped_column(String(100))
    storage_path: Mapped[str] = mapped_column(String(500))
    media_object_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "media_objects.id",
            ondelete="RESTRICT",
            name="fk_image_session_assets_media_object_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    session: Mapped[ImageSession] = relationship(back_populates="assets")
    media_object: Mapped[MediaObject | None] = relationship(back_populates="image_session_assets")
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
    tool_options: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    generation_count: Mapped[int] = mapped_column(Integer, default=1)
    completed_candidates: Mapped[int] = mapped_column(Integer, default=0)
    active_candidate_index: Mapped[int | None] = mapped_column(Integer, nullable=True)
    progress_phase: Mapped[str | None] = mapped_column(String(64), nullable=True)
    progress_updated_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    provider_response_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_response_status: Mapped[str | None] = mapped_column(String(64), nullable=True)
    progress_metadata: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    result_generation_group_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    attempts: Mapped[int] = mapped_column(Integer, default=0)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)

    session: Mapped[ImageSession] = relationship(back_populates="generation_tasks")
    base_asset: Mapped[ImageSessionAsset | None] = relationship(foreign_keys=[base_asset_id])


class ImageGalleryEntry(Base):
    """全局精选画廊条目，引用连续生图生成资产，不复制图片文件。"""

    __tablename__ = "image_gallery_entries"
    __table_args__ = (
        Index("uq_image_gallery_entries_asset_id", "image_session_asset_id", unique=True),
        Index("ix_image_gallery_entries_round_id", "image_session_round_id"),
        Index("ix_image_gallery_entries_created_at", "created_at"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    image_session_asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "image_session_assets.id",
            ondelete="CASCADE",
            name="fk_image_gallery_entries_image_session_asset_id",
        ),
    )
    image_session_round_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "image_session_rounds.id",
            ondelete="SET NULL",
            name="fk_image_gallery_entries_image_session_round_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    asset: Mapped[ImageSessionAsset] = relationship(foreign_keys=[image_session_asset_id])
    round: Mapped[ImageSessionRound | None] = relationship(foreign_keys=[image_session_round_id])
