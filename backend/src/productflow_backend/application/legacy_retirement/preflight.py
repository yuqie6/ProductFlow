from __future__ import annotations

import hashlib
import json
import os
from collections import Counter
from collections.abc import Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import sqlalchemy as sa
from sqlalchemy.engine import Connection, Engine

from productflow_backend.application.legacy_retirement.audit import audit_legacy_retirement_connection
from productflow_backend.application.legacy_retirement.contracts import (
    AuditIssue,
    ConfigurationValueFingerprint,
    LegacyCutoverPreflightReport,
    LegacyProviderConfigurationPreflightSummary,
    LegacyRetirementAuditReport,
    LegacyV1ExecutionPreflightSummary,
    LegacyV1FreezePreflightSummary,
    ProviderBindingPreflightSummary,
    ProviderProfilePreflightSummary,
    canonical_json_bytes,
    canonical_sha256,
    cutover_preflight_report_sha256,
)
from productflow_backend.application.legacy_retirement.freeze import (
    LEGACY_V1_WRITE_FREEZE_SETTING_KEY,
    legacy_v1_write_freeze_state_from_value,
)
from productflow_backend.application.legacy_retirement.profiles import (
    CANVAS_RUN_ACTIVE_STATUSES,
    CANVAS_RUN_KNOWN_STATUSES,
    WORKFLOW_NODE_ACTIVE_STATUSES,
    WORKFLOW_NODE_KNOWN_STATUSES,
    WORKFLOW_RUN_ACTIVE_STATUSES,
    WORKFLOW_RUN_KNOWN_STATUSES,
)
from productflow_backend.application.legacy_retirement.row_utils import as_bool, optional_id, required_id
from productflow_backend.application.legacy_retirement.source import open_legacy_read_only_connection
from productflow_backend.config import Settings, get_settings
from productflow_backend.infrastructure.provider_config import (
    AGENT_PURPOSE,
    IMAGE_PURPOSE,
    PROMPT_PURPOSE,
    capability_for_provider_kind,
    normalize_provider_binding_model_settings,
    normalize_provider_binding_runtime_config,
    provider_kinds_for_purpose,
    validate_provider_profile_contract,
)

TEXT_PURPOSE = "text"
_LEGACY_TEXT_PROVIDER_KINDS = {"mock", "openai"}
_COMPATIBILITY_PURPOSES = (TEXT_PURPOSE, PROMPT_PURPOSE, AGENT_PURPOSE, IMAGE_PURPOSE)
_V2_ONLINE_PURPOSES = frozenset({PROMPT_PURPOSE, AGENT_PURPOSE, IMAGE_PURPOSE})
_MODEL_KEYS = frozenset({"model", "brief_model", "copy_model"})
_SYSTEM_PROMPT_FIELDS = (
    ("prompt_brief_system", "PROMPT_BRIEF_SYSTEM"),
    ("prompt_copy_system", "PROMPT_COPY_SYSTEM"),
)
_PROVIDER_ENVIRONMENT_KEYS = (
    ("TEXT_PROVIDER_KIND", False, "text_provider_kind"),
    ("TEXT_API_KEY", True, "text_api_key"),
    ("TEXT_BASE_URL", False, "text_base_url"),
    ("TEXT_BRIEF_MODEL", False, "text_brief_model"),
    ("TEXT_COPY_MODEL", False, "text_copy_model"),
    ("PROMPT_PROVIDER_KIND", False, None),
    ("PROMPT_API_KEY", True, None),
    ("PROMPT_BASE_URL", False, None),
    ("PROMPT_MODEL", False, None),
    ("IMAGE_PROVIDER_KIND", False, "image_provider_kind"),
    ("IMAGE_API_KEY", True, "image_api_key"),
    ("IMAGE_BASE_URL", False, "image_base_url"),
    ("IMAGE_GENERATE_MODEL", False, "image_generate_model"),
)

