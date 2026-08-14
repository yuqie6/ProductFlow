from __future__ import annotations

import json
from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config

from alembic import command
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
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
    WorkflowRecipeKind,
    WorkflowRevealEventKind,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    AgentToolMutation,
    CopySet,
    DeliveryRenditionJob,
    ImageGalleryEntry,
    ImagePromptArtifact,
    ImagePromptArtifactVersion,
    ImagePromptArtifactVersionReference,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    MediaObject,
    PosterVariant,
    Product,
    ProductAssetFolder,
    ProductFactSetVersion,
    ProductImageAsset,
    ProductWorkflow,
    SourceAsset,
    UserCanvasTemplate,
    VisualException,
    VisualSystem,
    VisualSystemVersion,
    VisualSystemVersionReference,
    WorkflowDraft,
    WorkflowDraftRecipeSeed,
    WorkflowDraftRevision,
    WorkflowFolder,
    WorkflowImageGenerationRecord,
    WorkflowImageGenerationReference,
    WorkflowMaterialization,
    WorkflowMaterializationKey,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRecipe,
    WorkflowRecipeVersion,
    WorkflowRevealEvent,
    WorkflowRun,
    new_id,
    utcnow,
)

MODEL_LEGACY_COPY_COLUMNS = [
    "model_" + suffix
    for suffix in ("title", "selling" + "_points", "poster" + "_headline", "c" + "ta")
]
LEGACY_COPY_COLUMNS = ["title", "selling" + "_points", "poster" + "_headline", "c" + "ta"]


def _configure_sqlite_alembic(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    *,
    filename: str,
) -> tuple[Path, Config]:
    database_path = tmp_path / filename
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    return database_path, config


def _insert_gallery_migration_fixture(
    connection: sa.Connection,
    *,
    duplicate_generation_result: bool = False,
) -> None:
    now = "2026-08-12 14:00:00"
    connection.execute(
        sa.text(
            "INSERT INTO products (id, name, created_at, updated_at) "
            "VALUES ('product-gallery', '图库迁移商品', :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO media_objects "
            "(id, storage_path, mime_type, byte_size, width, height, sha256, verification_status, "
            "created_at, verified_at) VALUES "
            "('media-generated', 'products/product-gallery/generated.png', 'image/png', NULL, NULL, NULL, "
            "NULL, 'legacy_pending', :now, NULL), "
            "('media-upload', 'products/product-gallery/upload.png', 'image/png', NULL, NULL, NULL, "
            "NULL, 'legacy_pending', :now, NULL)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO product_image_assets "
            "(id, product_id, media_object_id, origin_type, display_name, original_filename, "
            "parent_asset_id, source_image_session_asset_id, created_at, updated_at) VALUES "
            "('asset-generated', 'product-gallery', 'media-generated', 'workflow_generation', "
            "'生成主图', 'generated.png', NULL, NULL, :now, :now), "
            "('asset-upload', 'product-gallery', 'media-upload', 'upload', "
            "'上传参考', 'upload.png', NULL, NULL, :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO product_workflows "
            "(id, product_id, title, active, schema_version, revision, created_at, updated_at) "
            "VALUES ('workflow-gallery', 'product-gallery', '图库迁移工作流', :active, 2, 1, :now, :now)"
        ),
        {"active": True, "now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_nodes "
            "(id, workflow_id, schema_version, node_key, node_type, title, position_x, position_y, "
            "config_json, status, output_json, failure_reason, last_run_at, folder_id, "
            "bound_image_asset_id, current_prompt_artifact_version_id, created_at, updated_at) "
            "VALUES ('node-image', 'workflow-gallery', 2, 'image.hero.1', 'image_generation', '商品主图', "
            "0, 0, '{}', 'succeeded', '{}', NULL, :now, NULL, 'asset-generated', NULL, :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_runs "
            "(id, workflow_id, status, started_at, finished_at, failure_reason, is_retryable, progress_metadata) "
            "VALUES ('run-image-1', 'workflow-gallery', 'succeeded', :now, :now, NULL, :is_retryable, NULL)"
        ),
        {"is_retryable": False, "now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_node_runs "
            "(id, workflow_run_id, node_id, status, output_json, failure_reason, copy_set_id, "
            "poster_variant_id, started_at, finished_at) "
            "VALUES ('node-run-image-1', 'run-image-1', 'node-image', 'succeeded', '{}', NULL, NULL, NULL, "
            ":now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO visual_systems (id, name, archived_at, created_at, updated_at) "
            "VALUES ('visual-system-gallery', '迁移视觉体系', NULL, :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO visual_system_versions "
            "(id, visual_system_id, version, schema_version, payload_json, payload_hash, source_markdown, "
            "source_draft_revision_id, created_at) VALUES "
            "('visual-version-gallery', 'visual-system-gallery', 1, 1, '{}', :hash, NULL, NULL, :now)"
        ),
        {"hash": "v" * 64, "now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO image_prompt_artifacts "
            "(id, workflow_id, image_type_key, title, created_at, updated_at) VALUES "
            "('prompt-artifact-gallery', 'workflow-gallery', 'hero', '商品主图', :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO image_prompt_artifact_versions "
            "(id, artifact_id, version, schema_version, payload_json, payload_hash, source_draft_revision_id, "
            "source_node_run_id, provider_name, provider_model, provider_response_id, created_at) VALUES "
            "('prompt-version-gallery', 'prompt-artifact-gallery', 1, 1, '{}', :hash, NULL, NULL, "
            "NULL, NULL, NULL, :now)"
        ),
        {"hash": "p" * 64, "now": now},
    )
    generation_rows = [
        {
            "id": "generation-gallery-1",
            "node_run_id": "node-run-image-1",
            "created_at": now,
        }
    ]
    if duplicate_generation_result:
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs "
                "(id, workflow_id, status, started_at, finished_at, failure_reason, is_retryable, progress_metadata) "
                "VALUES ('run-image-2', 'workflow-gallery', 'succeeded', :now, :now, NULL, :is_retryable, NULL)"
            ),
            {"is_retryable": False, "now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_node_runs "
                "(id, workflow_run_id, node_id, status, output_json, failure_reason, copy_set_id, "
                "poster_variant_id, started_at, finished_at) "
                "VALUES ('node-run-image-2', 'run-image-2', 'node-image', 'succeeded', '{}', NULL, NULL, NULL, "
                ":now, :now)"
            ),
            {"now": now},
        )
        generation_rows.append(
            {
                "id": "generation-gallery-2",
                "node_run_id": "node-run-image-2",
                "created_at": now,
            }
        )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_image_generation_records "
            "(id, workflow_node_run_id, product_id, workflow_id, node_id, result_asset_id, "
            "visual_system_version_id, prompt_artifact_version_id, requested_spec_json, "
            "effective_parameters_json, actual_media_json, compiled_prompt, compiled_prompt_hash, "
            "provider_name, provider_model, provider_response_id, provider_status, provider_request_json, "
            "provider_output_json, created_at) VALUES "
            "(:id, :node_run_id, 'product-gallery', 'workflow-gallery', 'node-image', 'asset-generated', "
            "'visual-version-gallery', 'prompt-version-gallery', '{}', '{}', '{}', 'prompt', :hash, "
            "'test', 'test-model', NULL, 'succeeded', NULL, NULL, :created_at)"
        ),
        [dict(row, hash="g" * 64) for row in generation_rows],
    )
    connection.execute(
        sa.text(
            "INSERT INTO workflow_drafts "
            "(id, product_id, status, current_revision_id, final_workflow_id, created_at, updated_at) "
            "VALUES ('draft-gallery', 'product-gallery', 'collecting', NULL, NULL, :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO agent_conversations "
            "(id, product_id, workflow_draft_id, harness_run_id, status, created_at, updated_at) "
            "VALUES ('conversation-gallery', 'product-gallery', 'draft-gallery', 'run-gallery', "
            "'collecting', :now, :now)"
        ),
        {"now": now},
    )
    connection.execute(
        sa.text(
            "INSERT INTO agent_tool_mutations "
            "(id, conversation_id, tool_name, idempotency_key, request_hash, asset_id, "
            "expected_display_name, target_display_name, status, result_json, created_at, updated_at) VALUES "
            "('mutation-gallery', 'conversation-gallery', 'rename_product_image_asset_v1', 'rename-key', "
            ":hash, 'asset-upload', '上传参考', '用户参考图', 'applied', '{}', :now, :now)"
        ),
        {"hash": "m" * 64, "now": now},
    )


def test_sqlalchemy_enum_columns_use_database_values() -> None:
    assert SourceAsset.__table__.c.kind.type.enums == [member.value for member in SourceAssetKind]
    assert ImageSessionAsset.__table__.c.kind.type.enums == [member.value for member in ImageSessionAssetKind]
    assert CopySet.__table__.c.status.type.enums == [member.value for member in CopyStatus]
    assert PosterVariant.__table__.c.kind.type.enums == [member.value for member in PosterKind]
    assert ImageSessionGenerationTask.__table__.c.status.type.enums == [member.value for member in JobStatus]
    assert DeliveryRenditionJob.__table__.c.status.type.enums == [member.value for member in JobStatus]
    assert WorkflowNode.__table__.c.node_type.type.enums == [member.value for member in WorkflowNodeType]
    assert WorkflowNode.__table__.c.status.type.enums == [member.value for member in WorkflowNodeStatus]
    assert WorkflowNodeRun.__table__.c.status.type.enums == [member.value for member in WorkflowNodeStatus]
    assert WorkflowRun.__table__.c.status.type.enums == [member.value for member in WorkflowRunStatus]
    assert MediaObject.__table__.c.verification_status.type.enums == [
        member.value for member in MediaVerificationStatus
    ]
    assert ProductImageAsset.__table__.c.origin_type.type.enums == [
        member.value for member in ProductImageOriginType
    ]
    assert WorkflowDraft.__table__.c.status.type.enums == [member.value for member in WorkflowDraftStatus]
    assert WorkflowRecipe.__table__.c.kind.type.enums == [member.value for member in WorkflowRecipeKind]
    assert WorkflowRevealEvent.__table__.c.kind.type.enums == [
        member.value for member in WorkflowRevealEventKind
    ]


