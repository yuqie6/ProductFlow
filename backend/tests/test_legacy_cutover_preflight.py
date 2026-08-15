from __future__ import annotations

import json
from datetime import UTC, datetime, timedelta

import sqlalchemy as sa
from helpers import _make_demo_image_bytes

from productflow_backend.application.legacy_retirement.freeze import set_legacy_v1_write_freeze_state
from productflow_backend.application.legacy_retirement.preflight import audit_legacy_cutover_preflight
from productflow_backend.application.product_workflow.mutations import get_or_create_product_workflow
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.commands import preflight_legacy_cutover as preflight_command
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    AppSetting,
    ProductWorkflow,
    ProviderBinding,
    ProviderProfile,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)


def _install_current_revision(db_session) -> None:
    db_session.execute(sa.text("CREATE TABLE alembic_version (version_num VARCHAR(32) PRIMARY KEY)"))
    db_session.execute(sa.text("INSERT INTO alembic_version (version_num) VALUES ('20260815_0041')"))
    db_session.commit()


def _seed_real_provider_bindings(db_session) -> ProviderProfile:
    profile = ProviderProfile(
        name="生产网关",
        provider_type="openai_compatible",
        base_url="https://private-gateway.example/v1",
        api_key="provider-secret-value",
        capabilities_json=["text_responses", "image_responses"],
        default_models_json={},
        config_json={},
        enabled=True,
    )
    db_session.add(profile)
    db_session.flush()
    db_session.add_all(
        [
            ProviderBinding(
                purpose="text",
                provider_kind="openai",
                provider_profile_id=profile.id,
                model_settings_json={"brief_model": "gpt-5.5", "copy_model": "gpt-5.5"},
                config_json={},
            ),
            ProviderBinding(
                purpose="prompt",
                provider_kind="openai",
                provider_profile_id=profile.id,
                model_settings_json={"model": "gpt-5.5"},
                config_json={},
            ),
            ProviderBinding(
                purpose="agent",
                provider_kind="openai",
                provider_profile_id=profile.id,
                model_settings_json={"model": "gpt-5.5"},
                config_json={},
            ),
            ProviderBinding(
                purpose="image",
                provider_kind="openai_responses",
                provider_profile_id=profile.id,
                model_settings_json={"model": "gpt-image-1"},
                config_json={"responses_background_enabled": False},
            ),
        ]
    )
    db_session.add(
        AppSetting(
            key="prompt_brief_system",
            value="private legacy system prompt body",
        )
    )
    db_session.commit()
    return profile