_PROVIDER_ISSUE_DETAILS = {
    "provider_binding_duplicate": "同一 provider purpose 存在多条绑定。",
    "provider_binding_kind_invalid": "provider binding 的接口类型不属于该 purpose。",
    "provider_binding_missing": "兼容切换要求的 provider binding 缺失。",
    "provider_binding_mock": "v2 在线 purpose 仍使用 mock，不能作为生产切换配置。",
    "provider_binding_purpose_unknown": "存在预检器不认识的 provider purpose。",
    "provider_binding_runtime_invalid": "provider binding 的模型或运行配置无效。",
    "provider_profile_api_key_missing": "真实 provider binding 对应的供应商档案没有 API key。",
    "provider_profile_contract_invalid": "供应商档案类型、能力或连接合同无效。",
    "provider_profile_missing": "真实 provider binding 没有可用的供应商档案。",
    "provider_profile_unavailable": "真实 provider binding 指向已停用或归档的供应商档案。",
}


def audit_legacy_cutover_preflight(
    engine: Engine,
    *,
    storage_root: Path,
    environment: Mapping[str, str] | None = None,
    base_settings: Settings | None = None,
    generated_at: datetime | None = None,
) -> LegacyCutoverPreflightReport:
    timestamp = generated_at or datetime.now(UTC)
    effective_environment = dict(os.environ if environment is None else environment)
    effective_settings = base_settings or get_settings()
    with open_legacy_read_only_connection(engine) as connection:
        source_report = audit_legacy_retirement_connection(
            connection,
            storage_root=storage_root,
            generated_at=timestamp,
        )
        return _build_cutover_preflight(
            connection,
            source_report=source_report,
            environment=effective_environment,
            base_settings=effective_settings,
            generated_at=timestamp,
        )


def _build_cutover_preflight(
    connection: Connection,
    *,
    source_report: LegacyRetirementAuditReport,
    environment: Mapping[str, str],
    base_settings: Settings,
    generated_at: datetime,
) -> LegacyCutoverPreflightReport:
    inspector = sa.inspect(connection)
    table_names = frozenset(inspector.get_table_names())
    app_settings = {
        str(row["key"]): row
        for row in _load_rows(connection, "app_settings", table_names)
    }
    freeze_row = app_settings.get(LEGACY_V1_WRITE_FREEZE_SETTING_KEY)
    freeze_state = legacy_v1_write_freeze_state_from_value(
        str(freeze_row["value"]) if freeze_row is not None else None,
        updated_at=freeze_row.get("updated_at") if freeze_row is not None else None,
    )
    freeze = LegacyV1FreezePreflightSummary(
        configured=freeze_state.configured,
        frozen=freeze_state.frozen,
        valid=freeze_state.valid,
        updated_at=freeze_state.updated_at,
    )
    execution = _execution_summary(source_report)
    provider_configuration, provider_issues = _provider_configuration_summary(
        connection,
        table_names=table_names,
        app_settings=app_settings,
        environment=environment,
        base_settings=base_settings,
    )

    issues = list(source_report.issues)
    if not freeze.valid:
        issues.append(
            AuditIssue(
                code="legacy_v1_freeze_invalid",
                severity="blocking",
                count=1,
                detail="旧工作流冻结状态值无效，系统虽会拒绝写入，但不能据此批准切换。",
            )
        )
    elif not freeze.frozen:
        issues.append(
            AuditIssue(
                code="legacy_v1_not_frozen",
                severity="blocking",
                count=1,
                detail="旧工作流写入尚未冻结，不能执行 final backfill 或退出在线代码。",
            )
        )
    issues.extend(provider_issues)
    issues = sorted(issues, key=lambda item: (item.severity != "blocking", item.code))
    provisional = LegacyCutoverPreflightReport(
        generated_at=generated_at,
        source_profile=source_report.source.schema_profile,
        source_report_sha256=source_report.report_sha256,
        freeze=freeze,
        execution=execution,
        provider_configuration=provider_configuration,
        issues=issues,
        ready_for_cutover=not any(issue.severity == "blocking" for issue in issues),
        report_sha256="0" * 64,
    )
    return provisional.model_copy(update={"report_sha256": cutover_preflight_report_sha256(provisional)})


