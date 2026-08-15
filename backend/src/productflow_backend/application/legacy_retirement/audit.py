from __future__ import annotations

from collections.abc import Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import sqlalchemy as sa
from sqlalchemy.engine import Connection, Engine

from productflow_backend.application.legacy_retirement.contracts import (
    AuditIssue,
    CanvasAgentAuditSummary,
    DatabaseSourceSummary,
    LegacyRetirementAuditReport,
    MediaAuditSummary,
    WorkflowAuditSummary,
    canonical_json_bytes,
    canonical_sha256,
    report_sha256,
)
from productflow_backend.application.legacy_retirement.integrity import INTEGRITY_KEYS, audit_integrity
from productflow_backend.application.legacy_retirement.profiles import (
    CANVAS_RUN_ACTIVE_STATUSES,
    CANVAS_RUN_KNOWN_STATUSES,
    CURRENT_ARCHIVE_PROFILE,
    CURRENT_CANONICAL_PROFILE,
    CURRENT_CUTOVER_PROFILE,
    LEGACY_CANVAS_PROFILE,
    RELEVANT_TABLES,
    UNKNOWN_PROFILE,
    VISIBLE_CANVAS_EVENT_TYPES,
    WORKFLOW_NODE_ACTIVE_STATUSES,
    WORKFLOW_NODE_KNOWN_STATUSES,
    WORKFLOW_RUN_ACTIVE_STATUSES,
    WORKFLOW_RUN_KNOWN_STATUSES,
    recognize_profile,
)
from productflow_backend.application.legacy_retirement.source import open_legacy_read_only_connection
from productflow_backend.application.legacy_retirement.summaries import (
    audit_canvas_agent,
    audit_media,
    audit_user_templates,
    audit_workflows,
    empty_canvas_summary,
    empty_template_summary,
    empty_workflow_summary,
)


def audit_legacy_retirement(
    engine: Engine,
    *,
    storage_root: Path,
    generated_at: datetime | None = None,
) -> LegacyRetirementAuditReport:
    """Inspect a supported ProductFlow database without permitting database writes."""

    timestamp = generated_at or datetime.now(UTC)
    with open_legacy_read_only_connection(engine) as connection:
        return audit_legacy_retirement_connection(
            connection,
            storage_root=storage_root,
            generated_at=timestamp,
        )


def audit_legacy_retirement_connection(
    connection: Connection,
    *,
    storage_root: Path,
    generated_at: datetime,
) -> LegacyRetirementAuditReport:
    """Audit an already-enforced read-only source connection."""

    if connection.dialect.name == "postgresql":
        read_only_enforced = connection.exec_driver_sql("SHOW transaction_read_only").scalar_one() == "on"
    elif connection.dialect.name == "sqlite":
        read_only_enforced = int(connection.exec_driver_sql("PRAGMA query_only").scalar_one()) == 1
    else:
        read_only_enforced = False
    if not read_only_enforced:
        raise RuntimeError("遗留资产审计连接不是只读连接")

    inspector = sa.inspect(connection)
    table_names = frozenset(inspector.get_table_names())
    columns_by_table = {
        table_name: frozenset(str(column["name"]) for column in inspector.get_columns(table_name))
        for table_name in RELEVANT_TABLES
        if table_name in table_names
    }
    revisions = _load_alembic_revisions(connection, table_names)
    profile, diagnostics = recognize_profile(revisions, columns_by_table, table_names)
    table_counts = _load_table_counts(connection, table_names)
    key_table_columns = {
        table_name: sorted(columns)
        for table_name, columns in sorted(columns_by_table.items())
    }

    source = DatabaseSourceSummary(
        dialect=connection.dialect.name,
        read_only_enforced=read_only_enforced,
        alembic_revisions=revisions,
        schema_profile=profile,
        profile_diagnostics=diagnostics,
        key_table_columns=key_table_columns,
        table_counts=table_counts,
    )

    if profile == UNKNOWN_PROFILE:
        workflows = empty_workflow_summary()
        templates = empty_template_summary()
        canvas_agent = empty_canvas_summary(present="canvas_agent_threads" in table_names)
        media = audit_media(profile, {}, storage_root)
        integrity_counts = {key: 0 for key in INTEGRITY_KEYS}
        source_fingerprint = canonical_sha256(
            {
                "alembic_revisions": revisions,
                "key_table_columns": key_table_columns,
                "table_counts": table_counts,
            }
        )
    else:
        rows_by_table = _load_rows(connection, table_names)
        workflows = audit_workflows(profile, rows_by_table)
        templates = audit_user_templates(rows_by_table)
        canvas_agent = audit_canvas_agent(rows_by_table, table_names)
        media = audit_media(profile, rows_by_table, storage_root)
        integrity_counts = audit_integrity(rows_by_table)
        source_fingerprint = canonical_sha256(
            {table_name: rows_by_table[table_name] for table_name in sorted(rows_by_table)}
        )

    issues = _build_issues(
        source=source,
        workflows=workflows,
        canvas_agent=canvas_agent,
        media=media,
        integrity_counts=integrity_counts,
    )
    provisional = LegacyRetirementAuditReport(
        generated_at=generated_at,
        source=source,
        workflows=workflows,
        user_templates=templates,
        canvas_agent=canvas_agent,
        media=media,
        integrity_counts=integrity_counts,
        issues=issues,
        ready_for_archive=not any(issue.severity == "blocking" for issue in issues),
        source_fingerprint_sha256=source_fingerprint,
        report_sha256="0" * 64,
    )
    return provisional.model_copy(update={"report_sha256": report_sha256(provisional)})