def _seed_v1_and_active_v2_workflows(db_session) -> tuple[ProductWorkflow, ProductWorkflow]:
    legacy_product = create_canonical_product(
        db_session,
        name="旧工作流商品",
        category="工具",
        price="199",
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "legacy.png", "image/png")],
    )
    legacy_workflow = get_or_create_product_workflow(db_session, legacy_product.id)

    v2_product = create_canonical_product(
        db_session,
        name="v2 工作流商品",
        category="工具",
        price="299",
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "v2.png", "image/png")],
    )
    v2_workflow = ProductWorkflow(
        product_id=v2_product.id,
        title="v2 工作流",
        active=True,
        schema_version=2,
        revision=1,
    )
    db_session.add(v2_workflow)
    db_session.flush()
    v2_node = WorkflowNode(
        workflow_id=v2_workflow.id,
        schema_version=2,
        node_key="prompt-1",
        node_type=WorkflowNodeType.PROMPT_GENERATION,
        title="提示词",
        position_x=0,
        position_y=0,
        config_json={},
        status=WorkflowNodeStatus.QUEUED,
    )
    db_session.add(v2_node)
    db_session.flush()
    v2_run = WorkflowRun(workflow_id=v2_workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(v2_run)
    db_session.flush()
    db_session.add(
        WorkflowNodeRun(
            workflow_run_id=v2_run.id,
            node_id=v2_node.id,
            status=WorkflowNodeStatus.QUEUED,
        )
    )
    db_session.commit()
    return legacy_workflow, v2_workflow


def test_cutover_preflight_is_stable_secret_free_and_ignores_active_v2_runs(
    configured_env,
    db_session,
) -> None:
    _install_current_revision(db_session)
    _seed_real_provider_bindings(db_session)
    legacy_workflow, _v2_workflow = _seed_v1_and_active_v2_workflows(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    timestamp = datetime(2026, 8, 15, 22, 0, tzinfo=UTC)
    environment = {
        "TEXT_PROVIDER_KIND": "openai",
        "TEXT_API_KEY": "legacy-environment-secret",
        "TEXT_BASE_URL": "https://legacy-private.example/v1",
        "TEXT_BRIEF_MODEL": "gpt-5.5",
        "TEXT_COPY_MODEL": "gpt-5.5",
    }

    first = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment=environment,
        base_settings=get_settings(),
        generated_at=timestamp,
    )
    second = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment=environment,
        base_settings=get_settings(),
        generated_at=timestamp + timedelta(minutes=5),
    )

    assert first.report_sha256 == second.report_sha256
    assert first.ready_for_cutover is True
    assert first.execution.workflow_count == 1
    assert first.execution.active_workflow_run_count == 0
    assert first.execution.active_node_run_count == 0
    assert first.execution.blocking_workflow_ids == []
    assert first.execution.blocking_canvas_agent_thread_ids == []
    assert legacy_workflow.schema_version == 1
    assert not any(issue.code == "active_workflow_execution" for issue in first.issues)
    assert {binding.purpose for binding in first.provider_configuration.bindings} == {
        "text",
        "prompt",
        "agent",
        "image",
    }
    assert all(binding.valid for binding in first.provider_configuration.bindings)
    assert all(len(binding.model_settings_sha256) == 64 for binding in first.provider_configuration.bindings)
    assert all(len(binding.config_sha256) == 64 for binding in first.provider_configuration.bindings)
    assert first.provider_configuration.required_purposes == ["agent", "image", "prompt", "text"]
    assert first.provider_configuration.missing_purposes == []
    api_key_fingerprints = [
        item for item in first.provider_configuration.environment_fingerprints if item.name.endswith("API_KEY")
    ]
    assert all(item.sensitive for item in api_key_fingerprints)
    assert next(item for item in api_key_fingerprints if item.name == "TEXT_API_KEY").value_sha256 is not None
    assert first.provider_configuration.profiles[0].api_key_sha256 is not None
    assert len(first.provider_configuration.profiles[0].default_models_sha256) == 64
    assert len(first.provider_configuration.profiles[0].config_sha256) == 64
    prompt_fingerprints = {
        item.name: item for item in first.provider_configuration.system_prompt_fingerprints
    }
    assert prompt_fingerprints["prompt_brief_system"].source == "database_override"
    assert prompt_fingerprints["prompt_copy_system"].source == "default"
    assert all(item.sensitive for item in prompt_fingerprints.values())

    rendered = json.dumps(first.model_dump(mode="json"), ensure_ascii=False, sort_keys=True)
    assert "provider-secret-value" not in rendered
    assert "legacy-environment-secret" not in rendered
    assert "private-gateway.example" not in rendered
    assert "legacy-private.example" not in rendered
    assert "private legacy system prompt body" not in rendered


def test_cutover_preflight_labels_non_default_base_prompt_as_environment_source(
    configured_env,
    db_session,
) -> None:
    _install_current_revision(db_session)
    _seed_real_provider_bindings(db_session)
    _seed_v1_and_active_v2_workflows(db_session)
    db_session.execute(sa.delete(AppSetting).where(AppSetting.key == "prompt_brief_system"))
    db_session.commit()
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    settings = get_settings().model_copy(
        update={
            "prompt_copy_system": "prompt loaded through settings source",
            "text_api_key": "settings-file-secret",
            "text_base_url": "https://settings-private.example/v1",
        }
    )

    report = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment={},
        base_settings=settings,
    )

    prompt_fingerprints = {
        item.name: item for item in report.provider_configuration.system_prompt_fingerprints
    }
    assert prompt_fingerprints["prompt_brief_system"].source == "default"
    assert prompt_fingerprints["prompt_copy_system"].source == "environment"
    environment_fingerprints = {
        item.name: item for item in report.provider_configuration.environment_fingerprints
    }
    assert environment_fingerprints["TEXT_API_KEY"].source == "environment"
    assert environment_fingerprints["TEXT_API_KEY"].configured is True
    assert environment_fingerprints["TEXT_API_KEY"].sensitive is True
    assert environment_fingerprints["TEXT_BASE_URL"].source == "environment"
    assert environment_fingerprints["TEXT_PROVIDER_KIND"].source == "default"
    assert environment_fingerprints["PROMPT_API_KEY"].source == "absent"
    rendered = json.dumps(report.model_dump(mode="json"), ensure_ascii=False, sort_keys=True)
    assert "prompt loaded through settings source" not in rendered
    assert "settings-file-secret" not in rendered
    assert "settings-private.example" not in rendered