def _execution_summary(source_report: LegacyRetirementAuditReport) -> LegacyV1ExecutionPreflightSummary:
    workflow_run_status_counts: Counter[str] = Counter()
    node_run_status_counts: Counter[str] = Counter()
    legacy_items = [item for item in source_report.workflows.items if item.archive_candidate]
    for item in legacy_items:
        workflow_run_status_counts.update(item.run_status_counts)
        node_run_status_counts.update(item.node_run_status_counts)
    canvas_status_counts = source_report.canvas_agent.run_status_counts
    blocking_workflow_ids = sorted(
        item.workflow_id
        for item in legacy_items
        if any(
            status in WORKFLOW_RUN_ACTIVE_STATUSES or status not in WORKFLOW_RUN_KNOWN_STATUSES
            for status in item.run_status_counts
        )
        or any(
            status in WORKFLOW_NODE_ACTIVE_STATUSES or status not in WORKFLOW_NODE_KNOWN_STATUSES
            for status in item.node_run_status_counts
        )
    )
    blocking_canvas_agent_thread_ids = sorted(
        item.thread_id
        for item in source_report.canvas_agent.items
        if any(
            status in CANVAS_RUN_ACTIVE_STATUSES or status not in CANVAS_RUN_KNOWN_STATUSES
            for status in item.run_status_counts
        )
    )
    return LegacyV1ExecutionPreflightSummary(
        workflow_count=len(legacy_items),
        workflow_run_status_counts=dict(sorted(workflow_run_status_counts.items())),
        node_run_status_counts=dict(sorted(node_run_status_counts.items())),
        blocking_workflow_ids=blocking_workflow_ids,
        blocking_canvas_agent_thread_ids=blocking_canvas_agent_thread_ids,
        active_workflow_run_count=sum(
            workflow_run_status_counts.get(status, 0) for status in WORKFLOW_RUN_ACTIVE_STATUSES
        ),
        active_node_run_count=sum(
            node_run_status_counts.get(status, 0) for status in WORKFLOW_NODE_ACTIVE_STATUSES
        ),
        unknown_workflow_run_count=sum(
            count for status, count in workflow_run_status_counts.items() if status not in WORKFLOW_RUN_KNOWN_STATUSES
        ),
        unknown_node_run_count=sum(
            count for status, count in node_run_status_counts.items() if status not in WORKFLOW_NODE_KNOWN_STATUSES
        ),
        active_canvas_agent_run_count=sum(
            canvas_status_counts.get(status, 0) for status in CANVAS_RUN_ACTIVE_STATUSES
        ),
        unknown_canvas_agent_run_count=sum(
            count for status, count in canvas_status_counts.items() if status not in CANVAS_RUN_KNOWN_STATUSES
        ),
    )


def _provider_configuration_summary(
    connection: Connection,
    *,
    table_names: frozenset[str],
    app_settings: Mapping[str, Mapping[str, Any]],
    environment: Mapping[str, str],
    base_settings: Settings,
) -> tuple[LegacyProviderConfigurationPreflightSummary, list[AuditIssue]]:
    profile_rows = _load_rows(connection, "provider_profiles", table_names)
    binding_rows = _load_rows(connection, "provider_bindings", table_names)
    profiles = [_profile_summary(row) for row in profile_rows]
    profiles.sort(key=lambda item: item.profile_id)
    profile_rows_by_id = {required_id(row, "id"): row for row in profile_rows}

    binding_results: list[ProviderBindingPreflightSummary] = []
    binding_rows_by_purpose: dict[str, list[Mapping[str, Any]]] = {}
    for row in binding_rows:
        binding_rows_by_purpose.setdefault(str(row.get("purpose") or ""), []).append(row)

    issue_counts: Counter[str] = Counter()
    for _purpose, rows in sorted(binding_rows_by_purpose.items()):
        if len(rows) > 1:
            issue_counts["provider_binding_duplicate"] += len(rows)
        for row in rows:
            summary = _binding_summary(row, profile_rows_by_id=profile_rows_by_id)
            binding_results.append(summary)
            issue_counts.update(summary.issue_codes)
    for purpose in _COMPATIBILITY_PURPOSES:
        if purpose not in binding_rows_by_purpose:
            issue_counts["provider_binding_missing"] += 1
    missing_purposes = sorted(set(_COMPATIBILITY_PURPOSES) - binding_rows_by_purpose.keys())
    binding_results.sort(key=lambda item: (item.purpose, item.provider_kind, item.provider_profile_id or ""))

    issues = [
        AuditIssue(
            code=code,
            severity="blocking",
            count=count,
            detail=_PROVIDER_ISSUE_DETAILS[code],
        )
        for code, count in sorted(issue_counts.items())
    ]
    explicit_prompt = next((item for item in binding_results if item.purpose == PROMPT_PURPOSE), None)
    transitional_text = next((item for item in binding_results if item.purpose == TEXT_PURPOSE), None)
    if explicit_prompt is not None and transitional_text is not None:
        text_copy_model = transitional_text.models.get("copy_model")
        if (
            explicit_prompt.provider_kind != transitional_text.provider_kind
            or explicit_prompt.provider_profile_id != transitional_text.provider_profile_id
            or explicit_prompt.models.get("model") != text_copy_model
        ):
            issues.append(
                AuditIssue(
                    code="prompt_text_binding_differs",
                    severity="warning",
                    count=1,
                    detail="prompt 与兼容期 text 配置不同；视为显式配置并保留，切换前需人工确认。",
                )
            )

    return (
        LegacyProviderConfigurationPreflightSummary(
            required_purposes=sorted(_COMPATIBILITY_PURPOSES),
            missing_purposes=missing_purposes,
            profiles=profiles,
            bindings=binding_results,
            system_prompt_fingerprints=_system_prompt_fingerprints(
                app_settings=app_settings,
                environment=environment,
                base_settings=base_settings,
            ),
            environment_fingerprints=_environment_fingerprints(
                environment,
                base_settings=base_settings,
            ),
        ),
        issues,
    )


