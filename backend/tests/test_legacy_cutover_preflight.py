from __future__ import annotations

import json
from datetime import UTC, datetime

import sqlalchemy as sa

from productflow_backend.application.legacy_retirement.freeze import set_legacy_v1_write_freeze_state
from productflow_backend.application.legacy_retirement.preflight import audit_legacy_cutover_preflight
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.models import ProviderBinding, ProviderProfile


def _seed_provider_bindings(db_session) -> None:
    profile = ProviderProfile(
        name="迁移预检网关",
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
    db_session.commit()


def test_preflight_audits_transitional_text_without_restoring_current_runtime(
    configured_env,
    db_session,
) -> None:
    _seed_provider_bindings(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    environment = {
        "TEXT_PROVIDER_KIND": "openai",
        "TEXT_API_KEY": "legacy-environment-secret",
        "TEXT_BASE_URL": "https://legacy-private.example/v1",
        "TEXT_BRIEF_MODEL": "gpt-5.5",
        "TEXT_COPY_MODEL": "gpt-5.5",
    }
    timestamp = datetime(2026, 8, 15, 22, 0, tzinfo=UTC)

    report = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment=environment,
        base_settings=get_settings(),
        generated_at=timestamp,
    )

    assert report.freeze.frozen is True
    assert report.freeze.valid is True
    assert {binding.purpose for binding in report.provider_configuration.bindings} == {
        "text",
        "prompt",
        "agent",
        "image",
    }
    assert all(binding.valid for binding in report.provider_configuration.bindings)
    assert report.provider_configuration.missing_purposes == []
    assert all(
        item.value_sha256 is not None
        for item in report.provider_configuration.environment_fingerprints
        if item.name == "TEXT_API_KEY"
    )
    rendered = json.dumps(report.model_dump(mode="json"), ensure_ascii=False, sort_keys=True)
    assert "provider-secret-value" not in rendered
    assert "legacy-environment-secret" not in rendered
    assert "private-gateway.example" not in rendered
    assert "legacy-private.example" not in rendered


def test_preflight_marks_missing_current_binding_as_blocking(configured_env, db_session) -> None:
    _seed_provider_bindings(db_session)
    set_legacy_v1_write_freeze_state(db_session, frozen=True)
    db_session.execute(sa.delete(ProviderBinding).where(ProviderBinding.purpose == "prompt"))
    db_session.commit()

    report = audit_legacy_cutover_preflight(
        db_session.get_bind(),
        storage_root=configured_env,
        environment={},
        base_settings=get_settings(),
    )

    assert report.ready_for_cutover is False
    assert report.provider_configuration.missing_purposes == ["prompt"]
    assert any(issue.code == "provider_binding_missing" for issue in report.issues)