def test_workflow_draft_models_match_atomic_materialization_contract() -> None:
    fact_table = ProductFactSetVersion.__table__
    fact_unique_constraints = {
        constraint.name for constraint in fact_table.constraints if isinstance(constraint, sa.UniqueConstraint)
    }
    assert fact_unique_constraints == {
        "uq_product_fact_set_versions_product_version",
        "uq_product_fact_set_versions_source_draft_revision_id",
    }
    assert {constraint.name for constraint in fact_table.constraints if isinstance(constraint, sa.CheckConstraint)} == {
        "ck_product_fact_set_versions_positive_version",
        "ck_product_fact_set_versions_payload_hash",
    }

    draft_table = WorkflowDraft.__table__
    assert {index.name for index in draft_table.indexes} == {"ix_workflow_drafts_product_status"}
    draft_fks = {fk.parent.name: fk for fk in draft_table.foreign_keys}
    assert draft_fks["product_id"].constraint.name == "fk_workflow_drafts_product_id"
    assert draft_fks["product_id"].ondelete == "CASCADE"
    assert draft_fks["current_revision_id"].constraint.name == "fk_workflow_drafts_current_revision_id"
    assert draft_fks["current_revision_id"].ondelete == "SET NULL"
    assert draft_fks["final_workflow_id"].constraint.name == "fk_workflow_drafts_final_workflow_id"
    assert draft_fks["final_workflow_id"].ondelete == "SET NULL"

    revision_table = WorkflowDraftRevision.__table__
    revision_unique_constraints = {
        constraint.name for constraint in revision_table.constraints if isinstance(constraint, sa.UniqueConstraint)
    }
    assert revision_unique_constraints == {
        "uq_workflow_draft_revisions_draft_version",
        "uq_workflow_draft_revisions_artifact_origin",
    }
    assert revision_table.c.payload_hash.type.length == 64
    assert revision_table.c.confirmed_at.nullable

    workflow_table = ProductWorkflow.__table__
    assert {index.name for index in workflow_table.indexes} == {
        "uq_product_workflows_one_active_per_product",
        "uq_product_workflows_product_v2_revision",
    }
    assert not workflow_table.c.schema_version.nullable
    assert not workflow_table.c.revision.nullable
    assert not workflow_table.c.edit_version.nullable
    assert "ck_product_workflows_non_negative_edit_version" in {
        constraint.name
        for constraint in workflow_table.constraints
        if isinstance(constraint, sa.CheckConstraint)
    }
    source_revision_fk = next(
        fk for fk in workflow_table.foreign_keys if fk.parent.name == "source_draft_revision_id"
    )
    assert source_revision_fk.constraint.name == "fk_product_workflows_source_draft_revision_id"
    assert source_revision_fk.ondelete == "SET NULL"

    folder_table = WorkflowFolder.__table__
    folder_unique_constraints = {
        constraint.name for constraint in folder_table.constraints if isinstance(constraint, sa.UniqueConstraint)
    }
    assert folder_unique_constraints == {"uq_workflow_folders_workflow_key"}
    assert "parent_id" not in folder_table.c
    assert {"position_x", "position_y", "width", "height", "config_json"}.isdisjoint(folder_table.c)

    recipe_table = WorkflowRecipe.__table__
    assert {index.name for index in recipe_table.indexes} == {"ix_workflow_recipes_archived_at"}
    recipe_version_table = WorkflowRecipeVersion.__table__
    assert {
        constraint.name
        for constraint in recipe_version_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {"uq_workflow_recipe_versions_recipe_version"}
    seed_table = WorkflowDraftRecipeSeed.__table__
    assert {
        constraint.name
        for constraint in seed_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_workflow_draft_recipe_seeds_draft_id",
        "uq_workflow_draft_recipe_seeds_product_key",
    }

    node_table = WorkflowNode.__table__
    node_fks = {fk.parent.name: fk for fk in node_table.foreign_keys}
    assert node_fks["folder_id"].constraint.name == "fk_workflow_nodes_folder_id"
    assert node_fks["folder_id"].ondelete == "SET NULL"
    assert node_fks["bound_image_asset_id"].constraint.name == "fk_workflow_nodes_bound_image_asset_id"
    assert node_fks["bound_image_asset_id"].ondelete == "RESTRICT"
    assert "uq_workflow_nodes_workflow_key" in {
        constraint.name for constraint in node_table.constraints if isinstance(constraint, sa.UniqueConstraint)
    }

    materialization_table = WorkflowMaterialization.__table__
    assert {
        constraint.name
        for constraint in materialization_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_workflow_materializations_product_idempotency",
        "uq_workflow_materializations_draft_revision_id",
        "uq_workflow_materializations_workflow_id",
    }
    materialization_key_table = WorkflowMaterializationKey.__table__
    assert {
        constraint.name
        for constraint in materialization_key_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {"uq_workflow_materialization_keys_product_key"}
    reveal_table = WorkflowRevealEvent.__table__
    assert "uq_workflow_reveal_events_materialization_sequence" in {
        constraint.name for constraint in reveal_table.constraints if isinstance(constraint, sa.UniqueConstraint)
    }


def test_prompt_visual_image_models_match_database_contract() -> None:
    visual_system_table = VisualSystem.__table__
    assert {index.name for index in visual_system_table.indexes} == {"ix_visual_systems_archived_at"}

    visual_version_table = VisualSystemVersion.__table__
    assert {
        constraint.name
        for constraint in visual_version_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_visual_system_versions_system_version",
        "uq_visual_system_versions_source_draft_revision_id",
    }
    assert {
        constraint.name
        for constraint in visual_version_table.constraints
        if isinstance(constraint, sa.CheckConstraint)
    } == {
        "ck_visual_system_versions_positive_version",
        "ck_visual_system_versions_schema_version",
        "ck_visual_system_versions_payload_hash",
    }
    visual_version_fks = {fk.parent.name: fk for fk in visual_version_table.foreign_keys}
    assert visual_version_fks["visual_system_id"].ondelete == "CASCADE"
    assert visual_version_fks["source_draft_revision_id"].ondelete == "SET NULL"

    visual_reference_table = VisualSystemVersionReference.__table__
    assert {
        constraint.name
        for constraint in visual_reference_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_visual_system_version_references_position",
        "uq_visual_system_version_references_asset_role",
    }
    visual_reference_fks = {fk.parent.name: fk for fk in visual_reference_table.foreign_keys}
    assert visual_reference_fks["visual_system_version_id"].ondelete == "CASCADE"
    assert visual_reference_fks["asset_id"].ondelete == "RESTRICT"

    draft_revision_visual_fk = next(
        fk
        for fk in WorkflowDraftRevision.__table__.foreign_keys
        if fk.parent.name == "visual_system_version_id"
    )
    assert draft_revision_visual_fk.constraint.name == "fk_workflow_draft_revisions_visual_system_version_id"
    assert draft_revision_visual_fk.ondelete == "SET NULL"
    workflow_visual_fk = next(
        fk for fk in ProductWorkflow.__table__.foreign_keys if fk.parent.name == "visual_system_version_id"
    )
    assert workflow_visual_fk.constraint.name == "fk_product_workflows_visual_system_version_id"
    assert workflow_visual_fk.ondelete == "RESTRICT"

    prompt_table = ImagePromptArtifact.__table__
    assert {
        constraint.name for constraint in prompt_table.constraints if isinstance(constraint, sa.UniqueConstraint)
    } == {"uq_image_prompt_artifacts_workflow_type"}
    prompt_workflow_fk = next(fk for fk in prompt_table.foreign_keys if fk.parent.name == "workflow_id")
    assert prompt_workflow_fk.ondelete == "CASCADE"

    prompt_version_table = ImagePromptArtifactVersion.__table__
    assert {
        constraint.name
        for constraint in prompt_version_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_image_prompt_artifact_versions_artifact_version",
        "uq_image_prompt_artifact_versions_source_node_run_id",
    }
    assert {
        constraint.name
        for constraint in prompt_version_table.constraints
        if isinstance(constraint, sa.CheckConstraint)
    } == {
        "ck_image_prompt_artifact_versions_positive_version",
        "ck_image_prompt_artifact_versions_schema_version",
        "ck_image_prompt_artifact_versions_payload_hash",
    }
    prompt_version_fks = {fk.parent.name: fk for fk in prompt_version_table.foreign_keys}
    assert prompt_version_fks["artifact_id"].ondelete == "CASCADE"
    assert prompt_version_fks["source_draft_revision_id"].ondelete == "SET NULL"
    assert prompt_version_fks["source_node_run_id"].ondelete == "SET NULL"

    prompt_reference_table = ImagePromptArtifactVersionReference.__table__
    assert {
        constraint.name
        for constraint in prompt_reference_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_image_prompt_artifact_version_references_position",
        "uq_image_prompt_artifact_version_references_asset_purpose",
    }
    prompt_reference_fks = {fk.parent.name: fk for fk in prompt_reference_table.foreign_keys}
    assert prompt_reference_fks["prompt_artifact_version_id"].ondelete == "CASCADE"
    assert prompt_reference_fks["asset_id"].ondelete == "RESTRICT"

    current_prompt_fk = next(
        fk
        for fk in WorkflowNode.__table__.foreign_keys
        if fk.parent.name == "current_prompt_artifact_version_id"
    )
    assert current_prompt_fk.constraint.name == "fk_workflow_nodes_current_prompt_artifact_version_id"
    assert current_prompt_fk.ondelete == "SET NULL"

    exception_table = VisualException.__table__
    assert {
        constraint.name
        for constraint in exception_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {"uq_visual_exceptions_workflow_key"}
    assert {
        constraint.name
        for constraint in exception_table.constraints
        if isinstance(constraint, sa.CheckConstraint)
    } == {"ck_visual_exceptions_scope"}
    exception_fks = {fk.parent.name: fk for fk in exception_table.foreign_keys}
    assert exception_fks["workflow_id"].ondelete == "CASCADE"
    assert exception_fks["source_draft_revision_id"].ondelete == "SET NULL"

    generation_table = WorkflowImageGenerationRecord.__table__
    assert {
        constraint.name
        for constraint in generation_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_workflow_image_generation_records_node_run_id",
        "uq_workflow_image_generation_records_result_asset_id",
    }
    assert {
        constraint.name
        for constraint in generation_table.constraints
        if isinstance(constraint, sa.CheckConstraint)
    } == {"ck_workflow_image_generation_records_prompt_hash"}
    for column_name in ("requested_spec_json", "effective_parameters_json", "actual_media_json"):
        assert not generation_table.c[column_name].nullable
    generation_fks = {fk.parent.name: fk for fk in generation_table.foreign_keys}
    assert generation_fks["workflow_node_run_id"].ondelete == "CASCADE"
    assert generation_fks["result_asset_id"].ondelete == "RESTRICT"
    assert generation_fks["visual_system_version_id"].ondelete == "RESTRICT"
    assert generation_fks["prompt_artifact_version_id"].ondelete == "RESTRICT"

    generation_reference_table = WorkflowImageGenerationReference.__table__
    assert {
        constraint.name
        for constraint in generation_reference_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_workflow_image_generation_references_position",
        "uq_workflow_image_generation_references_asset_role",
    }
    generation_reference_fks = {fk.parent.name: fk for fk in generation_reference_table.foreign_keys}
    assert generation_reference_fks["generation_record_id"].ondelete == "CASCADE"
    assert generation_reference_fks["asset_id"].ondelete == "RESTRICT"


def test_canonical_image_asset_models_match_database_contract() -> None:
    media_table = MediaObject.__table__
    assert media_table.c.id.type.length == 36
    assert media_table.c.storage_path.type.length == 500
    assert not media_table.c.storage_path.nullable
    assert media_table.c.mime_type.type.length == 100
    assert not media_table.c.mime_type.nullable
    assert media_table.c.byte_size.nullable
    assert media_table.c.width.nullable
    assert media_table.c.height.nullable
    assert media_table.c.sha256.type.length == 64
    assert media_table.c.sha256.nullable
    assert not media_table.c.verification_status.nullable
    assert media_table.c.verified_at.nullable
    unique_constraint_names = {
        constraint.name for constraint in media_table.constraints if isinstance(constraint, sa.UniqueConstraint)
    }
    assert unique_constraint_names == {
        "uq_media_objects_storage_path"
    }
    check_constraint_names = {
        constraint.name for constraint in media_table.constraints if isinstance(constraint, sa.CheckConstraint)
    }
    assert check_constraint_names == {
        "ck_media_objects_verified_metadata"
    }

    asset_table = ProductImageAsset.__table__
    assert asset_table.c.id.type.length == 36
    assert not asset_table.c.product_id.nullable
    assert not asset_table.c.media_object_id.nullable
    assert not asset_table.c.origin_type.nullable
    assert not asset_table.c.display_name.nullable
    assert not asset_table.c.original_filename.nullable
    assert asset_table.c.image_type_key.nullable
    assert asset_table.c.image_type_key.type.length == 80
    assert asset_table.c.user_folder_id.nullable
    assert asset_table.c.parent_asset_id.nullable
    assert asset_table.c.source_image_session_asset_id.nullable
    assert {index.name for index in asset_table.indexes} == {
        "ix_product_image_assets_product_created",
        "ix_product_image_assets_product_folder_created",
        "ix_product_image_assets_product_type_created",
        "ix_product_image_assets_product_origin_created",
        "ix_product_image_assets_media_object_id",
        "ix_product_image_assets_parent_asset_id",
        "ix_product_image_assets_source_image_session_asset_id",
        "uq_product_image_assets_product_session_asset",
    }
    asset_foreign_keys = {fk.parent.name: fk for fk in asset_table.foreign_keys}
    assert asset_foreign_keys["product_id"].constraint.name == "fk_product_image_assets_product_id"
    assert asset_foreign_keys["product_id"].ondelete == "CASCADE"
    assert asset_foreign_keys["media_object_id"].constraint.name == "fk_product_image_assets_media_object_id"
    assert asset_foreign_keys["media_object_id"].ondelete == "RESTRICT"
    assert asset_foreign_keys["user_folder_id"].constraint.name == "fk_product_image_assets_user_folder_id"
    assert asset_foreign_keys["user_folder_id"].ondelete == "SET NULL"
    assert asset_foreign_keys["parent_asset_id"].constraint.name == "fk_product_image_assets_parent_asset_id"
    assert asset_foreign_keys["parent_asset_id"].ondelete == "RESTRICT"
    assert (
        asset_foreign_keys["source_image_session_asset_id"].constraint.name
        == "fk_product_image_assets_source_image_session_asset_id"
    )
    assert asset_foreign_keys["source_image_session_asset_id"].ondelete == "SET NULL"

    product_table = Product.__table__
    assert product_table.c.cover_image_asset_id.nullable
    cover_fk = next(fk for fk in product_table.foreign_keys if fk.parent.name == "cover_image_asset_id")
    assert cover_fk.constraint.name == "fk_products_cover_image_asset_id"
    assert cover_fk.ondelete == "SET NULL"

    folder_table = ProductAssetFolder.__table__
    assert "parent_id" not in folder_table.c
    assert not folder_table.c.product_id.nullable
    assert not folder_table.c.name.nullable
    assert not folder_table.c.sort_order.nullable
    assert {
        constraint.name
        for constraint in folder_table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {"uq_product_asset_folders_product_name"}
    assert {
        constraint.name
        for constraint in folder_table.constraints
        if isinstance(constraint, sa.CheckConstraint)
    } == {"ck_product_asset_folders_non_negative_sort_order"}
    assert {index.name for index in folder_table.indexes} == {"ix_product_asset_folders_product_sort"}
    folder_product_fk = next(fk for fk in folder_table.foreign_keys if fk.parent.name == "product_id")
    assert folder_product_fk.constraint.name == "fk_product_asset_folders_product_id"
    assert folder_product_fk.ondelete == "CASCADE"

    for table, column_name, constraint_name in (
        (SourceAsset.__table__, "canonical_asset_id", "fk_source_assets_canonical_asset_id"),
        (PosterVariant.__table__, "canonical_asset_id", "fk_poster_variants_canonical_asset_id"),
        (ImageSessionAsset.__table__, "media_object_id", "fk_image_session_assets_media_object_id"),
    ):
        assert table.c[column_name].nullable
        foreign_key = next(fk for fk in table.foreign_keys if fk.parent.name == column_name)
        assert foreign_key.constraint.name == constraint_name
        assert foreign_key.ondelete == "RESTRICT"

    mutation_table = AgentToolMutation.__table__
    assert mutation_table.c.asset_id.nullable
    assert mutation_table.c.expected_display_name.nullable
    assert mutation_table.c.target_display_name.nullable
    assert not mutation_table.c.prepared_json.nullable
    mutation_asset_fk = next(fk for fk in mutation_table.foreign_keys if fk.parent.name == "asset_id")
    assert mutation_asset_fk.constraint.name == "fk_agent_tool_mutations_asset_id"
    assert mutation_asset_fk.ondelete == "SET NULL"


def test_delivery_rendition_job_model_matches_database_contract() -> None:
    table = DeliveryRenditionJob.__table__
    assert table.c.id.type.length == 36
    assert not table.c.product_id.nullable
    assert not table.c.source_asset_id.nullable
    assert table.c.result_asset_id.nullable
    assert not table.c.spec_schema_version.nullable
    assert not table.c.spec_json.nullable
    assert table.c.spec_hash.type.length == 64
    assert not table.c.spec_hash.nullable
    assert not table.c.status.nullable
    assert not table.c.attempts.nullable
    assert table.c.active_attempt_id.nullable
    assert not table.c.is_retryable.nullable
    assert table.c.failure_reason.nullable
    assert table.c.started_at.nullable
    assert table.c.finished_at.nullable

    assert {
        constraint.name
        for constraint in table.constraints
        if isinstance(constraint, sa.UniqueConstraint)
    } == {
        "uq_delivery_rendition_jobs_source_spec",
        "uq_delivery_rendition_jobs_result_asset_id",
    }
    assert {
        constraint.name
        for constraint in table.constraints
        if isinstance(constraint, sa.CheckConstraint)
    } == {
        "ck_delivery_rendition_jobs_schema_version",
        "ck_delivery_rendition_jobs_spec_hash",
        "ck_delivery_rendition_jobs_non_negative_attempts",
        "ck_delivery_rendition_jobs_status",
        "ck_delivery_rendition_jobs_active_attempt",
        "ck_delivery_rendition_jobs_result_state",
    }
    assert {index.name for index in table.indexes} == {
        "ix_delivery_rendition_jobs_product_status_created",
        "ix_delivery_rendition_jobs_source_created",
    }
    foreign_keys = {fk.parent.name: fk for fk in table.foreign_keys}
    assert foreign_keys["product_id"].constraint.name == "fk_delivery_rendition_jobs_product_id"
    assert foreign_keys["product_id"].ondelete == "CASCADE"
    assert foreign_keys["source_asset_id"].constraint.name == "fk_delivery_rendition_jobs_source_asset_id"
    assert foreign_keys["source_asset_id"].ondelete == "RESTRICT"
    assert foreign_keys["result_asset_id"].constraint.name == "fk_delivery_rendition_jobs_result_asset_id"
    assert foreign_keys["result_asset_id"].ondelete == "RESTRICT"


def test_workflow_run_model_has_retryability_and_progress_metadata() -> None:
    table = WorkflowRun.__table__
    assert "is_retryable" in table.c
    assert not table.c.is_retryable.nullable
    assert table.c.is_retryable.default is not None
    assert "progress_metadata" in table.c
    assert table.c.progress_metadata.nullable


def test_source_asset_model_matches_poster_lineage_contract() -> None:
    table = SourceAsset.__table__
    assert table.c.source_poster_variant_id.type.length == 36
    assert table.c.source_poster_variant_id.nullable
    assert "ix_source_assets_source_poster_variant_id" in {index.name for index in table.indexes}
    foreign_keys = {fk.parent.name: fk for fk in table.foreign_keys}
    lineage_fk = foreign_keys["source_poster_variant_id"]
    assert lineage_fk.constraint.name == "fk_source_assets_source_poster_variant_id"
    assert lineage_fk.target_fullname == "poster_variants.id"
    assert lineage_fk.ondelete == "SET NULL"


def test_gallery_entry_model_matches_migration_contract() -> None:
    table = ImageGalleryEntry.__table__
    assert table.c.id.type.length == 36
    assert not table.c.id.nullable
    assert table.c.id.default is not None
    assert table.c.id.default.arg.__name__ == new_id.__name__
    assert table.c.image_session_asset_id.type.length == 36
    assert not table.c.image_session_asset_id.nullable
    assert table.c.image_session_round_id.nullable
    assert not table.c.created_at.nullable
    assert table.c.created_at.default is not None
    assert table.c.created_at.default.arg.__name__ == utcnow.__name__
    assert {index.name for index in table.indexes} == {
        "uq_image_gallery_entries_asset_id",
        "ix_image_gallery_entries_round_id",
        "ix_image_gallery_entries_created_at",
    }
    foreign_keys = {fk.parent.name: fk for fk in table.foreign_keys}
    assert foreign_keys["image_session_asset_id"].constraint.name == "fk_image_gallery_entries_image_session_asset_id"
    assert foreign_keys["image_session_asset_id"].ondelete == "CASCADE"
    assert foreign_keys["image_session_round_id"].constraint.name == "fk_image_gallery_entries_image_session_round_id"
    assert foreign_keys["image_session_round_id"].ondelete == "SET NULL"


def test_user_canvas_template_model_matches_migration_contract() -> None:
    table = UserCanvasTemplate.__table__
    assert table.c.id.type.length == 36
    assert not table.c.id.nullable
    assert table.c.id.default is not None
    assert table.c.id.default.arg.__name__ == new_id.__name__
    assert table.c.key.type.length == 80
    assert not table.c.key.nullable
    assert table.c.title.type.length == 255
    assert not table.c.title.nullable
    assert table.c.description.nullable
    assert table.c.kind.type.length == 40
    assert not table.c.kind.nullable
    assert not table.c.schema_version.nullable
    assert not table.c.template_json.nullable
    assert table.c.archived_at.nullable
    assert not table.c.created_at.nullable
    assert not table.c.updated_at.nullable
    assert {constraint.name for constraint in table.constraints if isinstance(constraint, sa.UniqueConstraint)} == {
        None
    }
    assert {index.name for index in table.indexes} == {"ix_user_canvas_templates_archived_at"}


def test_alembic_upgrade_head_supports_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "alembic.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "head")

    assert database_path.exists()
    get_settings.cache_clear()


def test_canvas_recipe_migration_preserves_member_folders_and_round_trips_sqlite(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="canvas-recipes-roundtrip.db",
    )
    command.upgrade(config, "20260812_0035")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-08-13 10:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-canvas', '画布迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows "
                "(id, product_id, title, active, schema_version, revision, created_at, updated_at) "
                "VALUES "
                "('workflow-canvas', 'product-canvas', '画布迁移工作流', :active, 2, 1, :now, :now), "
                "('workflow-v1', 'product-canvas', '旧版工作流', :inactive, 1, 1, :now, :now)"
            ),
            {"active": True, "inactive": False, "now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_folders "
                "(id, workflow_id, folder_key, title, sort_order, position_x, position_y, width, height, "
                "config_json, created_at, updated_at) VALUES "
                "('folder-member', 'workflow-canvas', 'member-folder', '有成员', 0, 120, 80, 900, 600, "
                "'{}', :now, :now), "
                "('folder-empty', 'workflow-canvas', 'empty-folder', '空文件夹', 1, 900, 80, 640, 420, "
                "'{}', :now, :now), "
                "('folder-v1-empty', 'workflow-v1', 'v1-empty-folder', '旧版空文件夹', 0, 0, 0, 640, 420, "
                "'{}', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_nodes "
                "(id, workflow_id, schema_version, node_key, node_type, title, position_x, position_y, "
                "config_json, status, output_json, failure_reason, last_run_at, folder_id, "
                "bound_image_asset_id, current_prompt_artifact_version_id, created_at, updated_at) "
                "VALUES ('node-canvas', 'workflow-canvas', 2, 'node-canvas', 'product_context', '商品信息', "
                "180, 140, '{}', 'idle', NULL, NULL, NULL, 'folder-member', NULL, NULL, :now, :now)"
            ),
            {"now": now},
        )
    engine.dispose()

    command.upgrade(config, "20260813_0036")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    workflow_columns = {column["name"]: column for column in inspector.get_columns("product_workflows")}
    folder_columns = {column["name"] for column in inspector.get_columns("workflow_folders")}
    assert workflow_columns["edit_version"]["nullable"] is False
    assert {"position_x", "position_y", "width", "height", "config_json"}.isdisjoint(folder_columns)
    assert {
        "workflow_recipes",
        "workflow_recipe_versions",
        "workflow_draft_recipe_seeds",
    } <= set(inspector.get_table_names())
    with engine.connect() as connection:
        assert connection.scalar(
            sa.text("SELECT edit_version FROM product_workflows WHERE id = 'workflow-canvas'")
        ) == 0
        assert connection.execute(
            sa.text("SELECT id FROM workflow_folders ORDER BY id")
        ).scalars().all() == ["folder-member", "folder-v1-empty"]
        assert connection.scalar(
            sa.text("SELECT folder_id FROM workflow_nodes WHERE id = 'node-canvas'")
        ) == "folder-member"
    engine.dispose()

    command.downgrade(config, "20260812_0035")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    folder_columns = {column["name"] for column in inspector.get_columns("workflow_folders")}
    assert {"position_x", "position_y", "width", "height", "config_json"} <= folder_columns
    assert "edit_version" not in {
        column["name"] for column in inspector.get_columns("product_workflows")
    }
    with engine.connect() as connection:
        restored = connection.execute(
            sa.text(
                "SELECT position_x, position_y, width, height, config_json "
                "FROM workflow_folders WHERE id = 'folder-member'"
            )
        ).mappings().one()
    assert restored["position_x"] == 0
    assert restored["position_y"] == 0
    assert restored["width"] == 640
    assert restored["height"] == 420
    assert restored["config_json"] in ({}, "{}")
    engine.dispose()
    get_settings.cache_clear()


def test_canvas_recipe_migration_rejects_downgrade_with_saved_recipe_data(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="canvas-recipes-unsafe-downgrade.db",
    )
    command.upgrade(config, "20260813_0036")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO workflow_recipes "
                "(id, kind, current_version_id, archived_at, created_at, updated_at) "
                "VALUES ('recipe-saved', 'workflow_recipe', NULL, NULL, :now, :now)"
            ),
            {"now": "2026-08-13 11:00:00"},
        )
    engine.dispose()

    with pytest.raises(RuntimeError, match="saved recipe data exists"):
        command.downgrade(config, "20260812_0035")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(sa.text("DELETE FROM workflow_recipes WHERE id = 'recipe-saved'"))
    engine.dispose()
    command.downgrade(config, "20260812_0035")
    get_settings.cache_clear()