def _load_alembic_revisions(connection: Connection, table_names: frozenset[str]) -> list[str]:
    if "alembic_version" not in table_names:
        return []
    rows = connection.execute(sa.text("SELECT version_num FROM alembic_version ORDER BY version_num")).scalars()
    return [str(value) for value in rows]


def _load_table_counts(connection: Connection, table_names: frozenset[str]) -> dict[str, int]:
    metadata = sa.MetaData()
    counts: dict[str, int] = {}
    for table_name in RELEVANT_TABLES:
        if table_name not in table_names:
            continue
        table = sa.Table(table_name, metadata, autoload_with=connection)
        counts[table_name] = int(connection.scalar(sa.select(sa.func.count()).select_from(table)) or 0)
    return counts


def _load_rows(connection: Connection, table_names: frozenset[str]) -> dict[str, list[dict[str, Any]]]:
    metadata = sa.MetaData()
    rows_by_table: dict[str, list[dict[str, Any]]] = {}
    for table_name in RELEVANT_TABLES:
        if table_name not in table_names:
            continue
        table = sa.Table(table_name, metadata, autoload_with=connection)
        rows = [dict(row) for row in connection.execute(sa.select(table)).mappings()]
        rows.sort(key=canonical_json_bytes)
        rows_by_table[table_name] = rows
    return rows_by_table