def test_cutover_preflight_blocks_unfrozen_active_unknown_v1_and_invalid_provider(
    configured_env,
    db_session,
) -> None:
    _install_current_revision(db_session)
    _seed_real_provider_bindings(db_session)
    legacy_workflow, _v2_workflow = _seed_v1_and_active_v2_workflows(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=False)
    active_run = WorkflowRun(workflow_id=legacy_workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(active_run)
    unknown_run = WorkflowRun(workflow_id=legacy_workflow.id, status=WorkflowRunStatus.SUCCEEDED)
    db_session.add(unknown_run)
    prompt_binding = db_session.scalar(sa.select(ProviderBinding).where(ProviderBinding.purpose == "prompt"))
    assert prompt_binding is not None
    db_session.delete(prompt_binding)
    db_session.commit()
    db_session.execute(
        sa.text("UPDATE workflow_runs SET status = 'provider_new_state' WHERE id = :run_id"),
        {"run_id": unknown_run.id},
    )
    db_session.commit()

    report = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment={},
        base_settings=get_settings(),
    )

    assert report.ready_for_cutover is False
    assert report.execution.active_workflow_run_count == 1
    assert report.execution.unknown_workflow_run_count == 1
    assert report.execution.blocking_workflow_ids == [legacy_workflow.id]
    assert report.provider_configuration.missing_purposes == ["prompt"]
    issue_codes = {issue.code for issue in report.issues}
    assert {
        "active_workflow_execution",
        "unknown_workflow_execution_status",
        "legacy_v1_not_frozen",
        "provider_binding_missing",
    } <= issue_codes


def test_cutover_preflight_preserves_explicit_prompt_difference_and_blocks_unavailable_providers(
    configured_env,
    db_session,
) -> None:
    _install_current_revision(db_session)
    profile = _seed_real_provider_bindings(db_session)
    _seed_v1_and_active_v2_workflows(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    profile.enabled = False
    profile.api_key = None
    prompt_binding = db_session.scalar(sa.select(ProviderBinding).where(ProviderBinding.purpose == "prompt"))
    image_binding = db_session.scalar(sa.select(ProviderBinding).where(ProviderBinding.purpose == "image"))
    assert prompt_binding is not None
    assert image_binding is not None
    prompt_binding.model_settings_json = {"model": "explicit-prompt-model"}
    image_binding.provider_kind = "mock"
    image_binding.provider_profile_id = None
    image_binding.config_json = {}
    db_session.commit()

    report = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment={},
        base_settings=get_settings(),
    )

    issues = {issue.code: issue for issue in report.issues}
    assert report.ready_for_cutover is False
    assert report.provider_configuration.missing_purposes == []
    assert issues["prompt_text_binding_differs"].severity == "warning"
    assert issues["provider_profile_unavailable"].severity == "blocking"
    assert issues["provider_profile_api_key_missing"].severity == "blocking"
    assert issues["provider_binding_mock"].severity == "blocking"
    prompt_summary = next(
        item for item in report.provider_configuration.bindings if item.purpose == "prompt"
    )
    image_summary = next(
        item for item in report.provider_configuration.bindings if item.purpose == "image"
    )
    assert prompt_summary.models == {"model": "explicit-prompt-model"}
    assert "provider_profile_unavailable" in prompt_summary.issue_codes
    assert "provider_profile_api_key_missing" in prompt_summary.issue_codes
    assert image_summary.issue_codes == ["provider_binding_mock"]


def test_cutover_preflight_hash_changes_when_non_secret_provider_config_drifts(
    configured_env,
    db_session,
) -> None:
    _install_current_revision(db_session)
    _seed_real_provider_bindings(db_session)
    _seed_v1_and_active_v2_workflows(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    timestamp = datetime(2026, 8, 15, 22, 30, tzinfo=UTC)
    before = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment={},
        base_settings=get_settings(),
        generated_at=timestamp,
    )
    agent_binding = db_session.scalar(sa.select(ProviderBinding).where(ProviderBinding.purpose == "agent"))
    assert agent_binding is not None
    previous_config_sha256 = next(
        item.config_sha256 for item in before.provider_configuration.bindings if item.purpose == "agent"
    )
    agent_binding.config_json = {"reasoning_effort": "high"}
    db_session.commit()

    after = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment={},
        base_settings=get_settings(),
        generated_at=timestamp,
    )

    current_config_sha256 = next(
        item.config_sha256 for item in after.provider_configuration.bindings if item.purpose == "agent"
    )
    assert current_config_sha256 != previous_config_sha256
    assert after.report_sha256 != before.report_sha256
    assert "high" not in json.dumps(after.model_dump(mode="json"), ensure_ascii=False, sort_keys=True)


def test_cutover_preflight_command_writes_the_same_secret_free_report(
    configured_env,
    db_session,
    monkeypatch,
    tmp_path,
    capsys,
) -> None:
    _install_current_revision(db_session)
    _seed_real_provider_bindings(db_session)
    _seed_v1_and_active_v2_workflows(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    output = tmp_path / "cutover-preflight.json"
    monkeypatch.setattr(preflight_command, "get_engine", db_session.get_bind)

    assert preflight_command.main(["--output", str(output), "--compact"]) == 0
    stdout_payload = json.loads(capsys.readouterr().out)
    file_payload = json.loads(output.read_text(encoding="utf-8"))
    assert stdout_payload == file_payload
    assert stdout_payload["ready_for_cutover"] is True
    rendered = output.read_text(encoding="utf-8")
    assert "provider-secret-value" not in rendered
    assert "private legacy system prompt body" not in rendered
