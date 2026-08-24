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
    AgentCheckpointKind,
    AgentConversationScope,
    AgentConversationStatus,
    AgentExecutionPhase,
    AgentSessionStatus,
    AgentTaskStatus,
    AgentToolMutationStatus,
    AgentTurnStatus,
    AgentWorkflowRunRequestStatus,
    AsyncDispatchStatus,
    GraphActorType,
    GraphArtifactType,
    GraphEdgeDataType,
    GraphEdgeRole,
    GraphHistoryKind,
    GraphNodeType,
    GraphProposalStatus,
    GraphRunScope,
    ImageSessionAssetKind,
    JobStatus,
    LibraryOrganizationDraftStatus,
    LocalImageEditTaskStatus,
    MediaVerificationStatus,
    ProductImageFidelityOutcome,
    ProductImageOriginType,
    WorkflowDraftStatus,
    WorkflowNodeStatus,
    WorkflowRecipeCreationSource,
    WorkflowRecipeKind,
    WorkflowRecipeOrigin,
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
    verification_status: Mapped[MediaVerificationStatus] = mapped_column(enum_value_column(MediaVerificationStatus))
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

    image_assets: Mapped[list[ProductImageAsset]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="ProductImageAsset.product_id",
    )
    delivery_rendition_jobs: Mapped[list[DeliveryRenditionJob]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="DeliveryRenditionJob.product_id",
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
    graphs: Mapped[list[WorkflowGraph]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
    )
    workflow_drafts: Mapped[list[WorkflowDraft]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowDraft.product_id",
    )
    workflow_draft_recipe_seeds: Mapped[list[WorkflowDraftRecipeSeed]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowDraftRecipeSeed.product_id",
    )
    workflow_recipe_applications: Mapped[list[WorkflowRecipeApplication]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowRecipeApplication.product_id",
    )
    workflow_draft_legacy_archive_seeds: Mapped[list[WorkflowDraftLegacyArchiveSeed]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowDraftLegacyArchiveSeed.product_id",
    )
    agent_conversations: Mapped[list[AgentConversation]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="AgentConversation.product_id",
    )
    legacy_workflow_archives: Mapped[list[LegacyWorkflowArchive]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="LegacyWorkflowArchive.product_id",
    )
    legacy_canvas_agent_archives: Mapped[list[LegacyCanvasAgentArchive]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="LegacyCanvasAgentArchive.product_id",
    )
    local_image_edit_tasks: Mapped[list[LocalImageEditTask]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="LocalImageEditTask.product_id",
    )
    fidelity_checks: Mapped[list[ProductImageFidelityCheck]] = relationship(
        back_populates="product",
        cascade="all, delete-orphan",
        foreign_keys="ProductImageFidelityCheck.product_id",
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
        Index("ix_product_image_assets_source_library_asset_id", "source_library_asset_id"),
        Index(
            "uq_product_image_assets_product_session_asset",
            "product_id",
            "source_image_session_asset_id",
            unique=True,
        ),
        Index(
            "uq_product_image_assets_product_library_asset",
            "product_id",
            "source_library_asset_id",
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
    source_library_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "media_library_assets.id",
            ondelete="RESTRICT",
            use_alter=True,
            name="fk_product_image_assets_source_library_asset_id",
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
    source_rendition_jobs: Mapped[list[DeliveryRenditionJob]] = relationship(
        back_populates="source_asset",
        foreign_keys="DeliveryRenditionJob.source_asset_id",
        passive_deletes=True,
    )
    result_rendition_job: Mapped[DeliveryRenditionJob | None] = relationship(
        back_populates="result_asset",
        foreign_keys="DeliveryRenditionJob.result_asset_id",
        uselist=False,
        passive_deletes=True,
    )
    source_image_session_asset: Mapped[ImageSessionAsset | None] = relationship(
        foreign_keys=[source_image_session_asset_id]
    )
    source_library_asset: Mapped[MediaLibraryAsset | None] = relationship(
        foreign_keys=[source_library_asset_id],
    )
    legacy_archive_references: Mapped[list[LegacyWorkflowArchiveAsset]] = relationship(
        back_populates="asset",
        foreign_keys="LegacyWorkflowArchiveAsset.product_image_asset_id",
        passive_deletes=True,
    )
    fidelity_checks: Mapped[list[ProductImageFidelityCheck]] = relationship(
        back_populates="asset",
        foreign_keys="ProductImageFidelityCheck.asset_id",
        passive_deletes=True,
    )


class ProductImageFidelityCheck(Base):
    """商品图片的不可变、版本化人工保真检查记录。"""

    __tablename__ = "product_image_fidelity_checks"
    __table_args__ = (
        UniqueConstraint(
            "asset_id",
            "version",
            name="uq_product_image_fidelity_checks_asset_version",
        ),
        UniqueConstraint(
            "asset_id",
            "idempotency_key",
            name="uq_product_image_fidelity_checks_asset_idempotency",
        ),
        CheckConstraint("version >= 1", name="ck_product_image_fidelity_checks_positive_version"),
        CheckConstraint(
            "shape_fidelity IN ('pass', 'fail', 'not_applicable')",
            name="ck_product_image_fidelity_checks_shape_outcome",
        ),
        CheckConstraint(
            "color_material_fidelity IN ('pass', 'fail', 'not_applicable')",
            name="ck_product_image_fidelity_checks_color_outcome",
        ),
        CheckConstraint(
            "logo_text_legibility IN ('pass', 'fail', 'not_applicable')",
            name="ck_product_image_fidelity_checks_logo_outcome",
        ),
        CheckConstraint(
            "text_policy_compliance IN ('pass', 'fail', 'not_applicable')",
            name="ck_product_image_fidelity_checks_text_policy_outcome",
        ),
        CheckConstraint(
            "notes IS NULL OR length(notes) <= 4000",
            name="ck_product_image_fidelity_checks_notes_length",
        ),
        CheckConstraint(
            "checked_by = 'administrator'",
            name="ck_product_image_fidelity_checks_checked_by_authority",
        ),
        CheckConstraint(
            "length(idempotency_key) > 0",
            name="ck_product_image_fidelity_checks_idempotency_nonempty",
        ),
        CheckConstraint(
            "length(request_hash) = 64",
            name="ck_product_image_fidelity_checks_request_hash",
        ),
        Index(
            "ix_product_image_fidelity_checks_asset_created",
            "asset_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "products.id",
            ondelete="CASCADE",
            name="fk_product_image_fidelity_checks_product_id",
        ),
    )
    asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_product_image_fidelity_checks_asset_id",
        ),
    )
    version: Mapped[int] = mapped_column(Integer)
    shape_fidelity: Mapped[ProductImageFidelityOutcome] = mapped_column(String(20))
    color_material_fidelity: Mapped[ProductImageFidelityOutcome] = mapped_column(String(20))
    logo_text_legibility: Mapped[ProductImageFidelityOutcome] = mapped_column(String(20))
    text_policy_compliance: Mapped[ProductImageFidelityOutcome] = mapped_column(String(20))
    notes: Mapped[str | None] = mapped_column(Text, nullable=True)
    checked_by: Mapped[str] = mapped_column(String(80), default="administrator")
    idempotency_key: Mapped[str] = mapped_column(String(120))
    request_hash: Mapped[str] = mapped_column(String(64))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(back_populates="fidelity_checks", foreign_keys=[product_id])
    asset: Mapped[ProductImageAsset] = relationship(back_populates="fidelity_checks", foreign_keys=[asset_id])


class LegacyWorkflowArchive(Base):
    """不可变的 v1 workflow 历史快照，不具备执行语义。"""

    __tablename__ = "legacy_workflow_archives"
    __table_args__ = (
        UniqueConstraint(
            "source_profile",
            "legacy_workflow_id",
            name="uq_legacy_workflow_archives_source_id",
        ),
        CheckConstraint(
            "archive_schema_version = 1",
            name="ck_legacy_workflow_archives_schema_version",
        ),
        CheckConstraint(
            "length(source_fingerprint_sha256) = 64",
            name="ck_legacy_workflow_archives_source_hash",
        ),
        CheckConstraint(
            "length(payload_sha256) = 64",
            name="ck_legacy_workflow_archives_payload_hash",
        ),
        CheckConstraint(
            "node_count >= 0 AND edge_count >= 0 AND run_count >= 0 AND node_run_count >= 0 AND asset_count >= 0",
            name="ck_legacy_workflow_archives_counts",
        ),
        Index(
            "ix_legacy_workflow_archives_product_created",
            "product_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    source_profile: Mapped[str] = mapped_column(String(80))
    legacy_workflow_id: Mapped[str] = mapped_column(String(36))
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "products.id",
            ondelete="CASCADE",
            name="fk_legacy_workflow_archives_product_id",
        ),
    )
    source_title: Mapped[str] = mapped_column(String(255))
    source_updated_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    archive_schema_version: Mapped[int] = mapped_column(Integer, default=1)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    source_fingerprint_sha256: Mapped[str] = mapped_column(String(64))
    payload_sha256: Mapped[str] = mapped_column(String(64))
    node_count: Mapped[int] = mapped_column(Integer)
    edge_count: Mapped[int] = mapped_column(Integer)
    run_count: Mapped[int] = mapped_column(Integer)
    node_run_count: Mapped[int] = mapped_column(Integer)
    asset_count: Mapped[int] = mapped_column(Integer)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(
        back_populates="legacy_workflow_archives",
        foreign_keys=[product_id],
    )
    assets: Mapped[list[LegacyWorkflowArchiveAsset]] = relationship(
        back_populates="archive",
        cascade="all, delete-orphan",
        order_by="LegacyWorkflowArchiveAsset.id",
    )


class LegacyWorkflowArchiveAsset(Base):
    """归档快照对 canonical 商品图片的显式保留关系。"""

    __tablename__ = "legacy_workflow_archive_assets"
    __table_args__ = (
        UniqueConstraint(
            "archive_id",
            "product_image_asset_id",
            "role",
            "legacy_source_type",
            "legacy_source_id",
            name="uq_legacy_workflow_archive_assets_identity",
        ),
        Index(
            "ix_legacy_workflow_archive_assets_asset_id",
            "product_image_asset_id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    archive_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "legacy_workflow_archives.id",
            ondelete="CASCADE",
            name="fk_legacy_workflow_archive_assets_archive_id",
        ),
    )
    product_image_asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_legacy_workflow_archive_assets_asset_id",
        ),
    )
    role: Mapped[str] = mapped_column(String(80))
    legacy_source_type: Mapped[str] = mapped_column(String(80))
    legacy_source_id: Mapped[str] = mapped_column(String(80))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    archive: Mapped[LegacyWorkflowArchive] = relationship(back_populates="assets")
    asset: Mapped[ProductImageAsset] = relationship(back_populates="legacy_archive_references")