def _profile_summary(row: Mapping[str, Any]) -> ProviderProfilePreflightSummary:
    base_url = str(row.get("base_url") or "").strip()
    api_key = str(row.get("api_key") or "").strip()
    capabilities = sorted(_json_list(row.get("capabilities_json")))
    default_models = _json_mapping(row.get("default_models_json"))
    config = _json_mapping(row.get("config_json"))
    return ProviderProfilePreflightSummary(
        profile_id=required_id(row, "id"),
        name=str(row.get("name") or ""),
        provider_type=str(row.get("provider_type") or ""),
        enabled=as_bool(row.get("enabled")),
        archived=row.get("archived_at") is not None,
        capabilities=capabilities,
        default_model_keys=sorted(default_models),
        default_models_sha256=canonical_sha256(default_models),
        config_keys=sorted(config),
        config_sha256=canonical_sha256(config),
        base_url_configured=bool(base_url),
        base_url_sha256=_sha256_text(base_url) if base_url else None,
        api_key_configured=bool(api_key),
        api_key_sha256=_sha256_text(api_key) if api_key else None,
    )


def _binding_summary(
    row: Mapping[str, Any],
    *,
    profile_rows_by_id: Mapping[str, Mapping[str, Any]],
) -> ProviderBindingPreflightSummary:
    purpose = str(row.get("purpose") or "")
    provider_kind = str(row.get("provider_kind") or "")
    profile_id = optional_id(row, "provider_profile_id")
    model_settings = _json_mapping(row.get("model_settings_json"))
    config = _json_mapping(row.get("config_json"))
    models = {
        key: value.strip()
        for key, raw_value in sorted(model_settings.items())
        if key in _MODEL_KEYS and (value := str(raw_value or "").strip())
    }
    issue_codes: set[str] = set()
    kind_valid = False
    if purpose not in _COMPATIBILITY_PURPOSES:
        issue_codes.add("provider_binding_purpose_unknown")
    else:
        kind_valid = (
            provider_kind in _LEGACY_TEXT_PROVIDER_KINDS
            if purpose == TEXT_PURPOSE
            else provider_kind in provider_kinds_for_purpose(purpose)
        )
        if not kind_valid:
            issue_codes.add("provider_binding_kind_invalid")
        elif purpose == TEXT_PURPOSE:
            if not any(models.get(key) for key in ("model", "brief_model", "copy_model")):
                issue_codes.add("provider_binding_runtime_invalid")
        else:
            try:
                normalize_provider_binding_model_settings(purpose=purpose, model_settings=model_settings)
                normalize_provider_binding_runtime_config(
                    purpose=purpose,
                    provider_kind=provider_kind,
                    model_settings=model_settings,
                    config=config,
                )
            except (TypeError, ValueError):
                issue_codes.add("provider_binding_runtime_invalid")

    if provider_kind == "mock":
        if purpose in _V2_ONLINE_PURPOSES:
            issue_codes.add("provider_binding_mock")
        if profile_id is not None:
            issue_codes.add("provider_profile_contract_invalid")
    elif purpose in _COMPATIBILITY_PURPOSES and kind_valid:
        profile = profile_rows_by_id.get(profile_id or "")
        if profile is None:
            issue_codes.add("provider_profile_missing")
        else:
            if not as_bool(profile.get("enabled")) or profile.get("archived_at") is not None:
                issue_codes.add("provider_profile_unavailable")
            if not str(profile.get("api_key") or "").strip():
                issue_codes.add("provider_profile_api_key_missing")
            try:
                capabilities = _json_list(profile.get("capabilities_json"))
                validate_provider_profile_contract(
                    provider_type=str(profile.get("provider_type") or ""),
                    capabilities=capabilities,
                    base_url=str(profile.get("base_url") or "").strip() or None,
                )
                required_capability = (
                    "text_responses"
                    if purpose == TEXT_PURPOSE
                    else capability_for_provider_kind(provider_kind)
                )
                if required_capability not in set(capabilities):
                    raise ValueError
            except (TypeError, ValueError):
                issue_codes.add("provider_profile_contract_invalid")
    ordered_issue_codes = sorted(issue_codes)
    return ProviderBindingPreflightSummary(
        purpose=purpose,
        provider_kind=provider_kind,
        provider_profile_id=profile_id,
        models=models,
        model_settings_sha256=canonical_sha256(model_settings),
        config_keys=sorted(config),
        config_sha256=canonical_sha256(config),
        valid=not ordered_issue_codes,
        issue_codes=ordered_issue_codes,
    )