def test_delivery_rendition_migration_round_trips_and_rejects_populated_downgrade(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="delivery-renditions-roundtrip.db",
    )
    command.upgrade(config, "20260813_0036")
    command.upgrade(config, "20260814_0037")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "delivery_rendition_jobs" in inspector.get_table_names()
    indexes = {index["name"]: index for index in inspector.get_indexes("delivery_rendition_jobs")}
    assert indexes["ix_delivery_rendition_jobs_product_status_created"]["column_names"] == [
        "product_id",
        "status",
        "created_at",
        "id",
    ]
    assert indexes["ix_delivery_rendition_jobs_source_created"]["column_names"] == [
        "source_asset_id",
        "created_at",
        "id",
    ]
    foreign_keys = {
        tuple(foreign_key["constrained_columns"]): foreign_key
        for foreign_key in inspector.get_foreign_keys("delivery_rendition_jobs")
    }
    assert foreign_keys[("product_id",)]["options"]["ondelete"] == "CASCADE"
    assert foreign_keys[("source_asset_id",)]["options"]["ondelete"] == "RESTRICT"
    assert foreign_keys[("result_asset_id",)]["options"]["ondelete"] == "RESTRICT"

    now = "2026-08-14 12:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-rendition', '交付派生迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO media_objects "
                "(id, storage_path, mime_type, byte_size, width, height, sha256, verification_status, "
                "created_at, verified_at) VALUES "
                "('media-rendition-source', 'media/source.png', 'image/png', 100, 10, 10, :hash, "
                "'verified', :now, :now)"
            ),
            {"hash": "a" * 64, "now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_image_assets "
                "(id, product_id, media_object_id, origin_type, display_name, original_filename, "
                "image_type_key, user_folder_id, parent_asset_id, source_image_session_asset_id, "
                "created_at, updated_at) VALUES "
                "('asset-rendition-source', 'product-rendition', 'media-rendition-source', "
                "'workflow_generation', '生成原图', 'source.png', 'hero', NULL, NULL, NULL, :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO delivery_rendition_jobs "
                "(id, product_id, source_asset_id, result_asset_id, spec_schema_version, spec_json, "
                "spec_hash, status, attempts, active_attempt_id, is_retryable, failure_reason, started_at, "
                "finished_at, created_at, updated_at) VALUES "
                "('job-rendition', 'product-rendition', 'asset-rendition-source', NULL, 1, :spec, :hash, "
                "'queued', 0, NULL, :retryable, NULL, NULL, NULL, :now, :now)"
            ),
            {
                "spec": json.dumps({"width": 32, "height": 32, "format": "png", "fit": "cover"}),
                "hash": "b" * 64,
                "retryable": True,
                "now": now,
            },
        )
    engine.dispose()

    with pytest.raises(RuntimeError, match="rendition jobs exist"):
        command.downgrade(config, "20260813_0036")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(sa.text("DELETE FROM delivery_rendition_jobs"))
    engine.dispose()
    command.downgrade(config, "20260813_0036")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    assert "delivery_rendition_jobs" not in sa.inspect(engine).get_table_names()
    engine.dispose()
    get_settings.cache_clear()