class LegacyUserTemplateArchive(Base):
    """旧用户模板的不可应用只读快照或损坏记录。"""

    __tablename__ = "legacy_user_template_archives"
    __table_args__ = (
        UniqueConstraint(
            "source_profile",
            "legacy_template_id",
            name="uq_legacy_user_template_archives_source_id",
        ),
        CheckConstraint(
            "archive_schema_version = 1",
            name="ck_legacy_user_template_archives_schema_version",
        ),
        CheckConstraint(
            "archive_status IN ('archived', 'damaged')",
            name="ck_legacy_user_template_archives_status",
        ),
        CheckConstraint(
            "length(source_fingerprint_sha256) = 64",
            name="ck_legacy_user_template_archives_source_hash",
        ),
        CheckConstraint(
            "length(payload_sha256) = 64",
            name="ck_legacy_user_template_archives_payload_hash",
        ),
        Index("ix_legacy_user_template_archives_created", "created_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    source_profile: Mapped[str] = mapped_column(String(80))
    legacy_template_id: Mapped[str] = mapped_column(String(36))
    legacy_key: Mapped[str] = mapped_column(String(80))
    title: Mapped[str] = mapped_column(String(255))
    description: Mapped[str | None] = mapped_column(Text, nullable=True)
    archive_status: Mapped[str] = mapped_column(String(40))
    archive_schema_version: Mapped[int] = mapped_column(Integer, default=1)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    diagnostics_json: Mapped[list[dict[str, Any]]] = mapped_column(JSON, default=list)
    source_fingerprint_sha256: Mapped[str] = mapped_column(String(64))
    payload_sha256: Mapped[str] = mapped_column(String(64))
    source_updated_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class LegacyCanvasAgentArchive(Base):
    """旧 Canvas Agent thread 的有界用户历史快照。"""

    __tablename__ = "legacy_canvas_agent_archives"
    __table_args__ = (
        UniqueConstraint(
            "source_profile",
            "legacy_thread_id",
            name="uq_legacy_canvas_agent_archives_source_id",
        ),
        CheckConstraint(
            "archive_schema_version = 1",
            name="ck_legacy_canvas_agent_archives_schema_version",
        ),
        CheckConstraint(
            "length(source_fingerprint_sha256) = 64",
            name="ck_legacy_canvas_agent_archives_source_hash",
        ),
        CheckConstraint(
            "length(payload_sha256) = 64",
            name="ck_legacy_canvas_agent_archives_payload_hash",
        ),
        CheckConstraint(
            "message_count >= 0 AND run_count >= 0 AND tool_event_count >= 0 "
            "AND plan_count >= 0 AND task_plan_count >= 0 AND timeline_event_count >= 0 "
            "AND visible_event_count >= 0 AND technical_event_count >= 0",
            name="ck_legacy_canvas_agent_archives_counts",
        ),
        CheckConstraint(
            "visible_event_count + technical_event_count = timeline_event_count",
            name="ck_legacy_canvas_agent_archives_event_total",
        ),
        Index(
            "ix_legacy_canvas_agent_archives_product_created",
            "product_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    source_profile: Mapped[str] = mapped_column(String(80))
    legacy_thread_id: Mapped[str] = mapped_column(String(36))
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "products.id",
            ondelete="CASCADE",
            name="fk_legacy_canvas_agent_archives_product_id",
        ),
    )
    title: Mapped[str] = mapped_column(String(255))
    source_status: Mapped[str] = mapped_column(String(40))
    source_updated_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    archive_schema_version: Mapped[int] = mapped_column(Integer, default=1)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    source_fingerprint_sha256: Mapped[str] = mapped_column(String(64))
    payload_sha256: Mapped[str] = mapped_column(String(64))
    message_count: Mapped[int] = mapped_column(Integer)
    run_count: Mapped[int] = mapped_column(Integer)
    tool_event_count: Mapped[int] = mapped_column(Integer)
    plan_count: Mapped[int] = mapped_column(Integer)
    task_plan_count: Mapped[int] = mapped_column(Integer)
    timeline_event_count: Mapped[int] = mapped_column(Integer)
    visible_event_count: Mapped[int] = mapped_column(Integer)
    technical_event_count: Mapped[int] = mapped_column(Integer)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(
        back_populates="legacy_canvas_agent_archives",
        foreign_keys=[product_id],
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
    __table_args__ = (
        CheckConstraint(
            "(intake_schema_version IS NULL AND intake_json IS NULL) OR "
            "(intake_schema_version = 1 AND intake_json IS NOT NULL)",
            name="ck_workflow_drafts_intake_pair",
        ),
        Index("ix_workflow_drafts_product_status", "product_id", "status"),
    )

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
    intake_schema_version: Mapped[int | None] = mapped_column(Integer, nullable=True)
    intake_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)

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
    agent_conversation: Mapped[AgentConversation | None] = relationship(
        back_populates="workflow_draft",
        foreign_keys="AgentConversation.workflow_draft_id",
        uselist=False,
    )
    recipe_seed: Mapped[WorkflowDraftRecipeSeed | None] = relationship(
        back_populates="workflow_draft",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowDraftRecipeSeed.workflow_draft_id",
        uselist=False,
    )
    legacy_archive_seed: Mapped[WorkflowDraftLegacyArchiveSeed | None] = relationship(
        back_populates="workflow_draft",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowDraftLegacyArchiveSeed.workflow_draft_id",
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
    visual_system_version: Mapped[VisualSystemVersion | None] = relationship(foreign_keys=[visual_system_version_id])
    agent_turn_projection: Mapped[AgentTurnProjection | None] = relationship(
        back_populates="workflow_draft_revision",
        foreign_keys="AgentTurnProjection.workflow_draft_revision_id",
        uselist=False,
    )


class LibraryOrganizationDraft(Base, TimestampMixin):
    """全局素材整理 Draft 的稳定身份和当前状态。"""

    __tablename__ = "library_organization_drafts"
    __table_args__ = (
        UniqueConstraint("conversation_id", name="uq_library_organization_drafts_conversation_id"),
        CheckConstraint(
            "(confirmation_idempotency_key IS NULL AND confirmation_request_hash IS NULL) OR "
            "(confirmation_idempotency_key IS NOT NULL AND length(confirmation_idempotency_key) > 0 "
            "AND confirmation_request_hash IS NOT NULL AND length(confirmation_request_hash) = 64)",
            name="ck_library_organization_drafts_confirmation_pair",
        ),
        Index("ix_library_organization_drafts_status_updated", "status", "updated_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    conversation_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_conversations.id",
            ondelete="CASCADE",
            name="fk_library_organization_drafts_conversation_id",
        ),
    )
    status: Mapped[LibraryOrganizationDraftStatus] = mapped_column(
        enum_value_column(LibraryOrganizationDraftStatus),
        default=LibraryOrganizationDraftStatus.AWAITING_CONFIRMATION,
    )
    current_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "library_organization_draft_revisions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_library_organization_drafts_current_revision_id",
        ),
        nullable=True,
    )
    confirmed_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "library_organization_draft_revisions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_library_organization_drafts_confirmed_revision_id",
        ),
        nullable=True,
    )
    confirmation_idempotency_key: Mapped[str | None] = mapped_column(String(200), nullable=True)
    confirmation_request_hash: Mapped[str | None] = mapped_column(String(64), nullable=True)
    confirmation_result_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    confirmed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    conversation: Mapped[AgentConversation] = relationship(
        back_populates="library_organization_draft",
        foreign_keys=[conversation_id],
    )
    revisions: Mapped[list[LibraryOrganizationDraftRevision]] = relationship(
        back_populates="draft",
        cascade="all, delete-orphan",
        foreign_keys="LibraryOrganizationDraftRevision.draft_id",
        order_by="LibraryOrganizationDraftRevision.version",
    )
    current_revision: Mapped[LibraryOrganizationDraftRevision | None] = relationship(
        foreign_keys=[current_revision_id],
        post_update=True,
    )
    confirmed_revision: Mapped[LibraryOrganizationDraftRevision | None] = relationship(
        foreign_keys=[confirmed_revision_id],
        post_update=True,
    )


class LibraryOrganizationDraftRevision(Base):
    """全局素材整理 Draft 的 append-only artifact 快照。"""

    __tablename__ = "library_organization_draft_revisions"
    __table_args__ = (
        UniqueConstraint("draft_id", "version", name="uq_library_organization_draft_revisions_draft_version"),
        UniqueConstraint(
            "draft_id",
            "source_turn_id",
            "source_artifact_step_id",
            name="uq_library_organization_draft_revisions_artifact_origin",
        ),
        CheckConstraint("version > 0", name="ck_library_organization_draft_revisions_positive_version"),
        CheckConstraint("schema_version = 1", name="ck_library_organization_draft_revisions_schema_version"),
        CheckConstraint("length(payload_hash) = 64", name="ck_library_organization_draft_revisions_payload_hash"),
        CheckConstraint(
            "(source_turn_id IS NULL AND source_artifact_step_id IS NULL) OR "
            "(source_turn_id IS NOT NULL AND source_artifact_step_id IS NOT NULL)",
            name="ck_library_organization_draft_revisions_artifact_origin_pair",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    draft_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "library_organization_drafts.id",
            ondelete="CASCADE",
            name="fk_library_organization_draft_revisions_draft_id",
        ),
    )
    version: Mapped[int] = mapped_column(Integer)
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    payload_hash: Mapped[str] = mapped_column(String(64))
    source_turn_id: Mapped[str | None] = mapped_column(String(120), nullable=True)
    source_artifact_step_id: Mapped[str | None] = mapped_column(String(120), nullable=True)
    confirmed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    draft: Mapped[LibraryOrganizationDraft] = relationship(
        back_populates="revisions",
        foreign_keys=[draft_id],
    )
    agent_turn_projection: Mapped[AgentTurnProjection | None] = relationship(
        back_populates="library_organization_draft_revision",
        foreign_keys="AgentTurnProjection.library_organization_draft_revision_id",
        uselist=False,
    )


