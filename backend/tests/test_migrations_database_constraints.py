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
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    ImageGalleryEntry,
    ImageSessionAsset,
    ImageSessionGenerationTask,
    MediaObject,
    PosterVariant,
    Product,
    ProductImageAsset,
    SourceAsset,
    UserCanvasTemplate,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
    new_id,
    utcnow,
)

MODEL_LEGACY_COPY_COLUMNS = [
    "model_" + suffix
    for suffix in ("title", "selling" + "_points", "poster" + "_headline", "c" + "ta")
]
LEGACY_COPY_COLUMNS = ["title", "selling" + "_points", "poster" + "_headline", "c" + "ta"]


def test_sqlalchemy_enum_columns_use_database_values() -> None:
    assert SourceAsset.__table__.c.kind.type.enums == [member.value for member in SourceAssetKind]
    assert ImageSessionAsset.__table__.c.kind.type.enums == [member.value for member in ImageSessionAssetKind]
    assert CopySet.__table__.c.status.type.enums == [member.value for member in CopyStatus]
    assert PosterVariant.__table__.c.kind.type.enums == [member.value for member in PosterKind]
    assert ImageSessionGenerationTask.__table__.c.status.type.enums == [member.value for member in JobStatus]
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
    assert asset_table.c.parent_asset_id.nullable
    assert asset_table.c.source_image_session_asset_id.nullable
    assert {index.name for index in asset_table.indexes} == {
        "ix_product_image_assets_product_created",
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

    for table, column_name, constraint_name in (
        (SourceAsset.__table__, "canonical_asset_id", "fk_source_assets_canonical_asset_id"),
        (PosterVariant.__table__, "canonical_asset_id", "fk_poster_variants_canonical_asset_id"),
        (ImageSessionAsset.__table__, "media_object_id", "fk_image_session_assets_media_object_id"),
    ):
        assert table.c[column_name].nullable
        foreign_key = next(fk for fk in table.foreign_keys if fk.parent.name == column_name)
        assert foreign_key.constraint.name == constraint_name
        assert foreign_key.ondelete == "RESTRICT"


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