def test_product_gallery_explorer_migration_round_trips_sqlite(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="product-gallery-explorer-roundtrip.db",
    )
    command.upgrade(config, "20260812_0034")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        _insert_gallery_migration_fixture(connection)
    engine.dispose()

    command.upgrade(config, "20260812_0035")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "product_asset_folders" in inspector.get_table_names()
    asset_columns = {column["name"]: column for column in inspector.get_columns("product_image_assets")}
    assert asset_columns["image_type_key"]["nullable"] is True
    assert asset_columns["user_folder_id"]["nullable"] is True
    asset_indexes = {index["name"]: index for index in inspector.get_indexes("product_image_assets")}
    assert asset_indexes["ix_product_image_assets_product_folder_created"]["column_names"] == [
        "product_id",
        "user_folder_id",
        "created_at",
        "id",
    ]
    assert asset_indexes["ix_product_image_assets_product_type_created"]["column_names"] == [
        "product_id",
        "image_type_key",
        "created_at",
        "id",
    ]
    assert asset_indexes["ix_product_image_assets_product_origin_created"]["column_names"] == [
        "product_id",
        "origin_type",
        "created_at",
        "id",
    ]
    folder_foreign_keys = {
        tuple(foreign_key["constrained_columns"]): foreign_key
        for foreign_key in inspector.get_foreign_keys("product_asset_folders")
    }
    assert folder_foreign_keys[("product_id",)]["options"]["ondelete"] == "CASCADE"
    asset_foreign_keys = {
        tuple(foreign_key["constrained_columns"]): foreign_key
        for foreign_key in inspector.get_foreign_keys("product_image_assets")
    }
    assert asset_foreign_keys[("user_folder_id",)]["options"]["ondelete"] == "SET NULL"
    generation_uniques = {
        constraint["name"]
        for constraint in inspector.get_unique_constraints("workflow_image_generation_records")
    }
    assert "uq_workflow_image_generation_records_result_asset_id" in generation_uniques

    with engine.connect() as connection:
        image_types = dict(
            connection.execute(
                sa.text("SELECT id, image_type_key FROM product_image_assets ORDER BY id")
            ).all()
        )
        mutation = connection.execute(
            sa.text(
                "SELECT asset_id, expected_display_name, target_display_name, prepared_json "
                "FROM agent_tool_mutations WHERE id = 'mutation-gallery'"
            )
        ).mappings().one()
    assert image_types == {"asset-generated": "hero", "asset-upload": None}
    assert mutation["asset_id"] == "asset-upload"
    assert mutation["expected_display_name"] == "上传参考"
    assert mutation["target_display_name"] == "用户参考图"
    prepared_json = mutation["prepared_json"]
    if isinstance(prepared_json, str):
        prepared_json = json.loads(prepared_json)
    assert prepared_json == {
        "schema_version": 1,
        "operation": "rename_asset",
        "scope": {
            "conversation_id": "conversation-gallery",
            "product_id": "product-gallery",
        },
        "before": {"asset_id": "asset-upload", "display_name": "上传参考"},
        "target": {"asset_id": "asset-upload", "display_name": "用户参考图"},
    }

    with engine.begin() as connection:
        connection.exec_driver_sql("PRAGMA foreign_keys=ON")
        connection.execute(
            sa.text(
                "INSERT INTO product_asset_folders "
                "(id, product_id, name, sort_order, created_at, updated_at) "
                "VALUES ('folder-gallery', 'product-gallery', '已整理', 0, :now, :now)"
            ),
            {"now": "2026-08-12 15:00:00"},
        )
        connection.execute(
            sa.text(
                "UPDATE product_image_assets SET user_folder_id = 'folder-gallery' "
                "WHERE id = 'asset-upload'"
            )
        )
        connection.execute(sa.text("DELETE FROM product_asset_folders WHERE id = 'folder-gallery'"))
        assert connection.scalar(
            sa.text("SELECT user_folder_id FROM product_image_assets WHERE id = 'asset-upload'")
        ) is None
    engine.dispose()

    command.downgrade(config, "20260812_0034")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "product_asset_folders" not in inspector.get_table_names()
    assert {"image_type_key", "user_folder_id"}.isdisjoint(
        {column["name"] for column in inspector.get_columns("product_image_assets")}
    )
    assert "prepared_json" not in {
        column["name"] for column in inspector.get_columns("agent_tool_mutations")
    }
    with engine.connect() as connection:
        assert connection.scalar(
            sa.text("SELECT display_name FROM product_image_assets WHERE id = 'asset-upload'")
        ) == "上传参考"
        assert connection.scalar(
            sa.text("SELECT target_display_name FROM agent_tool_mutations WHERE id = 'mutation-gallery'")
        ) == "用户参考图"
        assert connection.scalar(sa.text("SELECT COUNT(*) FROM media_objects")) == 2
    engine.dispose()

    command.upgrade(config, "20260812_0035")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.connect() as connection:
        assert connection.scalar(
            sa.text("SELECT image_type_key FROM product_image_assets WHERE id = 'asset-generated'")
        ) == "hero"
        assert connection.scalar(sa.text("SELECT COUNT(*) FROM media_objects")) == 2
    engine.dispose()
    get_settings.cache_clear()