class AgentSession(Base, TimestampMixin):
    """跨商品持续存在的 Agent 对话容器。"""

    __tablename__ = "agent_sessions"
    __table_args__ = (
        Index("ix_agent_sessions_status_updated", "status", "updated_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    title: Mapped[str] = mapped_column(String(160), nullable=False)
    summary: Mapped[str | None] = mapped_column(Text, nullable=True)
    status: Mapped[AgentSessionStatus] = mapped_column(
        enum_value_column(AgentSessionStatus),
        default=AgentSessionStatus.ACTIVE,
    )
    archived_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    conversations: Mapped[list[AgentConversation]] = relationship(
        back_populates="session",
        order_by="AgentConversation.updated_at.desc(), AgentConversation.id.desc()",
    )
    tasks: Mapped[list[AgentTask]] = relationship(
        back_populates="session",
        cascade="all, delete-orphan",
        order_by="AgentTask.updated_at.desc(), AgentTask.id.desc()",
    )


class AgentTask(Base, TimestampMixin):
    """一个可跨页面持续运行、可与同一 Session 中其他任务并行的业务目标。"""

    __tablename__ = "agent_tasks"
    __table_args__ = (
        UniqueConstraint("harness_run_id", name="uq_agent_tasks_harness_run_id"),
        Index("ix_agent_tasks_session_status_updated", "session_id", "status", "updated_at", "id"),
        Index("ix_agent_tasks_status_updated", "status", "updated_at", "id"),
        Index("ix_agent_tasks_conversation_updated", "conversation_id", "updated_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    session_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("agent_sessions.id", ondelete="CASCADE", name="fk_agent_tasks_session_id"),
    )
    conversation_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("agent_conversations.id", ondelete="SET NULL", name="fk_agent_tasks_conversation_id"),
        nullable=True,
    )
    product_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="SET NULL", name="fk_agent_tasks_product_id"),
        nullable=True,
    )
    workflow_draft_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("workflow_drafts.id", ondelete="SET NULL", name="fk_agent_tasks_workflow_draft_id"),
        nullable=True,
    )
    harness_run_id: Mapped[str] = mapped_column(String(120), nullable=False)
    title: Mapped[str] = mapped_column(String(160), nullable=False)
    goal: Mapped[str] = mapped_column(Text, nullable=False)
    summary: Mapped[str | None] = mapped_column(Text, nullable=True)
    status: Mapped[AgentTaskStatus] = mapped_column(
        enum_value_column(AgentTaskStatus),
        default=AgentTaskStatus.QUEUED,
    )
    waiting_reason: Mapped[str | None] = mapped_column(String(160), nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    current_turn_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    canceled_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    session: Mapped[AgentSession] = relationship(back_populates="tasks")
    conversation: Mapped[AgentConversation | None] = relationship(foreign_keys=[conversation_id])
    turns: Mapped[list[AgentTurnProjection]] = relationship(
        back_populates="task",
        order_by="AgentTurnProjection.created_at",
    )
    workflow_run_requests: Mapped[list[AgentWorkflowRunRequest]] = relationship(
        back_populates="task",
        order_by="AgentWorkflowRunRequest.created_at.desc(), AgentWorkflowRunRequest.id.desc()",
    )


class AgentPageContextSnapshot(Base):
    """一次 Turn 发送时浏览器提交的有界页面事实快照。"""

    __tablename__ = "agent_page_context_snapshots"
    __table_args__ = (
        CheckConstraint("length(digest) = 64", name="ck_agent_page_context_snapshots_digest"),
        Index("ix_agent_page_context_snapshots_task_created", "task_id", "created_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    task_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("agent_tasks.id", ondelete="CASCADE", name="fk_agent_page_context_snapshots_task_id"),
        nullable=True,
    )
    turn_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    route: Mapped[str] = mapped_column(String(512), nullable=False)
    page_type: Mapped[str] = mapped_column(String(80), nullable=False)
    product_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    workflow_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    selected_asset_ids_json: Mapped[list[str]] = mapped_column(JSON, default=list)
    visible_asset_ids_json: Mapped[list[str]] = mapped_column(JSON, default=list)
    filters_json: Mapped[dict[str, str]] = mapped_column(JSON, default=dict)
    workflow_revision: Mapped[int | None] = mapped_column(Integer, nullable=True)
    library_revision: Mapped[int | None] = mapped_column(Integer, nullable=True)
    digest: Mapped[str] = mapped_column(String(64), nullable=False)
    captured_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    task: Mapped[AgentTask | None] = relationship()


class AgentConversation(Base, TimestampMixin):
    """ProductFlow 对一个隔离 agent-harness run 的业务作用域绑定。"""

    __tablename__ = "agent_conversations"
    __table_args__ = (
        UniqueConstraint("workflow_draft_id", name="uq_agent_conversations_workflow_draft_id"),
        UniqueConstraint("harness_run_id", name="uq_agent_conversations_harness_run_id"),
        UniqueConstraint(
            "creation_idempotency_key",
            name="uq_agent_conversations_creation_idempotency_key",
        ),
        CheckConstraint(
            "(creation_idempotency_key IS NULL AND creation_request_hash IS NULL) OR "
            "(creation_idempotency_key IS NOT NULL AND length(creation_idempotency_key) > 0 "
            "AND creation_request_hash IS NOT NULL AND length(creation_request_hash) = 64)",
            name="ck_agent_conversations_creation_idempotency_pair",
        ),
        CheckConstraint(
            "(intake_idempotency_key IS NULL AND intake_request_hash IS NULL) OR "
            "(intake_idempotency_key IS NOT NULL AND length(intake_idempotency_key) > 0 "
            "AND intake_request_hash IS NOT NULL AND length(intake_request_hash) = 64)",
            name="ck_agent_conversations_intake_idempotency_pair",
        ),
        CheckConstraint(
            "(scope_type = 'product_workflow' AND product_id IS NOT NULL AND workflow_draft_id IS NOT NULL) OR "
            "(scope_type = 'global' AND product_id IS NULL AND workflow_draft_id IS NULL)",
            name="ck_agent_conversations_scope_fields",
        ),
        CheckConstraint(
            "scope_type IN ('product_workflow', 'global')",
            name="ck_agent_conversations_scope_type",
        ),
        Index(
            "ux_agent_conversations_session_global",
            "session_id",
            unique=True,
            postgresql_where=text("scope_type = 'global'"),
            sqlite_where=text("scope_type = 'global'"),
        ),
        Index("ix_agent_conversations_product_status", "product_id", "status"),
        Index("ix_agent_conversations_session_updated", "session_id", "updated_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    scope_type: Mapped[AgentConversationScope] = mapped_column(
        enum_value_column(AgentConversationScope),
        default=AgentConversationScope.PRODUCT_WORKFLOW,
        server_default=AgentConversationScope.PRODUCT_WORKFLOW.value,
        nullable=False,
    )
    session_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("agent_sessions.id", ondelete="SET NULL", name="fk_agent_conversations_session_id"),
        nullable=True,
    )
    product_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_agent_conversations_product_id"),
        nullable=True,
    )
    workflow_draft_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_drafts.id",
            ondelete="CASCADE",
            name="fk_agent_conversations_workflow_draft_id",
        ),
        nullable=True,
    )
    harness_run_id: Mapped[str] = mapped_column(String(120))
    creation_idempotency_key: Mapped[str | None] = mapped_column(String(200), nullable=True)
    creation_request_hash: Mapped[str | None] = mapped_column(String(64), nullable=True)
    intake_idempotency_key: Mapped[str | None] = mapped_column(String(200), nullable=True)
    intake_request_hash: Mapped[str | None] = mapped_column(String(64), nullable=True)
    status: Mapped[AgentConversationStatus] = mapped_column(
        enum_value_column(AgentConversationStatus),
        default=AgentConversationStatus.COLLECTING,
    )

    session: Mapped[AgentSession | None] = relationship(back_populates="conversations")
    product: Mapped[Product | None] = relationship(
        back_populates="agent_conversations",
        foreign_keys=[product_id],
    )
    workflow_draft: Mapped[WorkflowDraft | None] = relationship(
        back_populates="agent_conversation",
        foreign_keys=[workflow_draft_id],
    )
    library_organization_draft: Mapped[LibraryOrganizationDraft | None] = relationship(
        back_populates="conversation",
        foreign_keys="LibraryOrganizationDraft.conversation_id",
        uselist=False,
        cascade="all, delete-orphan",
        single_parent=True,
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
    workflow_run_requests: Mapped[list[AgentWorkflowRunRequest]] = relationship(
        back_populates="conversation",
        cascade="all, delete-orphan",
        order_by="AgentWorkflowRunRequest.created_at.desc(), AgentWorkflowRunRequest.id.desc()",
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
        UniqueConstraint(
            "library_organization_draft_revision_id",
            name="uq_agent_turn_proj_library_org_draft_rev_id",
        ),
        UniqueConstraint(
            "workflow_run_request_id",
            name="uq_agent_turn_projections_workflow_run_request_id",
        ),
        CheckConstraint("length(request_hash) = 64", name="ck_agent_turn_projections_request_hash"),
        Index(
            "ix_agent_turn_projections_conversation_created",
            "conversation_id",
            "created_at",
            "id",
        ),
        Index("ix_agent_turn_projections_task_created", "task_id", "created_at", "id"),
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
    task_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("agent_tasks.id", ondelete="SET NULL", name="fk_agent_turn_projections_task_id"),
        nullable=True,
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
    question_answer_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    continuation_turn_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    tool_steps_json: Mapped[list[dict[str, Any]]] = mapped_column(JSON, default=list)
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
    library_organization_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "library_organization_draft_revisions.id",
            ondelete="SET NULL",
            name="fk_agent_turn_proj_library_org_draft_rev_id",
        ),
        nullable=True,
    )
    workflow_run_request_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "agent_workflow_run_requests.id",
            ondelete="SET NULL",
            name="fk_agent_turn_projections_workflow_run_request_id",
        ),
        nullable=True,
    )
    page_context_snapshot_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "agent_page_context_snapshots.id",
            ondelete="SET NULL",
            name="fk_agent_turn_projections_page_context_snapshot_id",
        ),
        nullable=True,
    )
    sync_error: Mapped[str | None] = mapped_column(Text, nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    conversation: Mapped[AgentConversation] = relationship(back_populates="turns")
    task: Mapped[AgentTask | None] = relationship(back_populates="turns", foreign_keys=[task_id])
    page_context_snapshot: Mapped[AgentPageContextSnapshot | None] = relationship(
        foreign_keys=[page_context_snapshot_id],
    )
    workflow_draft_revision: Mapped[WorkflowDraftRevision | None] = relationship(
        back_populates="agent_turn_projection",
        foreign_keys=[workflow_draft_revision_id],
    )
    library_organization_draft_revision: Mapped[LibraryOrganizationDraftRevision | None] = relationship(
        back_populates="agent_turn_projection",
        foreign_keys=[library_organization_draft_revision_id],
    )
    workflow_run_request: Mapped[AgentWorkflowRunRequest | None] = relationship(
        back_populates="turn_projection",
        foreign_keys="AgentTurnProjection.workflow_run_request_id",
        uselist=False,
    )
    execution: Mapped[AgentTurnExecution | None] = relationship(
        back_populates="turn_projection",
        cascade="all, delete-orphan",
        uselist=False,
    )
    checkpoints: Mapped[list[AgentTurnCheckpoint]] = relationship(
        back_populates="turn_projection",
        order_by="AgentTurnCheckpoint.sequence",
    )
    events: Mapped[list[AgentTurnEvent]] = relationship(
        back_populates="turn_projection",
        cascade="all, delete-orphan",
        order_by="AgentTurnEvent.sequence",
    )
    effect_reconciliations: Mapped[list[AgentTurnEffectReconciliation]] = relationship(
        back_populates="turn_projection",
        cascade="all, delete-orphan",
        order_by="AgentTurnEffectReconciliation.created_at",
    )


class AgentTurnExecution(Base, TimestampMixin):
    """跨进程 Agent Turn claim、lease 和执行阶段的持久记录。"""

    __tablename__ = "agent_turn_executions"
    __table_args__ = (
        UniqueConstraint("turn_projection_id", name="uq_agent_turn_executions_projection_id"),
        CheckConstraint("attempt >= 0", name="ck_agent_turn_executions_non_negative_attempt"),
        CheckConstraint("fencing_token >= 0", name="ck_agent_turn_executions_non_negative_fencing"),
        Index("ix_agent_turn_executions_lease", "lease_expires_at", "id"),
        Index("ix_agent_turn_executions_owner", "owner_id", "lease_expires_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    turn_projection_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_turn_projections.id",
            ondelete="CASCADE",
            name="fk_agent_turn_executions_turn_projection_id",
        ),
    )
    harness_turn_id: Mapped[str] = mapped_column(String(120), nullable=False)
    owner_id: Mapped[str | None] = mapped_column(String(120), nullable=True)
    lease_token: Mapped[str | None] = mapped_column(String(36), nullable=True)
    lease_expires_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    attempt: Mapped[int] = mapped_column(Integer, default=0)
    fencing_token: Mapped[int] = mapped_column(Integer, default=0)
    phase: Mapped[AgentExecutionPhase] = mapped_column(
        enum_value_column(AgentExecutionPhase),
        default=AgentExecutionPhase.CLAIMED,
    )
    last_heartbeat_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    released_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    last_checkpoint_sequence: Mapped[int] = mapped_column(Integer, default=0)
    last_checkpoint_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    turn_projection: Mapped[AgentTurnProjection] = relationship(back_populates="execution")
    checkpoints: Mapped[list[AgentTurnCheckpoint]] = relationship(
        back_populates="execution",
        cascade="all, delete-orphan",
        order_by="AgentTurnCheckpoint.sequence",
    )


class AgentTurnCheckpoint(Base):
    """Agent Turn 的有界、可审计语义 checkpoint。"""

    __tablename__ = "agent_turn_checkpoints"
    __table_args__ = (
        UniqueConstraint(
            "execution_id",
            "attempt",
            "sequence",
            name="uq_agent_turn_checkpoints_execution_attempt_sequence",
        ),
        CheckConstraint("attempt > 0", name="ck_agent_turn_checkpoints_positive_attempt"),
        CheckConstraint("sequence > 0", name="ck_agent_turn_checkpoints_positive_sequence"),
        CheckConstraint("fencing_token > 0", name="ck_agent_turn_checkpoints_positive_fencing"),
        Index("ix_agent_turn_checkpoints_projection_sequence", "turn_projection_id", "sequence"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    turn_projection_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_turn_projections.id",
            ondelete="CASCADE",
            name="fk_agent_turn_checkpoints_turn_projection_id",
        ),
    )
    execution_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_turn_executions.id",
            ondelete="CASCADE",
            name="fk_agent_turn_checkpoints_execution_id",
        ),
    )
    attempt: Mapped[int] = mapped_column(Integer)
    fencing_token: Mapped[int] = mapped_column(Integer)
    sequence: Mapped[int] = mapped_column(Integer)
    kind: Mapped[AgentCheckpointKind] = mapped_column(enum_value_column(AgentCheckpointKind))
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    turn_projection: Mapped[AgentTurnProjection] = relationship(back_populates="checkpoints")
    execution: Mapped[AgentTurnExecution] = relationship(back_populates="checkpoints")


class AgentTurnEvent(Base):
    """跨实例 Agent Turn 事件日志；只保存前端需要的有界事件投影。"""

    __tablename__ = "agent_turn_events"
    __table_args__ = (
        UniqueConstraint(
            "turn_projection_id",
            "sequence",
            name="uq_agent_turn_events_projection_sequence",
        ),
        CheckConstraint("schema_version = 1", name="ck_agent_turn_events_schema_version"),
        CheckConstraint("sequence > 0", name="ck_agent_turn_events_positive_sequence"),
        CheckConstraint("attempt IS NULL OR attempt > 0", name="ck_agent_turn_events_attempt"),
        CheckConstraint("fencing_token IS NULL OR fencing_token > 0", name="ck_agent_turn_events_fencing"),
        Index("ix_agent_turn_events_projection_sequence", "turn_projection_id", "sequence"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    turn_projection_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_turn_projections.id",
            ondelete="CASCADE",
            name="fk_agent_turn_events_turn_projection_id",
        ),
    )
    execution_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "agent_turn_executions.id",
            ondelete="SET NULL",
            name="fk_agent_turn_events_execution_id",
        ),
        nullable=True,
    )
    run_id: Mapped[str] = mapped_column(String(120))
    turn_id: Mapped[str] = mapped_column(String(120))
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    sequence: Mapped[int] = mapped_column(Integer)
    attempt: Mapped[int | None] = mapped_column(Integer, nullable=True)
    fencing_token: Mapped[int | None] = mapped_column(Integer, nullable=True)
    kind: Mapped[str] = mapped_column(String(120))
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    turn_projection: Mapped[AgentTurnProjection] = relationship(back_populates="events")
    execution: Mapped[AgentTurnExecution | None] = relationship()