def _build_issues(
    *,
    source: DatabaseSourceSummary,
    workflows: WorkflowAuditSummary,
    canvas_agent: CanvasAgentAuditSummary,
    media: MediaAuditSummary,
    integrity_counts: Mapping[str, int],
) -> list[AuditIssue]:
    issues: list[AuditIssue] = []
    if source.schema_profile == UNKNOWN_PROFILE:
        issues.append(
            AuditIssue(
                code="schema_profile_unknown",
                severity="blocking",
                count=1,
                detail="Alembic revision 与关键表签名未匹配受支持 profile，禁止迁移、stamp 或归档写入。",
            )
        )
    elif source.schema_profile == LEGACY_CANVAS_PROFILE:
        issues.append(
            AuditIssue(
                code="migration_bridge_required",
                severity="blocking",
                count=1,
                detail="该旧 Canvas Agent revision 不在现行迁移图中，必须先批准并演练专用桥接方案。",
            )
        )

    integrity_total = sum(integrity_counts.values())
    if integrity_total:
        issues.append(
            AuditIssue(
                code="source_integrity_mismatch",
                severity="blocking",
                count=integrity_total,
                detail="检测到 dangling、orphan 或跨商品/工作流引用，归档映射前必须逐项处理。",
            )
        )
    legacy_workflows = [item for item in workflows.items if item.archive_candidate]
    active_workflow_runs = sum(
        item.run_status_counts.get(status, 0)
        for item in legacy_workflows
        for status in WORKFLOW_RUN_ACTIVE_STATUSES
    )
    active_node_runs = sum(
        item.node_run_status_counts.get(status, 0)
        for item in legacy_workflows
        for status in WORKFLOW_NODE_ACTIVE_STATUSES
    )
    if active_workflow_runs + active_node_runs:
        issues.append(
            AuditIssue(
                code="active_workflow_execution",
                severity="blocking",
                count=active_workflow_runs + active_node_runs,
                detail="仍有活动中的 v1 workflow/node run，维护窗口归档必须等待其结束。",
            )
        )
    unknown_workflow_statuses = sum(
        count
        for item in legacy_workflows
        for status, count in item.run_status_counts.items()
        if status not in WORKFLOW_RUN_KNOWN_STATUSES
    ) + sum(
        count
        for item in legacy_workflows
        for status, count in item.node_run_status_counts.items()
        if status not in WORKFLOW_NODE_KNOWN_STATUSES
    )
    if unknown_workflow_statuses:
        issues.append(
            AuditIssue(
                code="unknown_workflow_execution_status",
                severity="blocking",
                count=unknown_workflow_statuses,
                detail="存在审计器不认识的 workflow/node run 状态，不能推断其已终止。",
            )
        )

    active_canvas_runs = sum(
        canvas_agent.run_status_counts.get(status, 0) for status in CANVAS_RUN_ACTIVE_STATUSES
    )
    if active_canvas_runs:
        issues.append(
            AuditIssue(
                code="active_canvas_agent_run",
                severity="blocking",
                count=active_canvas_runs,
                detail="旧 Canvas Agent 仍有活动或待审批 run，必须保留并完成处理后再切换。",
            )
        )
    unknown_canvas_statuses = sum(
        count for status, count in canvas_agent.run_status_counts.items() if status not in CANVAS_RUN_KNOWN_STATUSES
    )
    if unknown_canvas_statuses:
        issues.append(
            AuditIssue(
                code="unknown_canvas_agent_run_status",
                severity="blocking",
                count=unknown_canvas_statuses,
                detail="存在审计器不认识的旧 Canvas Agent run 状态，不能视作终态。",
            )
        )
    missing_terminal_evidence = sum(item.runs_without_terminal_evidence for item in canvas_agent.items)
    if missing_terminal_evidence:
        issues.append(
            AuditIssue(
                code="canvas_agent_terminal_evidence_missing",
                severity="warning",
                count=missing_terminal_evidence,
                detail="部分旧 Agent run 没有可见终态事件；归档时保留 run 状态并标记诊断。",
            )
        )

    if media.declared_record_count and not media.storage_root_exists:
        issues.append(
            AuditIssue(
                code="storage_root_unavailable",
                severity="blocking",
                count=media.declared_record_count,
                detail="配置的 STORAGE_ROOT 不存在或不是目录，本次媒体对账不可作为切换证据。",
            )
        )
    if media.invalid_path_record_count:
        issues.append(
            AuditIssue(
                code="invalid_storage_path",
                severity="blocking",
                count=media.invalid_path_record_count,
                detail="存在绝对路径、空路径或越界路径，禁止据此建立历史下载关系。",
            )
        )
    if media.missing_record_count:
        issues.append(
            AuditIssue(
                code="media_file_missing",
                severity="warning",
                count=media.missing_record_count,
                detail="声明的媒体文件在 STORAGE_ROOT 下缺失；归档需标记 unavailable，不能伪造下载成功。",
            )
        )
    return sorted(issues, key=lambda issue: (issue.severity != "blocking", issue.code))


__all__ = [
    "CURRENT_CANONICAL_PROFILE",
    "CURRENT_ARCHIVE_PROFILE",
    "CURRENT_CUTOVER_PROFILE",
    "LEGACY_CANVAS_PROFILE",
    "UNKNOWN_PROFILE",
    "VISIBLE_CANVAS_EVENT_TYPES",
    "audit_legacy_retirement",
    "audit_legacy_retirement_connection",
]