def test_product_gallery_explorer_migration_rejects_unsafe_downgrade(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="product-gallery-explorer-unsafe-downgrade.db",
    )
    command.upgrade(config, "20260812_0034")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        _insert_gallery_migration_fixture(connection)
    engine.dispose()
    command.upgrade(config, "20260812_0035")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO product_asset_folders "
                "(id, product_id, name, sort_order, created_at, updated_at) "
                "VALUES ('folder-blocking', 'product-gallery', '不可丢弃', 0, :now, :now)"
            ),
            {"now": "2026-08-12 16:00:00"},
        )
    engine.dispose()
    with pytest.raises(RuntimeError, match="user folders exist"):
        command.downgrade(config, "20260812_0034")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(sa.text("DELETE FROM product_asset_folders WHERE id = 'folder-blocking'"))
        connection.execute(
            sa.text(
                "INSERT INTO agent_tool_mutations "
                "(id, conversation_id, tool_name, idempotency_key, request_hash, asset_id, "
                "expected_display_name, target_display_name, prepared_json, status, result_json, "
                "created_at, updated_at) VALUES "
                "('mutation-folder-v2', 'conversation-gallery', 'create_product_image_folder_v1', "
                "'folder-key', :hash, NULL, NULL, NULL, :prepared, 'applied', '{}', :now, :now)"
            ),
            {
                "hash": "n" * 64,
                "prepared": json.dumps(
                    {
                        "schema_version": 1,
                        "operation": "create_folder",
                        "scope": {"product_id": "product-gallery"},
                        "before": None,
                        "target": {"folder_id": "folder-v2", "name": "Agent 整理"},
                    }
                ),
                "now": "2026-08-12 16:01:00",
            },
        )
    engine.dispose()
    with pytest.raises(RuntimeError, match="v2-only mutation ledger rows exist"):
        command.downgrade(config, "20260812_0034")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "product_asset_folders" in inspector.get_table_names()
    with engine.connect() as connection:
        assert connection.scalar(sa.text("SELECT COUNT(*) FROM media_objects")) == 2
        assert connection.scalar(
            sa.text("SELECT COUNT(*) FROM agent_tool_mutations WHERE id = 'mutation-folder-v2'")
        ) == 1
    engine.dispose()
    get_settings.cache_clear()


def test_product_gallery_explorer_migration_rejects_duplicate_generation_results(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path, config = _configure_sqlite_alembic(
        tmp_path,
        monkeypatch,
        filename="product-gallery-explorer-duplicate-generation.db",
    )
    command.upgrade(config, "20260812_0034")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        _insert_gallery_migration_fixture(connection, duplicate_generation_result=True)
    engine.dispose()

    with pytest.raises(RuntimeError, match="duplicate result_asset_id asset-generated"):
        command.upgrade(config, "20260812_0035")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "product_asset_folders" not in inspector.get_table_names()
    with engine.connect() as connection:
        assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260812_0034"
        assert connection.scalar(
            sa.text("SELECT COUNT(*) FROM workflow_image_generation_records")
        ) == 2
    engine.dispose()
    get_settings.cache_clear()


def test_workflow_draft_migration_round_trips_sqlite_without_mutating_v1_rows(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "workflow-draft-roundtrip.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260811_0031")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products "
                "(id, name, category, price, source_note, current_confirmed_copy_set_id, "
                "cover_image_asset_id, created_at, updated_at) "
                "VALUES (:id, :name, NULL, NULL, NULL, NULL, NULL, :created_at, :updated_at)"
            ),
            {
                "id": "legacy-product",
                "name": "历史商品",
                "created_at": "2026-08-11 00:00:00",
                "updated_at": "2026-08-11 00:00:00",
            },
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows "
                "(id, product_id, title, active, created_at, updated_at) "
                "VALUES ('legacy-workflow', 'legacy-product', '历史工作流', 1, "
                "'2026-08-11 00:00:00', '2026-08-11 00:00:00')"
            )
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_nodes "
                "(id, workflow_id, node_type, title, position_x, position_y, config_json, status, "
                "output_json, failure_reason, last_run_at, created_at, updated_at) "
                "VALUES ('legacy-node', 'legacy-workflow', 'product_context', '商品', 0, 0, '{}', 'idle', "
                "NULL, NULL, NULL, '2026-08-11 00:00:00', '2026-08-11 00:00:00')"
            )
        )
    engine.dispose()

    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.connect() as connection:
        workflow = connection.execute(
            sa.text(
                "SELECT id, active, schema_version, revision, source_draft_revision_id, "
                "visual_system_version_id "
                "FROM product_workflows WHERE id = 'legacy-workflow'"
            )
        ).mappings().one()
        node = connection.execute(
            sa.text(
                "SELECT id, schema_version, node_key, folder_id, bound_image_asset_id, "
                "current_prompt_artifact_version_id "
                "FROM workflow_nodes WHERE id = 'legacy-node'"
            )
        ).mappings().one()
        assert dict(workflow) == {
            "id": "legacy-workflow",
            "active": 1,
            "schema_version": 1,
            "revision": 1,
            "source_draft_revision_id": None,
            "visual_system_version_id": None,
        }
        assert dict(node) == {
            "id": "legacy-node",
            "schema_version": 1,
            "node_key": None,
            "folder_id": None,
            "bound_image_asset_id": None,
            "current_prompt_artifact_version_id": None,
        }
        assert {
            "workflow_drafts",
            "workflow_draft_revisions",
            "product_fact_set_versions",
            "workflow_folders",
            "workflow_materializations",
            "workflow_materialization_keys",
            "workflow_reveal_events",
            "visual_systems",
            "visual_system_versions",
            "visual_system_version_references",
            "image_prompt_artifacts",
            "image_prompt_artifact_versions",
            "image_prompt_artifact_version_references",
            "visual_exceptions",
            "workflow_image_generation_records",
            "workflow_image_generation_references",
        }.issubset(sa.inspect(connection).get_table_names())
        assert connection.scalar(sa.text("SELECT count(*) FROM visual_systems")) == 0
    engine.dispose()

    command.downgrade(config, "20260811_0031")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.connect() as connection:
        inspector = sa.inspect(connection)
        assert "workflow_drafts" not in inspector.get_table_names()
        assert "schema_version" not in {column["name"] for column in inspector.get_columns("product_workflows")}
        assert "node_key" not in {column["name"] for column in inspector.get_columns("workflow_nodes")}
        assert connection.scalar(
            sa.text("SELECT active FROM product_workflows WHERE id = 'legacy-workflow'")
        ) == 1
        assert connection.scalar(sa.text("SELECT title FROM workflow_nodes WHERE id = 'legacy-node'")) == "商品"
    engine.dispose()

    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.connect() as connection:
        assert connection.scalar(
            sa.text("SELECT schema_version FROM product_workflows WHERE id = 'legacy-workflow'")
        ) == 1
        assert connection.scalar(sa.text("SELECT schema_version FROM workflow_nodes WHERE id = 'legacy-node'")) == 1
    engine.dispose()
    get_settings.cache_clear()