class AgentTurnEffectReconciliation(Base, TimestampMixin):
    """ProductFlow 对 Agent 副作用未知结果的持久对账裁决。"""

    __tablename__ = "agent_turn_effect_reconciliations"
    __table_args__ = (
        UniqueConstraint(
            "turn_projection_id",
            "tool_call_id",
            name="uq_agent_turn_effect_reconciliations_projection_tool",
        ),
        CheckConstraint(
            "effect_result IN ('applied', 'failed', 'unknown')",
            name="ck_agent_turn_effect_reconciliations_effect_result",
        ),
        CheckConstraint(
            "reconciliation_state IN ('applied', 'not_applied', 'conflict', 'unknown')",
            name="ck_agent_turn_effect_reconciliations_state",
        ),
        Index(
            "ix_agent_turn_effect_reconciliations_projection_created",
            "turn_projection_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    turn_projection_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_turn_projections.id",
            ondelete="CASCADE",
            name="fk_agent_turn_effect_reconciliations_turn_projection_id",
        ),
    )
    tool_call_id: Mapped[str] = mapped_column(String(120))
    tool_name: Mapped[str] = mapped_column(String(120))
    idempotency_key: Mapped[str] = mapped_column(String(200))
    effect_result: Mapped[str] = mapped_column(String(20))
    reconciliation_state: Mapped[str] = mapped_column(String(20))
    result_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    detail: Mapped[str | None] = mapped_column(Text, nullable=True)

    turn_projection: Mapped[AgentTurnProjection] = relationship(back_populates="effect_reconciliations")


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


class AgentWorkflowRunRequest(Base, TimestampMixin):
    """Agent 请求人工确认后执行一次 schema-v3 graph 运行的记录。"""

    __tablename__ = "agent_workflow_run_requests"
    __table_args__ = (
        UniqueConstraint(
            "conversation_id",
            "idempotency_key",
            name="uq_agent_workflow_run_requests_conversation_key",
        ),
        CheckConstraint(
            "expected_workflow_revision > 0",
            name="ck_agent_workflow_run_requests_positive_revision",
        ),
        CheckConstraint(
            "graph_id IS NOT NULL",
            name="ck_agent_workflow_run_requests_graph_required",
        ),
        CheckConstraint(
            "length(request_hash) = 64",
            name="ck_agent_workflow_run_requests_request_hash",
        ),
        CheckConstraint(
            "length(source_step_id) > 0",
            name="ck_agent_workflow_run_requests_source_step_id",
        ),
        Index(
            "ix_agent_workflow_run_requests_conversation_status_updated",
            "conversation_id",
            "status",
            "updated_at",
            "id",
        ),
        Index("ix_agent_workflow_run_requests_task_status_updated", "task_id", "status", "updated_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    conversation_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "agent_conversations.id",
            ondelete="CASCADE",
            name="fk_agent_workflow_run_requests_conversation_id",
        ),
    )
    task_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("agent_tasks.id", ondelete="SET NULL", name="fk_agent_workflow_run_requests_task_id"),
        nullable=True,
    )
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_agent_workflow_run_requests_product_id"),
    )
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_agent_workflow_run_requests_graph_id"),
    )
    source_graph_run_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_runs.id",
            ondelete="RESTRICT",
            name="fk_agent_workflow_run_requests_source_graph_run_id",
        ),
        nullable=True,
    )
    expected_workflow_revision: Mapped[int] = mapped_column(Integer)
    idempotency_key: Mapped[str] = mapped_column(String(200))
    request_hash: Mapped[str] = mapped_column(String(64))
    source_step_id: Mapped[str] = mapped_column(String(120))
    status: Mapped[AgentWorkflowRunRequestStatus] = mapped_column(
        enum_value_column(AgentWorkflowRunRequestStatus),
        default=AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION,
    )
    graph_run_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_runs.id",
            ondelete="SET NULL",
            name="fk_agent_workflow_run_requests_graph_run_id",
        ),
        nullable=True,
    )
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    confirmed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    conversation: Mapped[AgentConversation] = relationship(back_populates="workflow_run_requests")
    task: Mapped[AgentTask | None] = relationship(
        back_populates="workflow_run_requests",
        foreign_keys=[task_id],
    )
    product: Mapped[Product] = relationship(foreign_keys=[product_id])
    graph: Mapped[WorkflowGraph] = relationship(foreign_keys=[graph_id])
    graph_run: Mapped[WorkflowGraphRun | None] = relationship(foreign_keys=[graph_run_id])
    source_graph_run: Mapped[WorkflowGraphRun | None] = relationship(foreign_keys=[source_graph_run_id])
    turn_projection: Mapped[AgentTurnProjection | None] = relationship(
        back_populates="workflow_run_request",
        foreign_keys="AgentTurnProjection.workflow_run_request_id",
        uselist=False,
    )


class WorkflowRecipe(Base, TimestampMixin):
    """用户保存的配方稳定身份。origin=official 仅保留历史 seed 行。"""

    __tablename__ = "workflow_recipes"
    __table_args__ = (
        Index("ix_workflow_recipes_archived_at", "archived_at"),
        Index("ix_workflow_recipes_origin", "origin"),
        UniqueConstraint("official_key", name="uq_workflow_recipes_official_key"),
        CheckConstraint(
            "(origin = 'official' AND official_key IS NOT NULL) OR "
            "(origin = 'user' AND official_key IS NULL)",
            name="ck_workflow_recipes_origin_key",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    kind: Mapped[WorkflowRecipeKind] = mapped_column(enum_value_column(WorkflowRecipeKind))
    origin: Mapped[WorkflowRecipeOrigin] = mapped_column(enum_value_column(WorkflowRecipeOrigin))
    official_key: Mapped[str | None] = mapped_column(String(80), nullable=True)
    current_version_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_recipe_versions.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_workflow_recipes_current_version_id",
        ),
        nullable=True,
    )
    archived_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    versions: Mapped[list[WorkflowRecipeVersion]] = relationship(
        back_populates="recipe",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowRecipeVersion.recipe_id",
        order_by="WorkflowRecipeVersion.version",
    )
    current_version: Mapped[WorkflowRecipeVersion | None] = relationship(
        foreign_keys=[current_version_id],
        post_update=True,
    )


class WorkflowRecipeVersion(Base):
    """配方的 append-only 严格快照。"""

    __tablename__ = "workflow_recipe_versions"
    __table_args__ = (
        UniqueConstraint("recipe_id", "version", name="uq_workflow_recipe_versions_recipe_version"),
        CheckConstraint("version > 0", name="ck_workflow_recipe_versions_positive_version"),
        CheckConstraint("schema_version = 3", name="ck_workflow_recipe_versions_schema_version"),
        CheckConstraint("catalog_version > 0", name="ck_workflow_recipe_versions_catalog_version"),
        CheckConstraint("length(payload_hash) = 64", name="ck_workflow_recipe_versions_payload_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    recipe_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_recipes.id",
            ondelete="CASCADE",
            name="fk_workflow_recipe_versions_recipe_id",
        ),
    )
    version: Mapped[int] = mapped_column(Integer)
    schema_version: Mapped[int] = mapped_column(Integer, default=3)
    catalog_version: Mapped[int] = mapped_column(Integer)
    creation_source: Mapped[WorkflowRecipeCreationSource] = mapped_column(
        enum_value_column(WorkflowRecipeCreationSource)
    )
    title: Mapped[str] = mapped_column(String(255))
    description: Mapped[str | None] = mapped_column(Text, nullable=True)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    payload_hash: Mapped[str] = mapped_column(String(64))
    governance_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    preferred_visual_system_version_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "visual_system_versions.id",
            ondelete="RESTRICT",
            name="fk_workflow_recipe_versions_preferred_visual_system_version_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    recipe: Mapped[WorkflowRecipe] = relationship(
        back_populates="versions",
        foreign_keys=[recipe_id],
    )
    preferred_visual_system_version: Mapped[VisualSystemVersion | None] = relationship(
        foreign_keys=[preferred_visual_system_version_id]
    )
    draft_seeds: Mapped[list[WorkflowDraftRecipeSeed]] = relationship(
        back_populates="recipe_version",
        foreign_keys="WorkflowDraftRecipeSeed.recipe_version_id",
    )
    graph_applications: Mapped[list[WorkflowRecipeApplication]] = relationship(
        back_populates="recipe_version",
        foreign_keys="WorkflowRecipeApplication.recipe_version_id",
    )


class WorkflowDraftRecipeSeed(Base):
    """把配方应用到目标商品后供 Agent 重建 Draft 的不可变种子。"""

    __tablename__ = "workflow_draft_recipe_seeds"
    __table_args__ = (
        UniqueConstraint("workflow_draft_id", name="uq_workflow_draft_recipe_seeds_draft_id"),
        UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_draft_recipe_seeds_product_key",
        ),
        CheckConstraint("schema_version = 1", name="ck_workflow_draft_recipe_seeds_schema_version"),
        CheckConstraint("length(request_hash) = 64", name="ck_workflow_draft_recipe_seeds_request_hash"),
        Index(
            "ix_workflow_draft_recipe_seeds_product_created",
            "product_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_draft_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_drafts.id",
            ondelete="CASCADE",
            name="fk_workflow_draft_recipe_seeds_draft_id",
        ),
    )
    recipe_version_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_recipe_versions.id",
            ondelete="RESTRICT",
            name="fk_workflow_draft_recipe_seeds_recipe_version_id",
        ),
    )
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "products.id",
            ondelete="CASCADE",
            name="fk_workflow_draft_recipe_seeds_product_id",
        ),
    )
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    idempotency_key: Mapped[str] = mapped_column(String(120))
    request_hash: Mapped[str] = mapped_column(String(64))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    workflow_draft: Mapped[WorkflowDraft] = relationship(
        back_populates="recipe_seed",
        foreign_keys=[workflow_draft_id],
    )
    recipe_version: Mapped[WorkflowRecipeVersion] = relationship(
        back_populates="draft_seeds",
        foreign_keys=[recipe_version_id],
    )
    product: Mapped[Product] = relationship(
        back_populates="workflow_draft_recipe_seeds",
        foreign_keys=[product_id],
    )


