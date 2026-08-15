from __future__ import annotations

import csv
import io
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Literal

import sqlalchemy as sa
from sqlalchemy.orm import Session

from productflow_backend.application.legacy_retirement.contracts import (
    LegacyArchiveAssetDeclaration,
    LegacyArchiveBackfillItemResult,
    LegacyArchiveBackfillReport,
    LegacyArchiveExportPage,
    LegacyArchiveSnapshot,
    archive_backfill_report_sha256,
    archive_export_page_sha256,
    canonical_sha256,
)
from productflow_backend.application.legacy_retirement.profiles import (
    CURRENT_ARCHIVE_PROFILE,
    LEGACY_CANVAS_PROFILE,
    RELEVANT_TABLES,
    UNKNOWN_PROFILE,
    recognize_profile,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import (
    ImageSessionAsset,
    LegacyCanvasAgentArchive,
    LegacyUserTemplateArchive,
    LegacyWorkflowArchive,
    LegacyWorkflowArchiveAsset,
    PosterVariant,
    Product,
    ProductImageAsset,
    SourceAsset,
)

_PlanStatus = Literal["would_create", "unchanged", "blocked"]


@dataclass(slots=True)
class _ResolvedAsset:
    declaration: LegacyArchiveAssetDeclaration
    asset: ProductImageAsset


@dataclass(slots=True)
class _ItemPlan:
    snapshot: LegacyArchiveSnapshot
    status: _PlanStatus
    archive_id: str | None
    resolved_assets: list[_ResolvedAsset]
    diagnostic_codes: list[str]


def backfill_legacy_archive_page(
    session: Session,
    *,
    page: LegacyArchiveExportPage,
    expected_source_report_sha256: str,
    apply: bool = False,
    allow_legacy_bridge: bool = False,
    generated_at: datetime | None = None,
) -> LegacyArchiveBackfillReport:
    if archive_export_page_sha256(page) != page.page_sha256:
        raise BusinessValidationError("归档导出 page hash 校验失败")
    if expected_source_report_sha256 != page.source_report_sha256:
        raise BusinessValidationError("归档导出 page 与批准的 source report hash 不一致")
    if any(snapshot.kind != page.kind or snapshot.source_profile != page.source_profile for snapshot in page.items):
        raise BusinessValidationError("归档导出 page 包含不匹配的 item kind")
    for snapshot in page.items:
        if canonical_sha256(snapshot.payload_json) != snapshot.payload_sha256:
            raise BusinessValidationError(f"归档快照 payload hash 校验失败: {snapshot.source_id}")

    target_profile = _target_schema_profile(session)
    global_blockers = set(page.blocking_issue_codes)
    if allow_legacy_bridge and page.source_profile == LEGACY_CANVAS_PROFILE:
        global_blockers.discard("migration_bridge_required")
    if page.source_profile == LEGACY_CANVAS_PROFILE and not allow_legacy_bridge:
        global_blockers.add("legacy_bridge_not_approved")
    if target_profile != CURRENT_ARCHIVE_PROFILE:
        global_blockers.add("target_archive_schema_profile_required")

    plans = (
        [_plan_item(session, snapshot) for snapshot in page.items]
        if target_profile == CURRENT_ARCHIVE_PROFILE
        else [_blocked_target_plan(snapshot) for snapshot in page.items]
    )
    item_blocked = any(plan.status == "blocked" for plan in plans)
    can_apply = not global_blockers and not item_blocked
    applied = False
    if apply and can_apply:
        try:
            for plan in plans:
                if plan.status == "would_create":
                    plan.archive_id = _create_archive(session, plan)
            session.commit()
        except Exception:
            session.rollback()
            raise
        applied = True

    item_results = [
        LegacyArchiveBackfillItemResult(
            kind=plan.snapshot.kind,
            source_id=plan.snapshot.source_id,
            status=("created" if applied and plan.status == "would_create" else plan.status),
            archive_id=plan.archive_id,
            resolved_asset_count=len(plan.resolved_assets),
            diagnostic_codes=plan.diagnostic_codes,
        )
        for plan in plans
    ]
    timestamp = generated_at or datetime.now(UTC)
    provisional = LegacyArchiveBackfillReport(
        generated_at=timestamp,
        apply_requested=apply,
        applied=applied,
        allow_legacy_bridge=allow_legacy_bridge,
        source_profile=page.source_profile,
        source_report_sha256=page.source_report_sha256,
        source_page_sha256=page.page_sha256,
        target_profile=target_profile,
        global_blocking_issue_codes=sorted(global_blockers),
        created_count=sum(item.status == "created" for item in item_results),
        unchanged_count=sum(item.status == "unchanged" for item in item_results),
        blocked_count=sum(item.status == "blocked" for item in item_results),
        items=item_results,
        report_sha256="0" * 64,
    )
    return provisional.model_copy(update={"report_sha256": archive_backfill_report_sha256(provisional)})


def render_archive_backfill_csv(report: LegacyArchiveBackfillReport) -> str:
    output = io.StringIO(newline="")
    writer = csv.DictWriter(
        output,
        fieldnames=(
            "kind",
            "source_id",
            "status",
            "archive_id",
            "resolved_asset_count",
            "diagnostic_codes",
        ),
    )
    writer.writeheader()
    for item in report.items:
        writer.writerow(
            {
                "kind": item.kind,
                "source_id": item.source_id,
                "status": item.status,
                "archive_id": item.archive_id or "",
                "resolved_asset_count": item.resolved_asset_count,
                "diagnostic_codes": ",".join(item.diagnostic_codes),
            }
        )
    return output.getvalue()


def _target_schema_profile(session: Session) -> str:
    connection = session.connection()
    inspector = sa.inspect(connection)
    table_names = frozenset(inspector.get_table_names())
    if "alembic_version" not in table_names:
        return UNKNOWN_PROFILE
    revision_rows = connection.execute(
        sa.text("SELECT version_num FROM alembic_version ORDER BY version_num")
    ).scalars()
    revisions = [str(value) for value in revision_rows]
    columns_by_table = {
        table_name: frozenset(str(column["name"]) for column in inspector.get_columns(table_name))
        for table_name in RELEVANT_TABLES
        if table_name in table_names
    }
    profile, _diagnostics = recognize_profile(revisions, columns_by_table, table_names)
    return profile


def _plan_item(session: Session, snapshot: LegacyArchiveSnapshot) -> _ItemPlan:
    diagnostics: list[str] = []
    if snapshot.kind in {"workflow", "canvas_agent_thread"}:
        if snapshot.product_id is None or session.get(Product, snapshot.product_id) is None:
            diagnostics.append("target_product_missing")

    resolved_assets: list[_ResolvedAsset] = []
    if snapshot.kind == "workflow" and not diagnostics:
        for declaration in snapshot.asset_declarations:
            resolved, error_code = _resolve_asset(session, declaration)
            if error_code is not None:
                diagnostics.append(error_code)
            elif resolved is not None:
                resolved_assets.append(resolved)

    existing = _load_existing_archive(session, snapshot)
    if existing is not None:
        archive_id, matches = _existing_archive_matches(existing, snapshot, resolved_assets)
        if not matches:
            diagnostics.append("existing_archive_snapshot_drift")
        return _ItemPlan(
            snapshot=snapshot,
            status="blocked" if diagnostics else "unchanged",
            archive_id=archive_id,
            resolved_assets=resolved_assets,
            diagnostic_codes=sorted(set(diagnostics)),
        )
    return _ItemPlan(
        snapshot=snapshot,
        status="blocked" if diagnostics else "would_create",
        archive_id=None,
        resolved_assets=resolved_assets,
        diagnostic_codes=sorted(set(diagnostics)),
    )


def _blocked_target_plan(snapshot: LegacyArchiveSnapshot) -> _ItemPlan:
    return _ItemPlan(
        snapshot=snapshot,
        status="blocked",
        archive_id=None,
        resolved_assets=[],
        diagnostic_codes=["target_archive_schema_profile_required"],
    )


def _resolve_asset(
    session: Session,
    declaration: LegacyArchiveAssetDeclaration,
) -> tuple[_ResolvedAsset | None, str | None]:
    if declaration.legacy_source_type == "image_session_asset":
        source = session.get(ImageSessionAsset, declaration.legacy_source_id)
        if source is None:
            return None, "target_legacy_asset_source_missing"
        if source.media_object_id is None:
            return None, "target_legacy_asset_media_mapping_missing"
        asset = session.scalar(
            sa.select(ProductImageAsset).where(
                ProductImageAsset.product_id == declaration.product_id,
                ProductImageAsset.source_image_session_asset_id == declaration.legacy_source_id,
            )
        )
        if asset is None:
            return None, "target_canonical_asset_mapping_missing"
        if source.media_object_id != asset.media_object_id:
            return None, "target_canonical_asset_mapping_drift"
        if (
            declaration.source_canonical_asset_id is not None
            and declaration.source_canonical_asset_id != asset.id
        ):
            return None, "target_canonical_asset_mapping_drift"
        return _ResolvedAsset(declaration=declaration, asset=asset), None
    if declaration.legacy_source_type == "source_asset":
        source = session.get(SourceAsset, declaration.legacy_source_id)
    else:
        source = session.get(PosterVariant, declaration.legacy_source_id)
    if source is None:
        return None, "target_legacy_asset_source_missing"
    if source.product_id != declaration.product_id:
        return None, "target_legacy_asset_source_cross_product"
    canonical_asset_id = source.canonical_asset_id
    if canonical_asset_id is None:
        return None, "target_canonical_asset_mapping_missing"
    if (
        declaration.source_canonical_asset_id is not None
        and declaration.source_canonical_asset_id != canonical_asset_id
    ):
        return None, "target_canonical_asset_mapping_drift"
    asset = session.get(ProductImageAsset, canonical_asset_id)
    if asset is None:
        return None, "target_canonical_asset_missing"
    if asset.product_id != declaration.product_id:
        return None, "target_canonical_asset_cross_product"
    return _ResolvedAsset(declaration=declaration, asset=asset), None


def _load_existing_archive(session: Session, snapshot: LegacyArchiveSnapshot) -> Any | None:
    if snapshot.kind == "workflow":
        return session.scalar(
            sa.select(LegacyWorkflowArchive).where(
                LegacyWorkflowArchive.source_profile == snapshot.source_profile,
                LegacyWorkflowArchive.legacy_workflow_id == snapshot.source_id,
            )
        )
    if snapshot.kind == "user_template":
        return session.scalar(
            sa.select(LegacyUserTemplateArchive).where(
                LegacyUserTemplateArchive.source_profile == _snapshot_source_profile(snapshot),
                LegacyUserTemplateArchive.legacy_template_id == snapshot.source_id,
            )
        )
    return session.scalar(
        sa.select(LegacyCanvasAgentArchive).where(
            LegacyCanvasAgentArchive.source_profile == _snapshot_source_profile(snapshot),
            LegacyCanvasAgentArchive.legacy_thread_id == snapshot.source_id,
        )
    )


def _existing_archive_matches(
    existing: Any,
    snapshot: LegacyArchiveSnapshot,
    resolved_assets: list[_ResolvedAsset],
) -> tuple[str, bool]:
    common_matches = (
        existing.source_fingerprint_sha256 == snapshot.source_fingerprint_sha256
        and existing.payload_sha256 == snapshot.payload_sha256
        and canonical_sha256(existing.payload_json) == snapshot.payload_sha256
    )
    if snapshot.kind == "workflow":
        expected_relations = {
            (
                item.asset.id,
                item.declaration.role,
                item.declaration.legacy_source_type,
                item.declaration.legacy_source_id,
            )
            for item in resolved_assets
        }
        actual_relations = {
            (
                item.product_image_asset_id,
                item.role,
                item.legacy_source_type,
                item.legacy_source_id,
            )
            for item in existing.assets
        }
        counts_match = (
            existing.node_count == _snapshot_count(snapshot, "node_count")
            and existing.edge_count == _snapshot_count(snapshot, "edge_count")
            and existing.run_count == _snapshot_count(snapshot, "run_count")
            and existing.node_run_count == _snapshot_count(snapshot, "node_run_count")
            and existing.asset_count == len(resolved_assets)
        )
        return existing.id, common_matches and counts_match and actual_relations == expected_relations
    if snapshot.kind == "canvas_agent_thread":
        counts_match = all(
            getattr(existing, key) == _snapshot_count(snapshot, key)
            for key in (
                "message_count",
                "run_count",
                "tool_event_count",
                "plan_count",
                "task_plan_count",
                "timeline_event_count",
                "visible_event_count",
                "technical_event_count",
            )
        )
        return existing.id, common_matches and counts_match
    return existing.id, common_matches


def _create_archive(session: Session, plan: _ItemPlan) -> str:
    snapshot = plan.snapshot
    source_profile = _snapshot_source_profile(snapshot)
    if snapshot.kind == "workflow":
        archive = LegacyWorkflowArchive(
            source_profile=source_profile,
            legacy_workflow_id=snapshot.source_id,
            product_id=_required_product_id(snapshot),
            source_title=snapshot.title,
            source_updated_at=snapshot.source_updated_at,
            archive_schema_version=1,
            payload_json=snapshot.payload_json,
            source_fingerprint_sha256=snapshot.source_fingerprint_sha256,
            payload_sha256=snapshot.payload_sha256,
            node_count=_snapshot_count(snapshot, "node_count"),
            edge_count=_snapshot_count(snapshot, "edge_count"),
            run_count=_snapshot_count(snapshot, "run_count"),
            node_run_count=_snapshot_count(snapshot, "node_run_count"),
            asset_count=len(plan.resolved_assets),
        )
        archive.assets = [
            LegacyWorkflowArchiveAsset(
                asset=item.asset,
                role=item.declaration.role,
                legacy_source_type=item.declaration.legacy_source_type,
                legacy_source_id=item.declaration.legacy_source_id,
            )
            for item in plan.resolved_assets
        ]
        session.add(archive)
    elif snapshot.kind == "user_template":
        template = _payload_mapping(snapshot, "template")
        archive = LegacyUserTemplateArchive(
            source_profile=source_profile,
            legacy_template_id=snapshot.source_id,
            legacy_key=str(template.get("key") or snapshot.source_id),
            title=snapshot.title,
            description=str(template["description"]) if template.get("description") is not None else None,
            archive_status=(
                "damaged"
                if any(diagnostic.code == "invalid_user_template_payload" for diagnostic in snapshot.diagnostics)
                else "archived"
            ),
            archive_schema_version=1,
            payload_json=snapshot.payload_json,
            diagnostics_json=[diagnostic.model_dump(mode="json") for diagnostic in snapshot.diagnostics],
            source_fingerprint_sha256=snapshot.source_fingerprint_sha256,
            payload_sha256=snapshot.payload_sha256,
            source_updated_at=snapshot.source_updated_at,
        )
        session.add(archive)
    else:
        thread = _payload_mapping(snapshot, "thread")
        archive = LegacyCanvasAgentArchive(
            source_profile=source_profile,
            legacy_thread_id=snapshot.source_id,
            product_id=_required_product_id(snapshot),
            title=snapshot.title,
            source_status=str(thread.get("status") or "unknown"),
            source_updated_at=snapshot.source_updated_at,
            archive_schema_version=1,
            payload_json=snapshot.payload_json,
            source_fingerprint_sha256=snapshot.source_fingerprint_sha256,
            payload_sha256=snapshot.payload_sha256,
            message_count=_snapshot_count(snapshot, "message_count"),
            run_count=_snapshot_count(snapshot, "run_count"),
            tool_event_count=_snapshot_count(snapshot, "tool_event_count"),
            plan_count=_snapshot_count(snapshot, "plan_count"),
            task_plan_count=_snapshot_count(snapshot, "task_plan_count"),
            timeline_event_count=_snapshot_count(snapshot, "timeline_event_count"),
            visible_event_count=_snapshot_count(snapshot, "visible_event_count"),
            technical_event_count=_snapshot_count(snapshot, "technical_event_count"),
        )
        session.add(archive)
    session.flush()
    return archive.id


def _snapshot_source_profile(snapshot: LegacyArchiveSnapshot) -> str:
    return snapshot.source_profile


def _snapshot_count(snapshot: LegacyArchiveSnapshot, key: str) -> int:
    value = snapshot.counts.get(key)
    if value is None:
        raise BusinessValidationError(f"归档快照缺少计数字段 {key}: {snapshot.source_id}")
    return value


def _required_product_id(snapshot: LegacyArchiveSnapshot) -> str:
    if snapshot.product_id is None:
        raise BusinessValidationError(f"归档快照缺少 product_id: {snapshot.source_id}")
    return snapshot.product_id


def _payload_mapping(snapshot: LegacyArchiveSnapshot, key: str) -> dict[str, Any]:
    value = snapshot.payload_json.get(key)
    if not isinstance(value, dict):
        raise BusinessValidationError(f"归档快照缺少 {key}: {snapshot.source_id}")
    return value


__all__ = ["backfill_legacy_archive_page", "render_archive_backfill_csv"]