def test_source_asset_poster_lineage_migration_supports_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "source-asset-poster-lineage.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260627_0029")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-08-06 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) VALUES "
                "('product-1', 'lineage product', :now, :now), "
                "('product-2', 'other product', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO copy_sets "
                "(id, product_id, creative_brief_id, status, provider_name, model_name, prompt_version, "
                "edited_at, confirmed_at, created_at, updated_at, structured_payload, model_structured_payload) "
                "VALUES "
                "('copy-1', 'product-1', NULL, 'draft', 'test', 'test', 'test', NULL, NULL, :now, :now, NULL, NULL), "
                "('copy-2', 'product-2', NULL, 'draft', 'test', 'test', 'test', NULL, NULL, :now, :now, NULL, NULL)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO poster_variants "
                "(id, product_id, copy_set_id, kind, template_name, mime_type, storage_path, width, height, "
                "created_at) "
                "VALUES (:id, :product_id, :copy_set_id, 'promo_poster', 'test', 'image/png', :storage_path, 1, 1, "
                ":now)"
            ),
            [
                {
                    "id": poster_id,
                    "product_id": product_id,
                    "copy_set_id": copy_set_id,
                    "storage_path": f"{poster_id}.png",
                    "now": now,
                }
                for poster_id, product_id, copy_set_id in (
                    ("poster-normal", "product-1", "copy-1"),
                    ("poster-legacy", "product-1", "copy-1"),
                    ("poster-duplicate", "product-1", "copy-1"),
                    ("poster-reference", "product-1", "copy-1"),
                    ("poster-ambiguous-a", "product-1", "copy-1"),
                    ("poster-ambiguous-b", "product-1", "copy-1"),
                    ("poster-missing-source", "product-1", "copy-1"),
                    ("poster-existing", "product-1", "copy-1"),
                    ("poster-other", "product-2", "copy-2"),
                )
            ],
        )
        connection.execute(
            sa.text(
                "INSERT INTO source_assets "
                "(id, product_id, kind, original_filename, mime_type, storage_path, created_at, "
                "source_poster_variant_id) "
                "VALUES (:id, :product_id, :kind, :filename, 'image/png', :storage_path, :created_at, :poster_id)"
            ),
            [
                {
                    "id": asset_id,
                    "product_id": product_id,
                    "kind": kind,
                    "filename": f"{asset_id}.png",
                    "storage_path": f"{asset_id}.png",
                    "created_at": created_at,
                    "poster_id": poster_id,
                }
                for asset_id, product_id, kind, created_at, poster_id in (
                    ("asset-normal", "product-1", "reference_image", now, None),
                    ("asset-legacy", "product-1", "reference_image", now, None),
                    ("asset-duplicate-old", "product-1", "reference_image", "2026-08-06 00:00:01", None),
                    ("asset-duplicate-new", "product-1", "reference_image", "2026-08-06 00:00:02", None),
                    ("asset-reference", "product-1", "reference_image", now, None),
                    ("asset-ambiguous", "product-1", "reference_image", now, None),
                    ("asset-existing", "product-1", "reference_image", now, "poster-existing"),
                    ("asset-invalid", "product-1", "reference_image", now, "missing-poster"),
                    ("asset-cross-product", "product-1", "reference_image", now, "poster-other"),
                    ("asset-original", "product-1", "original_image", now, "poster-normal"),
                )
            ],
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows "
                "(id, product_id, title, active, created_at, updated_at) "
                "VALUES ('workflow-1', 'product-1', 'lineage workflow', 1, :now, :now)"
            ),
            {"now": now},
        )
        node_rows = [
            (
                "node-image-1",
                "image_generation",
                {
                    "generated_poster_variant_ids": [
                        "poster-normal",
                        "poster-duplicate",
                        "poster-missing-source",
                        "poster-ambiguous-a",
                    ],
                    "filled_source_asset_ids": [
                        "asset-normal",
                        "asset-duplicate-old",
                        "asset-not-found",
                        "asset-ambiguous",
                    ],
                },
            ),
            (
                "node-image-2",
                "image_generation",
                {
                    "generated_poster_variant_ids": ["poster-duplicate", "poster-ambiguous-b", "poster-orphan"],
                    "filled_source_asset_ids": ["asset-duplicate-new", "asset-ambiguous", "asset-not-found"],
                },
            ),
            (
                "node-legacy",
                "image_generation",
                {"poster_variant_ids": ["poster-legacy"], "filled_source_asset_ids": ["asset-legacy"]},
            ),
            (
                "node-reference",
                "reference_image",
                {"source_poster_variant_id": "poster-reference", "source_asset_ids": ["asset-reference"]},
            ),
        ]
        connection.execute(
            sa.text(
                "INSERT INTO workflow_nodes "
                "(id, workflow_id, node_type, title, position_x, position_y, config_json, status, output_json, "
                "failure_reason, last_run_at, created_at, updated_at) "
                "VALUES (:id, 'workflow-1', :node_type, :id, 0, 0, '{}', 'succeeded', :output_json, "
                "NULL, NULL, :now, :now)"
            ),
            [
                {"id": node_id, "node_type": node_type, "output_json": json.dumps(output), "now": now}
                for node_id, node_type, output in node_rows
            ],
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    indexes = {index["name"]: index for index in inspector.get_indexes("source_assets")}
    assert indexes["ix_source_assets_source_poster_variant_id"]["column_names"] == ["source_poster_variant_id"]
    foreign_keys = {
        tuple(foreign_key["constrained_columns"]): foreign_key
        for foreign_key in inspector.get_foreign_keys("source_assets")
    }
    lineage_fk = foreign_keys[("source_poster_variant_id",)]
    assert lineage_fk["name"] == "fk_source_assets_source_poster_variant_id"
    assert lineage_fk["referred_table"] == "poster_variants"
    assert lineage_fk["options"]["ondelete"] == "SET NULL"

    with engine.connect() as connection:
        lineage = dict(
            connection.execute(
                sa.text("SELECT id, source_poster_variant_id FROM source_assets")
            ).all()
        )
    assert lineage["asset-normal"] == "poster-normal"
    assert lineage["asset-legacy"] == "poster-legacy"
    assert lineage["asset-duplicate-new"] == "poster-duplicate"
    assert lineage["asset-duplicate-old"] is None
    assert lineage["asset-reference"] == "poster-reference"
    assert lineage["asset-ambiguous"] is None
    assert lineage["asset-existing"] == "poster-existing"
    assert lineage["asset-invalid"] is None
    assert lineage["asset-cross-product"] is None
    assert lineage["asset-original"] is None

    with engine.begin() as connection:
        connection.exec_driver_sql("PRAGMA foreign_keys=ON")
        connection.execute(sa.text("DELETE FROM poster_variants WHERE id = 'poster-normal'"))
        remaining_lineage = connection.execute(
            sa.text("SELECT source_poster_variant_id FROM source_assets WHERE id = 'asset-normal'")
        ).scalar_one()
        assert remaining_lineage is None
        assert (
            connection.execute(sa.text("SELECT COUNT(*) FROM source_assets WHERE id = 'asset-normal'")).scalar_one()
            == 1
        )

    engine.dispose()
    command.downgrade(config, "20260627_0029")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "source_poster_variant_id" in {column["name"] for column in inspector.get_columns("source_assets")}
    assert "ix_source_assets_source_poster_variant_id" in {
        index["name"] for index in inspector.get_indexes("source_assets")
    }
    assert not any(
        tuple(foreign_key["constrained_columns"]) == ("source_poster_variant_id",)
        for foreign_key in inspector.get_foreign_keys("source_assets")
    )
    engine.dispose()
    get_settings.cache_clear()


def test_canonical_image_asset_migration_backfills_and_downgrades_sqlite(
    tmp_path: Path,
    monkeypatch,
) -> None:
    database_path = tmp_path / "canonical-image-assets.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260806_0030")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-08-11 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES "
                "('product-1', 'canonical product', :now, :now), "
                "('product-2', 'cross-lineage product', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO copy_sets "
                "(id, product_id, creative_brief_id, status, provider_name, model_name, prompt_version, "
                "edited_at, confirmed_at, created_at, updated_at, structured_payload, model_structured_payload) "
                "VALUES "
                "('copy-1', 'product-1', NULL, 'draft', 'test', 'test', 'test', NULL, NULL, :now, :now, NULL, NULL)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO poster_variants "
                "(id, product_id, copy_set_id, kind, template_name, mime_type, storage_path, width, height, "
                "created_at) VALUES "
                "('poster-paired', 'product-1', 'copy-1', 'promo_poster', 'paired', 'image/png', "
                "'products/product-1/posters/poster-paired.png', 1200, 1200, :now), "
                "('poster-orphan', 'product-1', 'copy-1', 'main_image', 'orphan', 'image/webp', "
                "'products/product-1/posters/poster-orphan.webp', 1200, 1200, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO source_assets "
                "(id, product_id, kind, original_filename, mime_type, storage_path, source_poster_variant_id, "
                "created_at) VALUES "
                "('asset-original', 'product-1', 'original_image', 'front.png', 'image/png', "
                "'products/product-1/source/front.png', NULL, :now), "
                "('asset-reference', 'product-1', 'reference_image', 'generated.png', 'image/png', "
                "'products/product-1/references/generated.png', 'poster-paired', :now), "
                "('asset-cross', 'product-2', 'reference_image', 'cross.png', 'image/png', "
                "'products/product-2/references/cross.png', 'poster-paired', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_sessions (id, title, created_at, updated_at) "
                "VALUES ('session-1', 'legacy session', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_session_assets "
                "(id, session_id, kind, original_filename, mime_type, storage_path, created_at) "
                "VALUES ('session-asset-1', 'session-1', 'generated_image', 'candidate.jpg', 'image/jpeg', "
                "'image_sessions/session-1/generated/candidate.jpg', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_gallery_entries "
                "(id, image_session_asset_id, image_session_round_id, created_at) "
                "VALUES ('gallery-entry-1', 'session-asset-1', NULL, :now)"
            ),
            {"now": now},
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert {"media_objects", "product_image_assets"} <= set(inspector.get_table_names())
    product_columns = {column["name"]: column for column in inspector.get_columns("products")}
    assert product_columns["cover_image_asset_id"]["nullable"] is True
    for table_name, column_name in (
        ("source_assets", "canonical_asset_id"),
        ("poster_variants", "canonical_asset_id"),
        ("image_session_assets", "media_object_id"),
    ):
        columns = {column["name"]: column for column in inspector.get_columns(table_name)}
        assert columns[column_name]["nullable"] is True

    asset_indexes = {index["name"]: index for index in inspector.get_indexes("product_image_assets")}
    assert asset_indexes["ix_product_image_assets_product_created"]["column_names"] == [
        "product_id",
        "created_at",
        "id",
    ]
    assert bool(asset_indexes["uq_product_image_assets_product_session_asset"]["unique"])
    asset_foreign_keys = {
        tuple(foreign_key["constrained_columns"]): foreign_key
        for foreign_key in inspector.get_foreign_keys("product_image_assets")
    }
    assert asset_foreign_keys[("product_id",)]["options"]["ondelete"] == "CASCADE"
    assert asset_foreign_keys[("media_object_id",)]["options"]["ondelete"] == "RESTRICT"
    assert asset_foreign_keys[("parent_asset_id",)]["options"]["ondelete"] == "RESTRICT"
    assert asset_foreign_keys[("source_image_session_asset_id",)]["options"]["ondelete"] == "SET NULL"

    with engine.connect() as connection:
        source_mapping = dict(
            connection.execute(sa.text("SELECT id, canonical_asset_id FROM source_assets")).all()
        )
        poster_mapping = dict(
            connection.execute(sa.text("SELECT id, canonical_asset_id FROM poster_variants")).all()
        )
        session_media_id = connection.execute(
            sa.text("SELECT media_object_id FROM image_session_assets WHERE id = 'session-asset-1'")
        ).scalar_one()
        gallery_media_id = connection.execute(
            sa.text(
                "SELECT image_session_assets.media_object_id "
                "FROM image_gallery_entries "
                "JOIN image_session_assets "
                "ON image_session_assets.id = image_gallery_entries.image_session_asset_id "
                "WHERE image_gallery_entries.id = 'gallery-entry-1'"
            )
        ).scalar_one()
        cover_asset_id = connection.execute(
            sa.text("SELECT cover_image_asset_id FROM products WHERE id = 'product-1'")
        ).scalar_one()
        assets = {
            row["id"]: row
            for row in connection.execute(
                sa.text(
                    "SELECT id, product_id, media_object_id, origin_type, display_name, original_filename "
                    "FROM product_image_assets"
                )
            ).mappings()
        }
        media_rows = connection.execute(
            sa.text(
                "SELECT id, storage_path, mime_type, byte_size, width, height, sha256, verification_status, "
                "verified_at FROM media_objects"
            )
        ).mappings().all()

    assert source_mapping == {
        "asset-cross": "asset-cross",
        "asset-original": "asset-original",
        "asset-reference": "asset-reference",
    }
    assert poster_mapping["poster-paired"] == "asset-reference"
    assert poster_mapping["poster-orphan"] == "poster-orphan"
    assert cover_asset_id == "asset-original"
    assert assets["asset-original"]["origin_type"] == "upload"
    assert assets["asset-reference"]["origin_type"] == "workflow_generation"
    assert assets["asset-cross"]["origin_type"] == "legacy_import"
    assert assets["poster-orphan"]["origin_type"] == "legacy_import"
    assert assets["poster-orphan"]["original_filename"] == "poster-orphan.webp"
    assert session_media_id in {row["id"] for row in media_rows}
    assert gallery_media_id == session_media_id
    assert len(media_rows) == 5
    assert all(row["verification_status"] == "legacy_pending" for row in media_rows)
    assert all(row["byte_size"] is None for row in media_rows)
    assert all(row["width"] is None for row in media_rows)
    assert all(row["height"] is None for row in media_rows)
    assert all(row["sha256"] is None for row in media_rows)
    assert all(row["verified_at"] is None for row in media_rows)

    engine.dispose()
    command.downgrade(config, "20260806_0030")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "media_objects" not in inspector.get_table_names()
    assert "product_image_assets" not in inspector.get_table_names()
    assert "cover_image_asset_id" not in {column["name"] for column in inspector.get_columns("products")}
    assert "canonical_asset_id" not in {column["name"] for column in inspector.get_columns("source_assets")}
    assert "canonical_asset_id" not in {column["name"] for column in inspector.get_columns("poster_variants")}
    assert "media_object_id" not in {
        column["name"] for column in inspector.get_columns("image_session_assets")
    }
    with engine.connect() as connection:
        assert connection.execute(sa.text("SELECT COUNT(*) FROM source_assets")).scalar_one() == 3
        assert connection.execute(sa.text("SELECT COUNT(*) FROM poster_variants")).scalar_one() == 2
        assert connection.execute(sa.text("SELECT COUNT(*) FROM image_session_assets")).scalar_one() == 1
        assert connection.execute(sa.text("SELECT COUNT(*) FROM image_gallery_entries")).scalar_one() == 1
    engine.dispose()
    get_settings.cache_clear()