class WorkflowRecipeApplication(Base):
    """配方写入目标商品 live graph 的幂等记录。"""

    __tablename__ = "workflow_recipe_applications"
    __table_args__ = (
        UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_recipe_applications_product_key",
        ),
        CheckConstraint(
            "schema_version IN (1, 2)",
            name="ck_workflow_recipe_applications_schema_version",
        ),
        CheckConstraint(
            "(schema_version = 1) OR ("
            "schema_version = 2 "
            "AND preview_graph_revision IS NOT NULL "
            "AND preview_graph_revision >= 0 "
            "AND preview_digest IS NOT NULL "
            "AND length(preview_digest) = 64 "
            "AND updated_node_ids_json IS NOT NULL "
            "AND required_bindings_json IS NOT NULL"
            ")",
            name="ck_workflow_recipe_applications_preview_v2",
        ),
        CheckConstraint("length(request_hash) = 64", name="ck_workflow_recipe_applications_request_hash"),
        CheckConstraint("mode IN ('create', 'merge')", name="ck_workflow_recipe_applications_mode"),
        Index(
            "ix_workflow_recipe_applications_product_created",
            "product_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_workflow_recipe_applications_product_id"),
    )
    recipe_version_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_recipe_versions.id",
            ondelete="RESTRICT",
            name="fk_workflow_recipe_applications_recipe_version_id",
        ),
    )
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_recipe_applications_graph_id"),
    )
    operation_group_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_operation_groups.id",
            ondelete="RESTRICT",
            name="fk_workflow_recipe_applications_operation_group_id",
        ),
    )
    mode: Mapped[str] = mapped_column(String(16))
    schema_version: Mapped[int] = mapped_column(Integer, default=2)
    idempotency_key: Mapped[str] = mapped_column(String(120))
    request_hash: Mapped[str] = mapped_column(String(64))
    added_node_ids_json: Mapped[list[str]] = mapped_column(JSON, default=list)
    added_edge_ids_json: Mapped[list[str]] = mapped_column(JSON, default=list)
    preview_graph_revision: Mapped[int | None] = mapped_column(Integer, nullable=True)
    preview_digest: Mapped[str | None] = mapped_column(String(64), nullable=True)
    updated_node_ids_json: Mapped[list[str] | None] = mapped_column(JSON, nullable=True)
    required_bindings_json: Mapped[list[str] | None] = mapped_column(JSON, nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    product: Mapped[Product] = relationship(
        back_populates="workflow_recipe_applications",
        foreign_keys=[product_id],
    )
    recipe_version: Mapped[WorkflowRecipeVersion] = relationship(
        back_populates="graph_applications",
        foreign_keys=[recipe_version_id],
    )
    graph: Mapped[WorkflowGraph] = relationship(foreign_keys=[graph_id])
    operation_group: Mapped[WorkflowOperationGroup] = relationship(foreign_keys=[operation_group_id])


class WorkflowDraftLegacyArchiveSeed(Base):
    """把一个不可变旧归档绑定到新的 Agent WorkflowDraft。"""

    __tablename__ = "workflow_draft_legacy_archive_seeds"
    __table_args__ = (
        UniqueConstraint(
            "workflow_draft_id",
            name="uq_workflow_draft_legacy_archive_seeds_draft_id",
        ),
        UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_draft_legacy_archive_seeds_product_key",
        ),
        CheckConstraint(
            "schema_version = 1",
            name="ck_workflow_draft_legacy_archive_seeds_schema_version",
        ),
        CheckConstraint(
            "length(request_hash) = 64",
            name="ck_workflow_draft_legacy_archive_seeds_request_hash",
        ),
        CheckConstraint(
            "(workflow_archive_id IS NOT NULL AND canvas_agent_archive_id IS NULL "
            "AND user_template_archive_id IS NULL) OR "
            "(workflow_archive_id IS NULL AND canvas_agent_archive_id IS NOT NULL "
            "AND user_template_archive_id IS NULL) OR "
            "(workflow_archive_id IS NULL AND canvas_agent_archive_id IS NULL "
            "AND user_template_archive_id IS NOT NULL)",
            name="ck_workflow_draft_legacy_archive_seeds_one_archive",
        ),
        Index(
            "ix_workflow_draft_legacy_archive_seeds_product_created",
            "product_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    workflow_draft_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_drafts.id",
            ondelete="CASCADE",
            name="fk_workflow_draft_legacy_archive_seeds_draft_id",
        ),
    )
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "products.id",
            ondelete="CASCADE",
            name="fk_workflow_draft_legacy_archive_seeds_product_id",
        ),
    )
    workflow_archive_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "legacy_workflow_archives.id",
            ondelete="RESTRICT",
            name="fk_workflow_draft_legacy_archive_seeds_workflow_archive_id",
        ),
        nullable=True,
    )
    canvas_agent_archive_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "legacy_canvas_agent_archives.id",
            ondelete="RESTRICT",
            name="fk_workflow_draft_legacy_archive_seeds_canvas_archive_id",
        ),
        nullable=True,
    )
    user_template_archive_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "legacy_user_template_archives.id",
            ondelete="RESTRICT",
            name="fk_workflow_draft_legacy_archive_seeds_template_archive_id",
        ),
        nullable=True,
    )
    schema_version: Mapped[int] = mapped_column(Integer, default=1)
    idempotency_key: Mapped[str] = mapped_column(String(120))
    request_hash: Mapped[str] = mapped_column(String(64))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    workflow_draft: Mapped[WorkflowDraft] = relationship(
        back_populates="legacy_archive_seed",
        foreign_keys=[workflow_draft_id],
    )
    product: Mapped[Product] = relationship(
        back_populates="workflow_draft_legacy_archive_seeds",
        foreign_keys=[product_id],
    )
    workflow_archive: Mapped[LegacyWorkflowArchive | None] = relationship(foreign_keys=[workflow_archive_id])
    canvas_agent_archive: Mapped[LegacyCanvasAgentArchive | None] = relationship(foreign_keys=[canvas_agent_archive_id])
    user_template_archive: Mapped[LegacyUserTemplateArchive | None] = relationship(
        foreign_keys=[user_template_archive_id]
    )
GRAPH_SCHEMA_VERSION = 3
_GRAPH_NODE_TYPES = ", ".join(f"'{member.value}'" for member in GraphNodeType)
_GRAPH_EDGE_DATA_TYPES = ", ".join(f"'{member.value}'" for member in GraphEdgeDataType)
_GRAPH_EDGE_ROLES = ", ".join(f"'{member.value}'" for member in GraphEdgeRole)
_GRAPH_ACTOR_TYPES = ", ".join(f"'{member.value}'" for member in GraphActorType)
_GRAPH_HISTORY_KINDS = ", ".join(f"'{member.value}'" for member in GraphHistoryKind)
_GRAPH_PROPOSAL_STATUSES = ", ".join(f"'{member.value}'" for member in GraphProposalStatus)
_GRAPH_RUN_SCOPES = ", ".join(f"'{member.value}'" for member in GraphRunScope)
_GRAPH_ARTIFACT_TYPES = ", ".join(f"'{member.value}'" for member in GraphArtifactType)
_GRAPH_RUN_STATUSES = ", ".join(f"'{member.value}'" for member in WorkflowRunStatus)
_GRAPH_NODE_RUN_STATUSES = ", ".join(f"'{member.value}'" for member in WorkflowNodeStatus)


class WorkflowGraph(Base, TimestampMixin):
    """schema-v3 canonical graph。"""

    __tablename__ = "workflow_graphs"
    __table_args__ = (
        Index(
            "uq_workflow_graphs_one_active_per_product",
            "product_id",
            unique=True,
            postgresql_where=text("active = true"),
            sqlite_where=text("active = 1"),
        ),
        CheckConstraint("schema_version = 3", name="ck_workflow_graphs_schema_version"),
        CheckConstraint("revision > 0", name="ck_workflow_graphs_positive_revision"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_workflow_graphs_product_id"),
    )
    title: Mapped[str] = mapped_column(String(255), default="商品创意工作流")
    active: Mapped[bool] = mapped_column(Boolean, default=True)
    schema_version: Mapped[int] = mapped_column(Integer, default=GRAPH_SCHEMA_VERSION)
    revision: Mapped[int] = mapped_column(Integer, default=1)
    source_draft_revision_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_draft_revisions.id",
            ondelete="SET NULL",
            name="fk_workflow_graphs_source_draft_revision_id",
        ),
        nullable=True,
    )

    product: Mapped[Product] = relationship(back_populates="graphs")
    nodes: Mapped[list[WorkflowGraphNode]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
    )
    edges: Mapped[list[WorkflowGraphEdge]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowGraphEdge.graph_id",
    )
    groups: Mapped[list[WorkflowGraphGroup]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
    )
    operation_groups: Mapped[list[WorkflowOperationGroup]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
        order_by="WorkflowOperationGroup.created_at.desc(), WorkflowOperationGroup.id.desc()",
    )
    runs: Mapped[list[WorkflowGraphRun]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
        order_by="WorkflowGraphRun.started_at.desc(), WorkflowGraphRun.id.desc()",
    )
    artifacts: Mapped[list[WorkflowGraphArtifact]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowGraphArtifact.graph_id",
    )
    source_draft_revision: Mapped[WorkflowDraftRevision | None] = relationship(
        foreign_keys=[source_draft_revision_id]
    )
    media_library_links: Mapped[list[WorkflowMediaLibraryAsset]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
        order_by="WorkflowMediaLibraryAsset.created_at.desc(), WorkflowMediaLibraryAsset.media_library_asset_id.desc()",
    )
    proposals: Mapped[list[WorkflowGraphProposal]] = relationship(
        back_populates="graph",
        cascade="all, delete-orphan",
        order_by="WorkflowGraphProposal.created_at.desc(), WorkflowGraphProposal.id.desc()",
    )


class WorkflowGraphGroup(Base, TimestampMixin):
    """schema-v3 画布分组，不是 DAG 节点。"""

    __tablename__ = "workflow_graph_groups"
    __table_args__ = (CheckConstraint("sort_order >= 0", name="ck_workflow_graph_groups_non_negative_order"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_graph_groups_graph_id"),
    )
    title: Mapped[str] = mapped_column(String(255))
    sort_order: Mapped[int] = mapped_column(Integer, default=0)

    graph: Mapped[WorkflowGraph] = relationship(back_populates="groups")
    nodes: Mapped[list[WorkflowGraphNode]] = relationship(back_populates="group")


class WorkflowGraphNode(Base, TimestampMixin):
    """schema-v3 图节点。配置状态由当前 revision 推导，不另存权威列。"""

    __tablename__ = "workflow_graph_nodes"
    __table_args__ = (
        CheckConstraint(f"node_type IN ({_GRAPH_NODE_TYPES})", name="ck_workflow_graph_nodes_type"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_graph_nodes_graph_id"),
    )
    node_type: Mapped[GraphNodeType] = mapped_column(String(40))
    title: Mapped[str] = mapped_column(String(255))
    position_x: Mapped[int] = mapped_column(Integer, default=0)
    position_y: Mapped[int] = mapped_column(Integer, default=0)
    config_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    bound_image_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_workflow_graph_nodes_bound_image_asset_id",
        ),
        nullable=True,
    )
    group_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_groups.id",
            ondelete="SET NULL",
            name="fk_workflow_graph_nodes_group_id",
        ),
        nullable=True,
    )
    current_artifact_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_artifacts.id",
            ondelete="SET NULL",
            use_alter=True,
            name="fk_workflow_graph_nodes_current_artifact_id",
        ),
        nullable=True,
    )

    graph: Mapped[WorkflowGraph] = relationship(back_populates="nodes")
    group: Mapped[WorkflowGraphGroup | None] = relationship(back_populates="nodes")
    bound_image_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[bound_image_asset_id])
    outgoing_edges: Mapped[list[WorkflowGraphEdge]] = relationship(
        back_populates="source_node",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowGraphEdge.source_node_id",
    )
    incoming_edges: Mapped[list[WorkflowGraphEdge]] = relationship(
        back_populates="target_node",
        cascade="all, delete-orphan",
        foreign_keys="WorkflowGraphEdge.target_node_id",
    )
    current_artifact: Mapped[WorkflowGraphArtifact | None] = relationship(
        foreign_keys=[current_artifact_id],
        post_update=True,
    )
    node_runs: Mapped[list[WorkflowGraphNodeRun]] = relationship(back_populates="node")


class WorkflowGraphEdge(Base):
    """schema-v3 typed edge。data_type 与 role 由 Node Catalog 决定，不接受客户端任意字符串。"""

    __tablename__ = "workflow_graph_edges"
    __table_args__ = (
        UniqueConstraint(
            "graph_id",
            "source_node_id",
            "target_node_id",
            "role",
            name="uq_workflow_graph_edges_pair_role",
        ),
        CheckConstraint(f"data_type IN ({_GRAPH_EDGE_DATA_TYPES})", name="ck_workflow_graph_edges_data_type"),
        CheckConstraint(f"role IN ({_GRAPH_EDGE_ROLES})", name="ck_workflow_graph_edges_role"),
        CheckConstraint("sort_order >= 0", name="ck_workflow_graph_edges_non_negative_order"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_graph_edges_graph_id"),
    )
    source_node_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graph_nodes.id", ondelete="CASCADE", name="fk_workflow_graph_edges_source_node_id"),
    )
    target_node_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graph_nodes.id", ondelete="CASCADE", name="fk_workflow_graph_edges_target_node_id"),
    )
    data_type: Mapped[GraphEdgeDataType] = mapped_column(String(40))
    role: Mapped[GraphEdgeRole] = mapped_column(String(40))
    sort_order: Mapped[int] = mapped_column(Integer, default=0)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    graph: Mapped[WorkflowGraph] = relationship(back_populates="edges", foreign_keys=[graph_id])
    source_node: Mapped[WorkflowGraphNode] = relationship(
        back_populates="outgoing_edges",
        foreign_keys=[source_node_id],
    )
    target_node: Mapped[WorkflowGraphNode] = relationship(
        back_populates="incoming_edges",
        foreign_keys=[target_node_id],
    )