def _system_prompt_fingerprints(
    *,
    app_settings: Mapping[str, Mapping[str, Any]],
    environment: Mapping[str, str],
    base_settings: Settings,
) -> list[ConfigurationValueFingerprint]:
    results: list[ConfigurationValueFingerprint] = []
    for field_name, environment_name in _SYSTEM_PROMPT_FIELDS:
        database_row = app_settings.get(field_name)
        if database_row is not None:
            value = str(database_row.get("value") or "")
            source = "database_override"
        elif environment_name in environment:
            value = environment[environment_name]
            source = "environment"
        elif not hasattr(base_settings, field_name):
            value = None
            source = "absent"
        else:
            value = str(getattr(base_settings, field_name))
            field_default = Settings.model_fields[field_name].default
            source = "default" if value == str(field_default) else "environment"
        results.append(
            ConfigurationValueFingerprint(
                name=field_name,
                source=source,
                configured=value is not None,
                sensitive=True,
                value_sha256=_sha256_text(value) if value is not None else None,
            )
        )
    return results


def _environment_fingerprints(
    environment: Mapping[str, str],
    *,
    base_settings: Settings,
) -> list[ConfigurationValueFingerprint]:
    results: list[ConfigurationValueFingerprint] = []
    for name, sensitive, settings_field in _PROVIDER_ENVIRONMENT_KEYS:
        if name in environment:
            configured = True
            source = "environment"
            value = environment[name]
        elif settings_field is None or not hasattr(base_settings, settings_field):
            configured = False
            source = "absent"
            value = None
        else:
            effective_value = getattr(base_settings, settings_field)
            default_value = Settings.model_fields[settings_field].default
            configured = effective_value != default_value
            source = "environment" if configured else ("default" if effective_value is not None else "absent")
            value = "" if effective_value is None else str(effective_value)
        results.append(
            ConfigurationValueFingerprint(
                name=name,
                source=source,
                configured=configured,
                sensitive=sensitive,
                value_sha256=_sha256_text(value) if value is not None else None,
            )
        )
    return results


def _load_rows(
    connection: Connection,
    table_name: str,
    table_names: frozenset[str],
) -> list[Mapping[str, Any]]:
    if table_name not in table_names:
        return []
    table = sa.Table(table_name, sa.MetaData(), autoload_with=connection)
    rows = list(connection.execute(sa.select(table)).mappings())
    return sorted(rows, key=canonical_json_bytes)


def _json_mapping(value: Any) -> dict[str, Any]:
    if isinstance(value, Mapping):
        return {str(key): item for key, item in value.items()}
    if isinstance(value, str):
        try:
            parsed = json.loads(value)
        except json.JSONDecodeError:
            return {}
        if isinstance(parsed, dict):
            return {str(key): item for key, item in parsed.items()}
    return {}


def _json_list(value: Any) -> list[str]:
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except json.JSONDecodeError:
            return []
    if not isinstance(value, list):
        return []
    return [str(item) for item in value]


def _sha256_text(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


__all__ = ["audit_legacy_cutover_preflight"]