def test_legacy_copy_fields_migrate_to_structured_payload_and_drop_columns(
    tmp_path: Path,
    monkeypatch,
) -> None:
    database_path = tmp_path / "drop-legacy-copy-fields.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260509_0023")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    with engine.begin() as connection:
        now = "2026-05-10 00:00:00"
        connection.execute(
            sa.text(
                """
                INSERT INTO products (
                    id, name, category, price, source_note, current_confirmed_copy_set_id, created_at, updated_at
                )
                VALUES (:id, :name, NULL, NULL, NULL, NULL, :now, :now)
                """
            ),
            {"id": "product-1", "name": "迁移商品", "now": now},
        )
        connection.execute(
            sa.text(
                """
                INSERT INTO copy_sets (
                    id, product_id, creative_brief_id, status,
                    __LEGACY_COPY_COLUMNS__,
                    structured_payload,
                    __MODEL_LEGACY_COLUMNS__,
                    model_structured_payload,
                    provider_name, model_name, prompt_version,
                    edited_at, confirmed_at, created_at, updated_at
                )
                VALUES (
                    :id, :product_id, NULL, :status,
                    :text_value, :points_value, :headline_value, :action_value,
                    NULL,
                    :model_text_value, :model_points_value, :model_headline_value, :model_action_value,
                    NULL,
                    :provider_name, :model_name, :prompt_version,
                    NULL, NULL, :now, :now
                )
                """
                .replace("__LEGACY_COPY_COLUMNS__", ", ".join(LEGACY_COPY_COLUMNS))
                .replace("__MODEL_LEGACY_COLUMNS__", ", ".join(MODEL_LEGACY_COPY_COLUMNS))
            ),
            {
                "id": "copy-set-1",
                "product_id": "product-1",
                "status": "draft",
                "text_value": "旧标题",
                "points_value": '["卖点一", "卖点二"]',
                "headline_value": "旧海报标题",
                "action_value": "立即购买",
                "model_text_value": "模型旧标题",
                "model_points_value": '["模型卖点"]',
                "model_headline_value": "模型海报标题",
                "model_action_value": "模型 CTA",
                "provider_name": "test",
                "model_name": "test",
                "prompt_version": "test",
                "now": now,
            },
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    copy_set_columns = {column["name"] for column in inspector.get_columns("copy_sets")}
    assert not {
        *LEGACY_COPY_COLUMNS,
        *MODEL_LEGACY_COPY_COLUMNS,
    } & copy_set_columns
    assert {"structured_payload", "model_structured_payload"} <= copy_set_columns
    with engine.connect() as connection:
        row = connection.execute(
            sa.text("SELECT structured_payload, model_structured_payload FROM copy_sets WHERE id = :id"),
            {"id": "copy-set-1"},
        ).mappings().one()
    structured_payload = json.loads(row["structured_payload"])
    model_structured_payload = json.loads(row["model_structured_payload"])
    assert structured_payload["version"] == 2
    assert structured_payload["summary"] == "旧海报标题"
    assert structured_payload["content"]["kind"] == "blocks"
    assert [block["text"] for block in structured_payload["content"]["blocks"][:3]] == [
        "旧标题",
        "卖点一",
        "卖点二",
    ]
    assert model_structured_payload["summary"] == "模型海报标题"
    engine.dispose()

    command.downgrade(config, "20260509_0023")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    downgraded_columns = {column["name"] for column in inspector.get_columns("copy_sets")}
    assert {*LEGACY_COPY_COLUMNS, *MODEL_LEGACY_COPY_COLUMNS} <= downgraded_columns
    with engine.connect() as connection:
        row = connection.execute(
            sa.text(
                """
                SELECT __LEGACY_COPY_COLUMNS__, __MODEL_LEGACY_COLUMNS__
                FROM copy_sets
                WHERE id = :id
                """
                .replace("__LEGACY_COPY_COLUMNS__", ", ".join(LEGACY_COPY_COLUMNS))
                .replace("__MODEL_LEGACY_COLUMNS__", ", ".join(MODEL_LEGACY_COPY_COLUMNS))
            ),
            {"id": "copy-set-1"},
        ).mappings().one()
    assert row[LEGACY_COPY_COLUMNS[0]] == "旧标题"
    assert json.loads(row[LEGACY_COPY_COLUMNS[1]]) == ["卖点一", "卖点二"]
    assert row[LEGACY_COPY_COLUMNS[2]] == "旧海报标题"
    assert row[LEGACY_COPY_COLUMNS[3]] == "立即购买"
    assert row[MODEL_LEGACY_COPY_COLUMNS[0]] == "模型旧标题"
    assert json.loads(row[MODEL_LEGACY_COPY_COLUMNS[1]]) == ["模型卖点"]
    assert row[MODEL_LEGACY_COPY_COLUMNS[2]] == "模型海报标题"
    assert row[MODEL_LEGACY_COPY_COLUMNS[3]] == "模型 CTA"
    engine.dispose()
    get_settings.cache_clear()


def test_workflow_run_retryability_migration_supports_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "workflow-run-retryability.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260510_0024")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-05-12 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-1', '重试迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows (id, product_id, title, active, created_at, updated_at) "
                "VALUES ('workflow-1', 'product-1', '迁移工作流', 1, :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at) "
                "VALUES ('run-1', 'workflow-1', 'failed', :now)"
            ),
            {"now": now},
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"]: column for column in inspector.get_columns("workflow_runs")}
    assert columns["is_retryable"]["nullable"] is False
    with engine.connect() as connection:
        retryable = connection.execute(
            sa.text("SELECT is_retryable FROM workflow_runs WHERE id = 'run-1'")
        ).scalar_one()
    assert bool(retryable) is True

    engine.dispose()
    command.downgrade(config, "20260510_0024")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"] for column in inspector.get_columns("workflow_runs")}
    assert "is_retryable" not in columns

    engine.dispose()
    get_settings.cache_clear()


def test_workflow_run_progress_metadata_migration_supports_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "workflow-run-progress-metadata.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260512_0025")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-05-13 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-1', '进度迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows (id, product_id, title, active, created_at, updated_at) "
                "VALUES ('workflow-1', 'product-1', '迁移工作流', 1, :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at, is_retryable) "
                "VALUES ('run-1', 'workflow-1', 'running', :now, 1)"
            ),
            {"now": now},
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"]: column for column in inspector.get_columns("workflow_runs")}
    assert columns["progress_metadata"]["nullable"] is True
    with engine.begin() as connection:
        connection.execute(
            sa.text("UPDATE workflow_runs SET progress_metadata = :metadata WHERE id = 'run-1'"),
            {"metadata": json.dumps({"last_failure_reason": "上次失败"})},
        )
        metadata = connection.execute(
            sa.text("SELECT progress_metadata FROM workflow_runs WHERE id = 'run-1'")
        ).scalar_one()
    assert json.loads(metadata)["last_failure_reason"] == "上次失败"

    engine.dispose()
    command.downgrade(config, "20260512_0025")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"] for column in inspector.get_columns("workflow_runs")}
    assert "progress_metadata" not in columns

    engine.dispose()
    get_settings.cache_clear()


def test_user_canvas_template_migration_schema_and_downgrade_support_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "user-canvas-template-migration.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "user_canvas_templates" in inspector.get_table_names()
    columns = {column["name"]: column for column in inspector.get_columns("user_canvas_templates")}
    assert columns["id"]["nullable"] is False
    assert columns["key"]["nullable"] is False
    assert columns["title"]["nullable"] is False
    assert columns["description"]["nullable"] is True
    assert columns["kind"]["nullable"] is False
    assert columns["schema_version"]["nullable"] is False
    assert columns["template_json"]["nullable"] is False
    assert columns["archived_at"]["nullable"] is True
    assert columns["created_at"]["nullable"] is False
    assert columns["updated_at"]["nullable"] is False
    assert {constraint["name"] for constraint in inspector.get_unique_constraints("user_canvas_templates")} == {None}
    indexes = {index["name"]: index for index in inspector.get_indexes("user_canvas_templates")}
    assert indexes["ix_user_canvas_templates_archived_at"]["column_names"] == ["archived_at"]

    engine.dispose()
    command.downgrade(config, "20260507_0021")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "user_canvas_templates" not in inspector.get_table_names()
    engine.dispose()
    get_settings.cache_clear()