class WorkflowOperationGroup(Base):
    """一次成功 Graph Command 的 operation group 与 inverse，作为撤销权威。"""

    __tablename__ = "workflow_operation_groups"
    __table_args__ = (
        UniqueConstraint("graph_id", "result_revision", name="uq_workflow_operation_groups_graph_revision"),
        CheckConstraint("base_revision >= 0", name="ck_workflow_operation_groups_non_negative_base"),
        CheckConstraint(
            "result_revision = base_revision + 1",
            name="ck_workflow_operation_groups_revision_step",
        ),
        CheckConstraint(f"actor_type IN ({_GRAPH_ACTOR_TYPES})", name="ck_workflow_operation_groups_actor_type"),
        CheckConstraint(
            f"history_kind IN ({_GRAPH_HISTORY_KINDS})",
            name="ck_workflow_operation_groups_history_kind",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_operation_groups_graph_id"),
    )
    actor_type: Mapped[GraphActorType] = mapped_column(String(40), default=GraphActorType.USER)
    history_kind: Mapped[GraphHistoryKind] = mapped_column(String(16), default=GraphHistoryKind.EDIT)
    summary: Mapped[str] = mapped_column(String(500))
    base_revision: Mapped[int] = mapped_column(Integer)
    result_revision: Mapped[int] = mapped_column(Integer)
    operations_json: Mapped[list[dict[str, Any]]] = mapped_column(JSON, default=list)
    inverse_operations_json: Mapped[list[dict[str, Any]]] = mapped_column(JSON, default=list)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    graph: Mapped[WorkflowGraph] = relationship(back_populates="operation_groups")


class WorkflowGraphProposal(Base):
    """未应用的 Agent 图提案。确认前不写入 live graph，也不能运行。"""

    __tablename__ = "workflow_graph_proposals"
    __table_args__ = (
        Index(
            "uq_workflow_graph_proposals_one_pending_per_graph",
            "graph_id",
            unique=True,
            postgresql_where=text("status = 'pending'"),
            sqlite_where=text("status = 'pending'"),
        ),
        CheckConstraint(
            f"status IN ({_GRAPH_PROPOSAL_STATUSES})",
            name="ck_workflow_graph_proposals_status",
        ),
        CheckConstraint("base_graph_revision >= 0", name="ck_workflow_graph_proposals_non_negative_base"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_graph_proposals_graph_id"),
    )
    conversation_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "agent_conversations.id",
            ondelete="SET NULL",
            name="fk_workflow_graph_proposals_conversation_id",
        ),
        nullable=True,
    )
    status: Mapped[GraphProposalStatus] = mapped_column(String(16), default=GraphProposalStatus.PENDING)
    summary: Mapped[str] = mapped_column(String(500))
    base_graph_revision: Mapped[int] = mapped_column(Integer)
    change_set_json: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    operation_group_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_operation_groups.id",
            ondelete="SET NULL",
            name="fk_workflow_graph_proposals_operation_group_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    resolved_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    graph: Mapped[WorkflowGraph] = relationship(back_populates="proposals")
    conversation: Mapped[AgentConversation | None] = relationship(foreign_keys=[conversation_id])
    operation_group: Mapped[WorkflowOperationGroup | None] = relationship(foreign_keys=[operation_group_id])


class WorkflowGraphRun(Base):
    """schema-v3 一次图运行。snapshot 固定 graph revision，执行不再读 live graph。"""

    __tablename__ = "workflow_graph_runs"
    __table_args__ = (
        Index(
            "uq_workflow_graph_runs_one_active_per_graph",
            "graph_id",
            unique=True,
            postgresql_where=text("status = 'running'"),
            sqlite_where=text("status = 'running'"),
        ),
        CheckConstraint("graph_revision > 0", name="ck_workflow_graph_runs_positive_revision"),
        CheckConstraint(f"run_scope IN ({_GRAPH_RUN_SCOPES})", name="ck_workflow_graph_runs_scope"),
        CheckConstraint(f"status IN ({_GRAPH_RUN_STATUSES})", name="ck_workflow_graph_runs_status"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_graph_runs_graph_id"),
    )
    status: Mapped[WorkflowRunStatus] = mapped_column(String(40), default=WorkflowRunStatus.RUNNING)
    run_scope: Mapped[GraphRunScope] = mapped_column(String(40))
    requested_node_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    graph_revision: Mapped[int] = mapped_column(Integer)
    snapshot_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)
    progress_metadata: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    graph: Mapped[WorkflowGraph] = relationship(back_populates="runs")
    node_runs: Mapped[list[WorkflowGraphNodeRun]] = relationship(
        back_populates="graph_run",
        cascade="all, delete-orphan",
    )


class WorkflowGraphNodeRun(Base):
    """schema-v3 一次运行内单个处理节点的执行记录。"""

    __tablename__ = "workflow_graph_node_runs"
    __table_args__ = (
        Index("ix_workflow_graph_node_runs_run_node", "graph_run_id", "node_id"),
        Index(
            "uq_workflow_graph_node_runs_one_active_per_node",
            "node_id",
            unique=True,
            postgresql_where=text("status IN ('queued', 'running')"),
            sqlite_where=text("status IN ('queued', 'running')"),
        ),
        CheckConstraint("sort_order >= 0", name="ck_workflow_graph_node_runs_non_negative_order"),
        CheckConstraint(f"status IN ({_GRAPH_NODE_RUN_STATUSES})", name="ck_workflow_graph_node_runs_status"),
        CheckConstraint(
            "progress_phase IS NULL OR progress_phase IN ("
            "'claimed', 'prepared', 'provider_call', 'provider_result_received', "
            "'unknown_provider_effect', 'requeued_after_idle')",
            name="ck_workflow_graph_node_runs_progress_phase",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_run_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graph_runs.id", ondelete="CASCADE", name="fk_workflow_graph_node_runs_run_id"),
    )
    node_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("workflow_graph_nodes.id", ondelete="SET NULL", name="fk_workflow_graph_node_runs_node_id"),
        nullable=True,
    )
    status: Mapped[WorkflowNodeStatus] = mapped_column(String(40))
    sort_order: Mapped[int] = mapped_column(Integer, default=0)
    compiled_context_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    output_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    active_attempt_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    progress_phase: Mapped[str | None] = mapped_column(String(80), nullable=True)
    progress_updated_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    graph_run: Mapped[WorkflowGraphRun] = relationship(back_populates="node_runs")
    node: Mapped[WorkflowGraphNode | None] = relationship(back_populates="node_runs")
    artifact: Mapped[WorkflowGraphArtifact | None] = relationship(
        back_populates="node_run",
        uselist=False,
        foreign_keys="WorkflowGraphArtifact.node_run_id",
    )
    provider_effect: Mapped[WorkflowGraphProviderEffect | None] = relationship(
        back_populates="node_run",
        uselist=False,
        cascade="all, delete-orphan",
    )


class WorkflowGraphProviderEffect(Base, TimestampMixin):
    """One provider request made while executing a schema-v3 graph node run."""

    __tablename__ = "workflow_graph_provider_effects"
    __table_args__ = (
        UniqueConstraint("node_run_id", name="uq_workflow_graph_provider_effects_node_run_id"),
        UniqueConstraint("operation_key", name="uq_workflow_graph_provider_effects_operation_key"),
        CheckConstraint(
            "effect_result IN ('pending', 'applied', 'failed', 'unknown')",
            name="ck_workflow_graph_provider_effects_effect_result",
        ),
        CheckConstraint(
            "reconciliation_state IN ('not_requested', 'applied', 'not_applied', 'unknown', 'unsupported')",
            name="ck_workflow_graph_provider_effects_reconciliation_state",
        ),
        CheckConstraint(
            "length(request_hash) = 64",
            name="ck_workflow_graph_provider_effects_request_hash",
        ),
        Index(
            "ix_workflow_graph_provider_effects_reconciliation",
            "effect_result",
            "reconciliation_state",
            "updated_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    node_run_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_node_runs.id",
            ondelete="CASCADE",
            name="fk_workflow_graph_provider_effects_node_run_id",
        ),
    )
    operation_key: Mapped[str] = mapped_column(String(255))
    effect_kind: Mapped[str] = mapped_column(String(80), default="workflow_graph_generation")
    request_hash: Mapped[str] = mapped_column(String(64))
    provider_name: Mapped[str] = mapped_column(String(80))
    attempt_id: Mapped[str] = mapped_column(String(36))
    effect_result: Mapped[str] = mapped_column(String(20), default="pending")
    reconciliation_state: Mapped[str] = mapped_column(String(20), default="not_requested")
    provider_response_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_status: Mapped[str | None] = mapped_column(String(80), nullable=True)
    request_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    result_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    detail: Mapped[str | None] = mapped_column(Text, nullable=True)

    node_run: Mapped[WorkflowGraphNodeRun] = relationship(back_populates="provider_effect")


class WorkflowGraphArtifact(Base):
    """schema-v3 不可变运行产物。current 引用在节点上，revision 不匹配时不得覆盖。"""

    __tablename__ = "workflow_graph_artifacts"
    __table_args__ = (
        UniqueConstraint("node_run_id", name="uq_workflow_graph_artifacts_node_run_id"),
        CheckConstraint("graph_revision > 0", name="ck_workflow_graph_artifacts_positive_revision"),
        CheckConstraint(f"artifact_type IN ({_GRAPH_ARTIFACT_TYPES})", name="ck_workflow_graph_artifacts_type"),
        CheckConstraint("schema_version = 3", name="ck_workflow_graph_artifacts_schema_version"),
        CheckConstraint("length(payload_hash) = 64", name="ck_workflow_graph_artifacts_payload_hash"),
        CheckConstraint("length(input_digest) = 64", name="ck_workflow_graph_artifacts_input_digest"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_graph_artifacts_graph_id"),
    )
    node_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("workflow_graph_nodes.id", ondelete="SET NULL", name="fk_workflow_graph_artifacts_node_id"),
        nullable=True,
    )
    node_run_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_node_runs.id",
            ondelete="SET NULL",
            name="fk_workflow_graph_artifacts_node_run_id",
        ),
        nullable=True,
    )
    artifact_type: Mapped[GraphArtifactType] = mapped_column(String(40))
    schema_version: Mapped[int] = mapped_column(Integer, default=3)
    graph_revision: Mapped[int] = mapped_column(Integer)
    payload_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    payload_hash: Mapped[str] = mapped_column(String(64))
    input_digest: Mapped[str] = mapped_column(String(64))
    product_image_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_workflow_graph_artifacts_product_image_asset_id",
        ),
        nullable=True,
    )
    provider_name: Mapped[str | None] = mapped_column(String(80), nullable=True)
    provider_model: Mapped[str | None] = mapped_column(String(255), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    graph: Mapped[WorkflowGraph] = relationship(back_populates="artifacts", foreign_keys=[graph_id])
    node: Mapped[WorkflowGraphNode | None] = relationship(foreign_keys=[node_id])
    node_run: Mapped[WorkflowGraphNodeRun | None] = relationship(
        back_populates="artifact",
        foreign_keys=[node_run_id],
    )
    product_image_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[product_image_asset_id])