def test_gallery_migration_schema_and_downgrade_support_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "gallery-migration.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "image_gallery_entries" in inspector.get_table_names()
    columns = {column["name"]: column for column in inspector.get_columns("image_gallery_entries")}
    assert columns["id"]["nullable"] is False
    assert columns["image_session_asset_id"]["nullable"] is False
    assert columns["image_session_round_id"]["nullable"] is True
    assert columns["created_at"]["nullable"] is False
    indexes = {index["name"]: index for index in inspector.get_indexes("image_gallery_entries")}
    assert bool(indexes["uq_image_gallery_entries_asset_id"]["unique"])
    assert indexes["uq_image_gallery_entries_asset_id"]["column_names"] == ["image_session_asset_id"]
    assert indexes["ix_image_gallery_entries_round_id"]["column_names"] == ["image_session_round_id"]
    assert indexes["ix_image_gallery_entries_created_at"]["column_names"] == ["created_at"]
    foreign_keys = {tuple(fk["constrained_columns"]): fk for fk in inspector.get_foreign_keys("image_gallery_entries")}
    assert foreign_keys[("image_session_asset_id",)]["referred_table"] == "image_session_assets"
    assert foreign_keys[("image_session_asset_id",)]["options"]["ondelete"] == "CASCADE"
    assert foreign_keys[("image_session_round_id",)]["referred_table"] == "image_session_rounds"
    assert foreign_keys[("image_session_round_id",)]["options"]["ondelete"] == "SET NULL"

    engine.dispose()
    command.downgrade(config, "20260427_0015")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "image_gallery_entries" not in inspector.get_table_names()
    engine.dispose()
    get_settings.cache_clear()


def test_image_session_product_scope_cleanup_migration_supports_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "image-session-product-scope-cleanup.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    workflow_node_run_columns = {column["name"] for column in inspector.get_columns("workflow_node_runs")}
    image_session_columns = {column["name"] for column in inspector.get_columns("image_sessions")}
    assert "image_session_asset_id" not in workflow_node_run_columns
    assert "product_id" not in image_session_columns

    engine.dispose()
    command.downgrade(config, "20260513_0028")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    workflow_node_run_columns = {column["name"] for column in inspector.get_columns("workflow_node_runs")}
    image_session_columns = {column["name"] for column in inspector.get_columns("image_sessions")}
    assert "image_session_asset_id" in workflow_node_run_columns
    assert "product_id" in image_session_columns
    engine.dispose()
    get_settings.cache_clear()


def test_job_runs_drop_migration_and_downgrade_support_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "job-runs-drop.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260428_0016")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "job_runs" in inspector.get_table_names()
    assert "uq_job_runs_one_active_per_product_kind" in {
        index["name"] for index in inspector.get_indexes("job_runs")
    }

    engine.dispose()
    command.upgrade(config, "head")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "job_runs" not in inspector.get_table_names()

    engine.dispose()
    command.downgrade(config, "20260428_0016")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "job_runs" in inspector.get_table_names()
    columns = {column["name"]: column for column in inspector.get_columns("job_runs")}
    assert columns["product_id"]["nullable"] is False
    assert columns["kind"]["nullable"] is False
    assert columns["status"]["nullable"] is False
    assert "uq_job_runs_one_active_per_product_kind" in {
        index["name"] for index in inspector.get_indexes("job_runs")
    }

    engine.dispose()
    get_settings.cache_clear()


def test_image_session_generation_progress_migration_and_downgrade_support_sqlite(
    tmp_path: Path,
    monkeypatch,
) -> None:
    database_path = tmp_path / "image-session-progress.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260428_0017")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-04-28 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO image_sessions (id, title, created_at, updated_at) "
                "VALUES ('session-1', '迁移会话', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_session_generation_tasks "
                "(id, session_id, status, prompt, size, generation_count, created_at, attempts, is_retryable) "
                "VALUES ('task-1', 'session-1', 'running', '旧任务', '1024x1024', 2, :now, 1, 1)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO image_session_generation_tasks "
                "(id, session_id, status, prompt, size, generation_count, created_at, attempts, is_retryable) "
                "VALUES ('failed-task-1', 'session-1', 'failed', '旧失败任务', '1024x1024', 4, :now, 1, 0)"
            ),
            {"now": now},
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"]: column for column in inspector.get_columns("image_session_generation_tasks")}
    assert columns["completed_candidates"]["nullable"] is False
    assert columns["completed_candidates"]["default"] is None
    assert columns["active_candidate_index"]["nullable"] is True
    assert columns["progress_phase"]["nullable"] is True
    assert columns["progress_updated_at"]["nullable"] is True
    assert columns["provider_response_id"]["nullable"] is True
    assert columns["provider_response_status"]["nullable"] is True
    assert columns["progress_metadata"]["nullable"] is True
    with engine.connect() as connection:
        completed_candidates = connection.execute(
            sa.text("SELECT completed_candidates FROM image_session_generation_tasks WHERE id = 'task-1'")
        ).scalar_one()
        failed_task_retryable = connection.execute(
            sa.text("SELECT is_retryable FROM image_session_generation_tasks WHERE id = 'failed-task-1'")
        ).scalar_one()
    assert completed_candidates == 0
    assert bool(failed_task_retryable) is True

    engine.dispose()
    command.downgrade(config, "20260428_0017")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    columns = {column["name"] for column in inspector.get_columns("image_session_generation_tasks")}
    assert "completed_candidates" not in columns
    assert "progress_metadata" not in columns

    engine.dispose()
    get_settings.cache_clear()


def test_alembic_upgrade_removes_legacy_workflow_nodes(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "legacy-workflow.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260424_0009")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-04-24 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-1', '旧工作流商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows (id, product_id, title, active, created_at, updated_at) "
                "VALUES ('workflow-1', 'product-1', '旧工作流', 1, :now, :now)"
            ),
            {"now": now},
        )
        for node_id, node_type in (
            ("context-1", "product_context"),
            ("copy-1", "copy_generation"),
            ("legacy-text-1", "legacy_text"),
            ("image-1", "image_generation"),
            ("legacy-result-1", "legacy_result"),
            ("slot-1", "image_upload"),
        ):
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_nodes "
                    "(id, workflow_id, node_type, title, position_x, position_y, config_json, status, "
                    "created_at, updated_at) "
                    "VALUES (:id, 'workflow-1', :node_type, :id, 0, 0, '{}', 'idle', :now, :now)"
                ),
                {"id": node_id, "node_type": node_type, "now": now},
            )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_edges "
                "(id, workflow_id, source_node_id, target_node_id, source_handle, target_handle, created_at) "
                "VALUES "
                "('edge-old-target', 'workflow-1', 'copy-1', 'legacy-text-1', 'output', 'input', :now), "
                "('edge-old-source', 'workflow-1', 'legacy-result-1', 'image-1', 'output', 'input', :now), "
                "('edge-supported', 'workflow-1', 'context-1', 'copy-1', 'output', 'input', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at) "
                "VALUES ('run-1', 'workflow-1', 'running', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_node_runs (id, workflow_run_id, node_id, status, started_at) "
                "VALUES "
                "('node-run-old', 'run-1', 'legacy-text-1', 'succeeded', :now), "
                "('node-run-supported', 'run-1', 'copy-1', 'succeeded', :now)"
            ),
            {"now": now},
        )

    command.upgrade(config, "head")

    with engine.connect() as connection:
        node_types = connection.execute(sa.text("SELECT node_type FROM workflow_nodes ORDER BY id")).scalars().all()
        edge_ids = connection.execute(sa.text("SELECT id FROM workflow_edges ORDER BY id")).scalars().all()
        node_run_ids = connection.execute(sa.text("SELECT id FROM workflow_node_runs ORDER BY id")).scalars().all()

    assert node_types == ["product_context", "copy_generation", "image_generation", "reference_image"]
    assert edge_ids == ["edge-supported"]
    assert node_run_ids == ["node-run-supported"]
    get_settings.cache_clear()


def test_disjoint_workflow_node_run_migration_constraints_support_sqlite(
    tmp_path: Path,
    monkeypatch,
) -> None:
    database_path = tmp_path / "workflow-node-run-constraints.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260507_0020")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-05-07 00:21:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-1', '节点并行迁移商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows (id, product_id, title, active, created_at, updated_at) "
                "VALUES ('workflow-1', 'product-1', '节点并行迁移工作流', 1, :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_nodes "
                "(id, workflow_id, node_type, title, position_x, position_y, "
                "config_json, status, created_at, updated_at) "
                "VALUES "
                "('node-1', 'workflow-1', 'copy_generation', '文案', 0, 0, '{}', 'queued', :now, :now), "
                "('node-2', 'workflow-1', 'image_generation', '生图', 100, 0, '{}', 'queued', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at) "
                "VALUES ('run-1', 'workflow-1', 'running', :now)"
            ),
            {"now": now},
        )

    engine.dispose()
    command.upgrade(config, "head")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    indexes = {index["name"]: index for index in inspector.get_indexes("workflow_node_runs")}
    assert "uq_workflow_node_runs_one_active_per_node" in indexes
    assert bool(indexes["uq_workflow_node_runs_one_active_per_node"]["unique"])
    assert indexes["uq_workflow_node_runs_one_active_per_node"]["column_names"] == ["node_id"]
    workflow_run_indexes = {index["name"] for index in inspector.get_indexes("workflow_runs")}
    assert "uq_workflow_runs_one_running_per_workflow" not in workflow_run_indexes

    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at) "
                "VALUES ('run-2', 'workflow-1', 'running', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_node_runs (id, workflow_run_id, node_id, status, started_at) "
                "VALUES "
                "('node-run-1', 'run-1', 'node-1', 'queued', :now), "
                "('node-run-2', 'run-2', 'node-2', 'running', :now)"
            ),
            {"now": now},
        )

    with pytest.raises(sa.exc.IntegrityError):
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_node_runs (id, workflow_run_id, node_id, status, started_at) "
                    "VALUES ('node-run-duplicate', 'run-2', 'node-1', 'running', :now)"
                ),
                {"now": now},
            )

    engine.dispose()
    command.downgrade(config, "20260507_0020")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    indexes = {index["name"]: index for index in inspector.get_indexes("workflow_runs")}
    assert "uq_workflow_runs_one_running_per_workflow" in indexes
    node_run_indexes = {index["name"] for index in inspector.get_indexes("workflow_node_runs")}
    assert "uq_workflow_node_runs_one_active_per_node" not in node_run_indexes
    with engine.connect() as connection:
        active_run_count = connection.execute(
            sa.text("SELECT COUNT(*) FROM workflow_runs WHERE workflow_id = 'workflow-1' AND status = 'running'")
        ).scalar_one()
        failed_run_count = connection.execute(
            sa.text("SELECT COUNT(*) FROM workflow_runs WHERE workflow_id = 'workflow-1' AND status = 'failed'")
        ).scalar_one()
        duplicate_run_active_node_runs = connection.execute(
            sa.text(
                "SELECT COUNT(*) FROM workflow_node_runs "
                "WHERE workflow_run_id = 'run-1' AND status IN ('queued', 'running')"
            )
        ).scalar_one()
    assert active_run_count == 1
    assert failed_run_count == 1
    assert duplicate_run_active_node_runs == 0
    engine.dispose()
    get_settings.cache_clear()