class LocalImageEditTask(Base, TimestampMixin):
    """持久化局部编辑意图、provider effect 边界和结果 lineage。"""

    __tablename__ = "local_image_edit_tasks"
    __table_args__ = (
        UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_local_image_edit_tasks_product_idempotency",
        ),
        CheckConstraint(
            "status IN ('draft', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'unknown')",
            name="ck_local_image_edit_tasks_status",
        ),
        CheckConstraint("revision >= 1", name="ck_local_image_edit_tasks_revision"),
        CheckConstraint("attempts >= 0", name="ck_local_image_edit_tasks_attempts"),
        CheckConstraint(
            "length(source_media_sha256) = 64",
            name="ck_local_image_edit_tasks_source_media_hash",
        ),
        CheckConstraint(
            "request_hash IS NULL OR length(request_hash) = 64",
            name="ck_local_image_edit_tasks_request_hash",
        ),
        CheckConstraint(
            "request_hash IS NULL OR "
            "(requested_provider_name IS NOT NULL AND requested_local_edit_mode IS NOT NULL)",
            name="ck_local_image_edit_tasks_provider_intent",
        ),
        CheckConstraint(
            "(status = 'running' AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL "
            "AND finished_at IS NULL) OR "
            "(status != 'running' AND active_attempt_id IS NULL)",
            name="ck_local_image_edit_tasks_active_attempt",
        ),
        CheckConstraint(
            "(status = 'succeeded' AND result_asset_id IS NOT NULL AND finished_at IS NOT NULL) OR "
            "(status IN ('draft', 'queued', 'running', 'failed', 'cancelled', 'unknown') "
            "AND result_asset_id IS NULL)",
            name="ck_local_image_edit_tasks_result_state",
        ),
        Index(
            "ix_local_image_edit_tasks_product_status_created",
            "product_id",
            "status",
            "created_at",
            "id",
        ),
        Index(
            "ix_local_image_edit_tasks_source_asset",
            "source_asset_id",
            "created_at",
            "id",
        ),
        Index(
            "ix_local_image_edit_tasks_target_node_status",
            "target_node_id",
            "status",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_local_image_edit_tasks_product_id"),
    )
    source_asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_tasks_source_asset_id",
        ),
    )
    source_media_sha256: Mapped[str] = mapped_column(String(64))
    mask_media_object_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "media_objects.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_tasks_mask_media_object_id",
        ),
    )
    target_graph_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graphs.id",
            ondelete="SET NULL",
            name="fk_local_image_edit_tasks_target_graph_id",
        ),
        nullable=True,
    )
    target_node_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_nodes.id",
            ondelete="SET NULL",
            name="fk_local_image_edit_tasks_target_node_id",
        ),
        nullable=True,
    )
    target_graph_revision: Mapped[int | None] = mapped_column(Integer, nullable=True)
    source_artifact_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_artifacts.id",
            ondelete="SET NULL",
            name="fk_local_image_edit_tasks_source_artifact_id",
        ),
        nullable=True,
    )
    source_artifact_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_tasks_source_artifact_asset_id",
        ),
        nullable=True,
    )
    source_artifact_input_digest: Mapped[str | None] = mapped_column(String(64), nullable=True)
    operation: Mapped[str] = mapped_column(String(32))
    instruction: Mapped[str | None] = mapped_column(Text, nullable=True)
    source_text: Mapped[str | None] = mapped_column(Text, nullable=True)
    replacement_text: Mapped[str | None] = mapped_column(Text, nullable=True)
    mask_geometry_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    requested_provider_name: Mapped[str | None] = mapped_column(String(80), nullable=True)
    requested_local_edit_mode: Mapped[str | None] = mapped_column(String(32), nullable=True)
    status: Mapped[LocalImageEditTaskStatus] = mapped_column(
        enum_value_column(LocalImageEditTaskStatus),
        default=LocalImageEditTaskStatus.DRAFT,
    )
    revision: Mapped[int] = mapped_column(Integer, default=1)
    idempotency_key: Mapped[str | None] = mapped_column(String(120), nullable=True)
    request_hash: Mapped[str | None] = mapped_column(String(64), nullable=True)
    attempts: Mapped[int] = mapped_column(Integer, default=0)
    active_attempt_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    progress_phase: Mapped[str | None] = mapped_column(String(80), nullable=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)
    provider_name: Mapped[str | None] = mapped_column(String(80), nullable=True)
    provider_model: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_response_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_status: Mapped[str | None] = mapped_column(String(80), nullable=True)
    result_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_tasks_result_asset_id",
        ),
        nullable=True,
    )
    queued_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    product: Mapped[Product] = relationship(
        back_populates="local_image_edit_tasks",
        foreign_keys=[product_id],
    )
    source_asset: Mapped[ProductImageAsset] = relationship(foreign_keys=[source_asset_id])
    source_artifact_asset: Mapped[ProductImageAsset | None] = relationship(
        foreign_keys=[source_artifact_asset_id]
    )
    mask_media_object: Mapped[MediaObject] = relationship(foreign_keys=[mask_media_object_id])
    target_graph: Mapped[WorkflowGraph | None] = relationship(foreign_keys=[target_graph_id])
    target_node: Mapped[WorkflowGraphNode | None] = relationship(foreign_keys=[target_node_id])
    source_artifact: Mapped[WorkflowGraphArtifact | None] = relationship(foreign_keys=[source_artifact_id])
    result_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[result_asset_id])
    references: Mapped[list[LocalImageEditTaskReference]] = relationship(
        back_populates="task",
        cascade="all, delete-orphan",
        order_by="LocalImageEditTaskReference.sort_order",
    )
    provider_attempts: Mapped[list[LocalImageEditProviderAttempt]] = relationship(
        back_populates="task",
        cascade="all, delete-orphan",
        order_by="LocalImageEditProviderAttempt.attempt_number",
    )
    adoption_events: Mapped[list[LocalImageEditAdoptionEvent]] = relationship(
        back_populates="task",
        foreign_keys="LocalImageEditAdoptionEvent.task_id",
        order_by="LocalImageEditAdoptionEvent.created_at, LocalImageEditAdoptionEvent.id",
    )


class LocalImageEditTaskReference(Base):
    """有序、受限的局部编辑参考资产 junction。"""

    __tablename__ = "local_image_edit_task_references"
    __table_args__ = (
        UniqueConstraint(
            "task_id",
            "asset_id",
            name="uq_local_image_edit_task_references_task_asset",
        ),
        UniqueConstraint(
            "task_id",
            "sort_order",
            name="uq_local_image_edit_task_references_task_order",
        ),
        CheckConstraint(
            "sort_order >= 0 AND sort_order < 6",
            name="ck_local_image_edit_task_references_bounded_order",
        ),
        Index("ix_local_image_edit_task_references_asset", "asset_id"),
    )

    task_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "local_image_edit_tasks.id",
            ondelete="CASCADE",
            name="fk_local_image_edit_task_references_task_id",
        ),
        primary_key=True,
    )
    asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_task_references_asset_id",
        ),
        primary_key=True,
    )
    sort_order: Mapped[int] = mapped_column(Integer)

    task: Mapped[LocalImageEditTask] = relationship(back_populates="references")
    asset: Mapped[ProductImageAsset] = relationship(foreign_keys=[asset_id])


class LocalImageEditProviderAttempt(Base, TimestampMixin):
    """局部编辑 provider attempt/effect 审计；不存图片、mask bytes 或 secret。"""

    __tablename__ = "local_image_edit_provider_attempts"
    __table_args__ = (
        UniqueConstraint(
            "task_id",
            "attempt_number",
            name="uq_local_image_edit_provider_attempts_task_number",
        ),
        UniqueConstraint(
            "task_id",
            "attempt_id",
            name="uq_local_image_edit_provider_attempts_task_attempt",
        ),
        CheckConstraint("attempt_number >= 1", name="ck_local_image_edit_provider_attempts_positive_number"),
        CheckConstraint(
            "length(request_hash) = 64",
            name="ck_local_image_edit_provider_attempts_request_hash",
        ),
        CheckConstraint(
            "phase IN ('claimed', 'provider_pending', 'provider_call', 'provider_result_received', "
            "'succeeded', 'failed', 'unknown')",
            name="ck_local_image_edit_provider_attempts_phase",
        ),
        CheckConstraint(
            "effect_result IN ('pending', 'applied', 'failed', 'unknown', 'unsupported')",
            name="ck_local_image_edit_provider_attempts_effect_result",
        ),
        Index(
            "ix_local_image_edit_provider_attempts_task_created",
            "task_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    task_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "local_image_edit_tasks.id",
            ondelete="CASCADE",
            name="fk_local_image_edit_provider_attempts_task_id",
        ),
    )
    attempt_id: Mapped[str] = mapped_column(String(36))
    attempt_number: Mapped[int] = mapped_column(Integer)
    operation_key: Mapped[str] = mapped_column(String(255))
    request_hash: Mapped[str] = mapped_column(String(64))
    phase: Mapped[str] = mapped_column(String(40), default="claimed")
    effect_result: Mapped[str] = mapped_column(String(20), default="pending")
    provider_name: Mapped[str] = mapped_column(String(80))
    provider_model: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_response_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_status: Mapped[str | None] = mapped_column(String(80), nullable=True)
    request_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    effective_parameters_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    result_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    late_result_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_provider_attempts_late_result_asset_id",
        ),
        nullable=True,
    )
    detail: Mapped[str | None] = mapped_column(Text, nullable=True)

    task: Mapped[LocalImageEditTask] = relationship(back_populates="provider_attempts")
    late_result_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[late_result_asset_id])


class LocalImageEditAdoptionEvent(Base):
    """局部编辑结果的显式 adopt/revert 事件；不另存 current state。"""

    __tablename__ = "local_image_edit_adoption_events"
    __table_args__ = (
        CheckConstraint(
            "event_type IN ('adopt', 'revert')",
            name="ck_local_image_edit_adoption_events_type",
        ),
        CheckConstraint(
            "from_artifact_id IS NOT NULL AND to_artifact_id IS NOT NULL",
            name="ck_local_image_edit_adoption_events_artifacts",
        ),
        Index(
            "ix_local_image_edit_adoption_events_node_created",
            "node_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "products.id",
            ondelete="CASCADE",
            name="fk_local_image_edit_adoption_events_product_id",
        ),
    )
    task_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "local_image_edit_tasks.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_adoption_events_task_id",
        ),
    )
    graph_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graphs.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_adoption_events_graph_id",
        ),
    )
    node_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_nodes.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_adoption_events_node_id",
        ),
    )
    event_type: Mapped[str] = mapped_column(String(16))
    from_artifact_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_artifacts.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_adoption_events_from_artifact_id",
        ),
    )
    to_artifact_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "workflow_graph_artifacts.id",
            ondelete="RESTRICT",
            name="fk_local_image_edit_adoption_events_to_artifact_id",
        ),
    )
    related_event_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "local_image_edit_adoption_events.id",
            ondelete="SET NULL",
            name="fk_local_image_edit_adoption_events_related_event_id",
        ),
        nullable=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    task: Mapped[LocalImageEditTask] = relationship(
        back_populates="adoption_events",
        foreign_keys=[task_id],
    )
class DeliveryRenditionJob(Base, TimestampMixin):
    """从成功的工作流生成原图确定性派生交付图片的 durable 任务。"""

    __tablename__ = "delivery_rendition_jobs"
    __table_args__ = (
        UniqueConstraint(
            "source_asset_id",
            "spec_hash",
            name="uq_delivery_rendition_jobs_source_spec",
        ),
        UniqueConstraint(
            "result_asset_id",
            name="uq_delivery_rendition_jobs_result_asset_id",
        ),
        CheckConstraint(
            "spec_schema_version = 1",
            name="ck_delivery_rendition_jobs_schema_version",
        ),
        CheckConstraint(
            "length(spec_hash) = 64",
            name="ck_delivery_rendition_jobs_spec_hash",
        ),
        CheckConstraint(
            "attempts >= 0",
            name="ck_delivery_rendition_jobs_non_negative_attempts",
        ),
        CheckConstraint(
            "status IN ('queued', 'running', 'succeeded', 'failed')",
            name="ck_delivery_rendition_jobs_status",
        ),
        CheckConstraint(
            "(status = 'running' AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL "
            "AND finished_at IS NULL) OR "
            "(status != 'running' AND active_attempt_id IS NULL)",
            name="ck_delivery_rendition_jobs_active_attempt",
        ),
        CheckConstraint(
            "(status = 'succeeded' AND result_asset_id IS NOT NULL AND finished_at IS NOT NULL) OR "
            "(status = 'failed' AND result_asset_id IS NULL AND finished_at IS NOT NULL) OR "
            "(status IN ('queued', 'running') AND result_asset_id IS NULL AND finished_at IS NULL)",
            name="ck_delivery_rendition_jobs_result_state",
        ),
        Index(
            "ix_delivery_rendition_jobs_product_status_created",
            "product_id",
            "status",
            "created_at",
            "id",
        ),
        Index(
            "ix_delivery_rendition_jobs_source_created",
            "source_asset_id",
            "created_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "products.id",
            ondelete="CASCADE",
            name="fk_delivery_rendition_jobs_product_id",
        ),
    )
    source_asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_delivery_rendition_jobs_source_asset_id",
        ),
    )
    result_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="RESTRICT",
            name="fk_delivery_rendition_jobs_result_asset_id",
        ),
        nullable=True,
    )
    spec_schema_version: Mapped[int] = mapped_column(Integer, default=1)
    spec_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    spec_hash: Mapped[str] = mapped_column(String(64))
    status: Mapped[JobStatus] = mapped_column(enum_value_column(JobStatus), default=JobStatus.QUEUED)
    attempts: Mapped[int] = mapped_column(Integer, default=0)
    active_attempt_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)
    failure_reason: Mapped[str | None] = mapped_column(Text, nullable=True)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    product: Mapped[Product] = relationship(
        back_populates="delivery_rendition_jobs",
        foreign_keys=[product_id],
    )
    source_asset: Mapped[ProductImageAsset] = relationship(
        back_populates="source_rendition_jobs",
        foreign_keys=[source_asset_id],
    )
    result_asset: Mapped[ProductImageAsset | None] = relationship(
        back_populates="result_rendition_job",
        foreign_keys=[result_asset_id],
    )


class AsyncDispatch(Base, TimestampMixin):
    """可靠异步投递的数据库权威记录。

    Redis/Dramatiq 只是 delivery channel；`async_dispatches` 记录每个业务投递的
    pending -> sent -> consumed / dead 生命周期，并提供 lease/token 防止 dispatcher 重复发送。
    """

    __tablename__ = "async_dispatches"
    __table_args__ = (
        UniqueConstraint("delivery_key", name="uq_async_dispatches_delivery_key"),
        CheckConstraint(
            "status IN ('pending', 'sent', 'consumed', 'dead')",
            name="ck_async_dispatches_status",
        ),
        CheckConstraint(
            "attempts >= 0",
            name="ck_async_dispatches_non_negative_attempts",
        ),
        Index("ix_async_dispatches_status_available", "status", "available_at", "id"),
        Index("ix_async_dispatches_lease_expiry", "status", "lease_expires_at", "id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    delivery_key: Mapped[str] = mapped_column(String(255))
    actor_name: Mapped[str] = mapped_column(String(120))
    aggregate_id: Mapped[str] = mapped_column(String(36))
    payload_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    status: Mapped[AsyncDispatchStatus] = mapped_column(
        enum_value_column(AsyncDispatchStatus),
        default=AsyncDispatchStatus.PENDING,
    )
    available_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    lease_token: Mapped[str | None] = mapped_column(String(36), nullable=True)
    lease_expires_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    attempts: Mapped[int] = mapped_column(Integer, default=0)
    last_error: Mapped[str | None] = mapped_column(Text, nullable=True)
    sent_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    consumed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)


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
    media_object_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "media_objects.id",
            ondelete="RESTRICT",
            name="fk_image_session_assets_media_object_id",
        ),
        nullable=False,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    session: Mapped[ImageSession] = relationship(back_populates="assets")
    media_object: Mapped[MediaObject] = relationship(back_populates="image_session_assets")
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
        CheckConstraint(
            "attempts >= 0",
            name="ck_image_session_generation_tasks_non_negative_attempts",
        ),
        CheckConstraint(
            "(status = 'running' AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL "
            "AND finished_at IS NULL) OR "
            "(status != 'running' AND active_attempt_id IS NULL)",
            name="ck_image_session_generation_tasks_active_attempt",
        ),
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
    active_attempt_id: Mapped[str | None] = mapped_column(String(36), nullable=True)
    is_retryable: Mapped[bool] = mapped_column(Boolean, default=True)

    session: Mapped[ImageSession] = relationship(back_populates="generation_tasks")
    base_asset: Mapped[ImageSessionAsset | None] = relationship(foreign_keys=[base_asset_id])
    provider_effects: Mapped[list[ImageSessionProviderEffect]] = relationship(
        back_populates="generation_task",
        cascade="all, delete-orphan",
        order_by="ImageSessionProviderEffect.candidate_start_index",
    )


class ImageSessionProviderEffect(Base, TimestampMixin):
    """One provider request made while materializing an image-session task."""

    __tablename__ = "image_session_provider_effects"
    __table_args__ = (
        UniqueConstraint(
            "generation_task_id",
            "candidate_start_index",
            name="uq_image_session_provider_effects_task_candidate",
        ),
        UniqueConstraint(
            "operation_key",
            name="uq_image_session_provider_effects_operation_key",
        ),
        CheckConstraint(
            "candidate_start_index >= 1",
            name="ck_image_session_provider_effects_candidate_start",
        ),
        CheckConstraint(
            "candidate_count >= 1",
            name="ck_image_session_provider_effects_candidate_count",
        ),
        CheckConstraint(
            "effect_result IN ('pending', 'applied', 'failed', 'unknown')",
            name="ck_image_session_provider_effects_effect_result",
        ),
        CheckConstraint(
            "reconciliation_state IN ('not_requested', 'applied', 'not_applied', 'unknown', 'unsupported')",
            name="ck_image_session_provider_effects_reconciliation_state",
        ),
        CheckConstraint(
            "length(request_hash) = 64",
            name="ck_image_session_provider_effects_request_hash",
        ),
        Index(
            "ix_image_session_provider_effects_reconciliation",
            "effect_result",
            "reconciliation_state",
            "updated_at",
            "id",
        ),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    generation_task_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "image_session_generation_tasks.id",
            ondelete="CASCADE",
            name="fk_image_session_provider_effects_generation_task_id",
        ),
    )
    candidate_start_index: Mapped[int] = mapped_column(Integer)
    candidate_count: Mapped[int] = mapped_column(Integer)
    operation_key: Mapped[str] = mapped_column(String(255))
    effect_kind: Mapped[str] = mapped_column(String(80), default="image_session_generation")
    request_hash: Mapped[str] = mapped_column(String(64))
    provider_name: Mapped[str] = mapped_column(String(80))
    attempt_id: Mapped[str] = mapped_column(String(36))
    effect_result: Mapped[str] = mapped_column(String(20), default="pending")
    reconciliation_state: Mapped[str] = mapped_column(String(20), default="not_requested")
    provider_response_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    provider_status: Mapped[str | None] = mapped_column(String(80), nullable=True)
    request_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    result_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    detail: Mapped[str | None] = mapped_column(Text, nullable=True)

    generation_task: Mapped[ImageSessionGenerationTask] = relationship(back_populates="provider_effects")


class MediaLibraryFolder(Base, TimestampMixin):
    """全局素材库的一层文件夹；删除文件夹只解除组织关系。"""

    __tablename__ = "media_library_folders"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    name: Mapped[str] = mapped_column(String(120))
    normalized_name: Mapped[str] = mapped_column(String(120), unique=True)

    assets: Mapped[list[MediaLibraryAsset]] = relationship(back_populates="folder")


class MediaLibraryTag(Base, TimestampMixin):
    """全局素材库的规范化标签。"""

    __tablename__ = "media_library_tags"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    name: Mapped[str] = mapped_column(String(80))
    normalized_name: Mapped[str] = mapped_column(String(80), unique=True)

    assets: Mapped[list[MediaLibraryAssetTag]] = relationship(back_populates="tag", cascade="all, delete-orphan")


class MediaLibraryAssetTag(Base):
    """素材库资产与标签的多对多 assignment。"""

    __tablename__ = "media_library_asset_tags"
    __table_args__ = (Index("ix_media_library_asset_tags_tag_id", "tag_id"),)

    asset_id: Mapped[str] = mapped_column(
        String(36), ForeignKey("media_library_assets.id", ondelete="CASCADE"), primary_key=True
    )
    tag_id: Mapped[str] = mapped_column(
        String(36), ForeignKey("media_library_tags.id", ondelete="CASCADE"), primary_key=True
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    asset: Mapped[MediaLibraryAsset] = relationship(back_populates="tag_assignments")
    tag: Mapped[MediaLibraryTag] = relationship(back_populates="assets")


class MediaLibraryAsset(Base, TimestampMixin):
    """全局素材库资产，持有 MediaObject 的全局身份和来源 provenance。"""

    __tablename__ = "media_library_assets"
    __table_args__ = (
        UniqueConstraint("source_type", "source_id", name="uq_media_library_assets_source"),
        CheckConstraint(
            "source_type IN ('legacy_gallery', 'image_session_generated', 'product_asset', 'direct_upload')",
            name="ck_media_library_assets_source_type",
        ),
        CheckConstraint("revision >= 1", name="ck_media_library_assets_revision"),
        CheckConstraint(
            "length(provenance_hash) = 64",
            name="ck_media_library_assets_provenance_hash",
        ),
        Index("ix_media_library_assets_media_object_id", "media_object_id"),
        Index("ix_media_library_assets_source_image_session_asset_id", "source_image_session_asset_id"),
        Index("ix_media_library_assets_source_product_asset_id", "source_product_asset_id"),
        Index("ix_media_library_assets_folder_id", "folder_id"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    media_object_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "media_objects.id",
            ondelete="RESTRICT",
            name="fk_media_library_assets_media_object_id",
        ),
    )
    source_type: Mapped[str] = mapped_column(String(40))
    source_id: Mapped[str] = mapped_column(String(36))
    source_image_session_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "image_session_assets.id",
            ondelete="SET NULL",
            name="fk_media_library_assets_source_image_session_asset_id",
        ),
        nullable=True,
    )
    source_product_asset_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey(
            "product_image_assets.id",
            ondelete="SET NULL",
            name="fk_media_library_assets_source_product_asset_id",
        ),
        nullable=True,
    )
    provenance_json: Mapped[dict[str, Any]] = mapped_column(JSON)
    provenance_hash: Mapped[str] = mapped_column(String(64))
    revision: Mapped[int] = mapped_column(Integer, default=1)
    display_name: Mapped[str] = mapped_column(String(255))
    original_filename: Mapped[str] = mapped_column(String(255))
    folder_id: Mapped[str | None] = mapped_column(
        String(36),
        ForeignKey("media_library_folders.id", ondelete="SET NULL", name="fk_media_library_assets_folder_id"),
        nullable=True,
    )
    is_archived: Mapped[bool] = mapped_column(Boolean, default=False)
    archived_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    media_object: Mapped[MediaObject] = relationship()
    folder: Mapped[MediaLibraryFolder | None] = relationship(back_populates="assets")
    tag_assignments: Mapped[list[MediaLibraryAssetTag]] = relationship(
        back_populates="asset", cascade="all, delete-orphan"
    )
    source_image_session_asset: Mapped[ImageSessionAsset | None] = relationship(
        foreign_keys=[source_image_session_asset_id]
    )
    source_product_asset: Mapped[ProductImageAsset | None] = relationship(foreign_keys=[source_product_asset_id])
    workflow_links: Mapped[list[WorkflowMediaLibraryAsset]] = relationship(
        back_populates="media_library_asset",
        cascade="all, delete-orphan",
    )


class MediaLibraryCollectionKey(Base):
    """商品收录请求使用过的 product-scoped idempotency key。"""

    __tablename__ = "media_library_collection_keys"
    __table_args__ = (
        UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_media_library_collection_keys_product_key",
        ),
        CheckConstraint("length(request_hash) = 64", name="ck_media_library_collection_keys_request_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    product_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("products.id", ondelete="CASCADE", name="fk_media_library_collection_keys_product_id"),
    )
    idempotency_key: Mapped[str] = mapped_column(String(200))
    request_hash: Mapped[str] = mapped_column(String(64))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class MediaLibraryUploadKey(Base):
    """批量直接上传请求使用过的 idempotency key 到所建资产的映射。"""

    __tablename__ = "media_library_upload_keys"
    __table_args__ = (
        UniqueConstraint("idempotency_key", name="uq_media_library_upload_keys_key"),
        CheckConstraint("length(request_hash) = 64", name="ck_media_library_upload_keys_request_hash"),
    )

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    idempotency_key: Mapped[str] = mapped_column(String(200))
    request_hash: Mapped[str] = mapped_column(String(64))
    asset_ids_json: Mapped[str] = mapped_column(Text)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class WorkflowMediaLibraryAsset(Base):
    """工作流可用素材集合与全局素材的关联，不持有媒体 bytes。"""

    __tablename__ = "workflow_media_library_assets"
    __table_args__ = (
        Index(
            "ix_workflow_media_library_assets_workflow_created",
            "workflow_id",
            "created_at",
            "media_library_asset_id",
        ),
        Index(
            "ix_workflow_media_library_assets_library_asset_id",
            "media_library_asset_id",
        ),
    )

    workflow_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey("workflow_graphs.id", ondelete="CASCADE", name="fk_workflow_media_library_assets_workflow_id"),
        primary_key=True,
    )
    media_library_asset_id: Mapped[str] = mapped_column(
        String(36),
        ForeignKey(
            "media_library_assets.id",
            ondelete="RESTRICT",
            name="fk_workflow_media_library_assets_library_asset_id",
        ),
        primary_key=True,
    )
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    graph: Mapped[WorkflowGraph] = relationship(back_populates="media_library_links")
    media_library_asset: Mapped[MediaLibraryAsset] = relationship(back_populates="workflow_links")
