from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.payloads import (
    ExportSnapshotPayload,
    GeneratedSummaryPayload,
    LaunchQualityScorePayload,
    SelectedAnglePayload,
    VariantContentPayload,
)
from productflow_backend.application.launch_kit.playbooks import get_active_category_playbook
from productflow_backend.application.launch_kit.query import get_launch_kit
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.domain.enums import JobStatus
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.launch_kits import (
    LaunchKitExportStatus,
    LaunchKitExportType,
    LaunchKitFailureCategory,
    LaunchKitPlatform,
    LaunchKitProgressStage,
    LaunchKitStatus,
    LaunchKitVariantKind,
)
from productflow_backend.infrastructure.db.models import (
    LaunchKit,
    LaunchKitExport,
    LaunchKitGenerationTask,
    LaunchKitVariant,
    LaunchQualityScore,
)
from productflow_backend.infrastructure.queue import enqueue_launch_kit_generation_task

ACTIVE_TASK_STATUSES = (JobStatus.QUEUED, JobStatus.RUNNING)
MAX_LAUNCH_KIT_REFERENCE_CHARS = 12_000
PROMPT_INJECTION_PATTERNS = (
    "ignore previous",
    "ignore all previous",
    "disregard previous",
    "system prompt",
    "developer message",
    "reveal your prompt",
    "jailbreak",
    "bypass",
    "act as",
    "do not follow",
    "forget the above",
)
PLATFORM_LABELS = {
    LaunchKitPlatform.SHOPEE.value: "Shopee",
    LaunchKitPlatform.TIKTOK_SHOP.value: "TikTok Shop",
    LaunchKitPlatform.BOTH.value: "Shopee + TikTok Shop",
}


def _now() -> datetime:
    return datetime.now(UTC)


def _has_active_generation_task(session: Session, launch_kit_id: str) -> bool:
    return session.scalar(
        select(LaunchKitGenerationTask.id)
        .where(LaunchKitGenerationTask.launch_kit_id == launch_kit_id)
        .where(LaunchKitGenerationTask.status.in_(ACTIVE_TASK_STATUSES))
        .limit(1)
    ) is not None


def create_launch_kit_generation_task(session: Session, *, launch_kit_id: str) -> LaunchKitGenerationTask:
    launch_kit = get_launch_kit(session, launch_kit_id)
    _validate_launch_kit_input_budget(launch_kit)
    if _has_active_generation_task(session, launch_kit.id):
        raise BusinessValidationError("LaunchKit generation is already queued or running")

    task = LaunchKitGenerationTask(
        launch_kit_id=launch_kit.id,
        status=JobStatus.QUEUED,
        progress_stage=LaunchKitProgressStage.EXTRACTING_FACTS,
        attempt_count=0,
        is_retryable=True,
        is_cancelable=True,
        provider_metadata_json={"schema_version": 1, "generator": "deterministic_v1"},
    )
    launch_kit.status = LaunchKitStatus.GENERATING
    launch_kit.updated_at = _now()
    session.add(task)
    session.commit()
    session.expire_all()
    return session.get(LaunchKitGenerationTask, task.id) or task


def mark_launch_kit_generation_task_enqueue_failed(session: Session, *, task_id: str, reason: str) -> None:
    task = session.get(LaunchKitGenerationTask, task_id)
    if task is None:
        return
    _mark_task_failed(session, task, category=LaunchKitFailureCategory.QUEUE_UNAVAILABLE, detail=reason, retryable=True)


def submit_launch_kit_generation_task(
    session: Session,
    *,
    launch_kit_id: str,
    enqueue: Callable[[str], None] | None = None,
) -> LaunchKit:
    task = create_launch_kit_generation_task(session, launch_kit_id=launch_kit_id)
    enqueue_or_mark_failed(
        task.id,
        enqueue=enqueue or enqueue_launch_kit_generation_task,
        mark_failed=lambda task_id, reason: mark_launch_kit_generation_task_enqueue_failed(
            session,
            task_id=task_id,
            reason=reason,
        ),
    )
    session.expire_all()
    return get_launch_kit(session, launch_kit_id)


def execute_launch_kit_generation_task(task_id: str, *, session_factory: Callable[[], Session]) -> None:
    """Run the v1 deterministic LaunchKit generator.

    This intentionally avoids provider calls. It creates an end-to-end usable manual-export kit from the product root,
    source references, and category playbook. Later AI generation can replace the content builder behind the same
    durable task contract.
    """

    session = session_factory()
    try:
        task = session.get(LaunchKitGenerationTask, task_id)
        if task is None or task.status != JobStatus.QUEUED:
            return
        launch_kit = get_launch_kit(session, task.launch_kit_id)
        _mark_task_running(session, task, launch_kit)

        _set_progress(session, task, LaunchKitProgressStage.EXTRACTING_FACTS)
        summary = _build_generated_summary(launch_kit)

        _set_progress(session, task, LaunchKitProgressStage.APPLYING_PLAYBOOK)
        playbook = get_active_category_playbook(session, launch_kit.category_key).playbook_json

        _set_progress(session, task, LaunchKitProgressStage.GENERATING_ANGLES)
        selected_angle = _build_selected_angle(launch_kit, playbook)

        _set_progress(session, task, LaunchKitProgressStage.GENERATING_COPY)
        variants = _build_variants(launch_kit, playbook, selected_angle)

        _set_progress(session, task, LaunchKitProgressStage.PLANNING_IMAGES)
        image_plan = _build_image_plan(launch_kit, playbook)
        variants.append(image_plan)

        _set_progress(session, task, LaunchKitProgressStage.SCORING)
        quality_score = _build_quality_score(summary, playbook, variants)

        _set_progress(session, task, LaunchKitProgressStage.EXPORTING_OPTIONAL_SNAPSHOT)
        _persist_generated_outputs(
            session,
            launch_kit=launch_kit,
            summary=summary,
            selected_angle=selected_angle,
            variants=variants,
            quality_score=quality_score,
            playbook=playbook,
        )
        _mark_task_succeeded(session, task, launch_kit)
    except Exception as exc:
        session.rollback()
        task = session.get(LaunchKitGenerationTask, task_id)
        if task is not None:
            _mark_task_failed(
                session,
                task,
                category=LaunchKitFailureCategory.UNKNOWN,
                detail=str(exc) or "LaunchKit generation failed",
                retryable=True,
            )
        raise
    finally:
        session.close()


def _mark_task_running(session: Session, task: LaunchKitGenerationTask, launch_kit: LaunchKit) -> None:
    now = _now()
    task.status = JobStatus.RUNNING
    task.started_at = now
    task.progress_updated_at = now
    task.attempt_count += 1
    task.is_cancelable = False
    launch_kit.status = LaunchKitStatus.GENERATING
    launch_kit.updated_at = now
    session.commit()


def _set_progress(session: Session, task: LaunchKitGenerationTask, stage: LaunchKitProgressStage) -> None:
    task.progress_stage = stage
    task.progress_updated_at = _now()
    session.commit()


def _mark_task_succeeded(session: Session, task: LaunchKitGenerationTask, launch_kit: LaunchKit) -> None:
    now = _now()
    task.status = JobStatus.SUCCEEDED
    task.finished_at = now
    task.progress_updated_at = now
    task.is_retryable = False
    task.is_cancelable = False
    launch_kit.status = LaunchKitStatus.READY
    launch_kit.updated_at = now
    session.commit()


def _mark_task_failed(
    session: Session,
    task: LaunchKitGenerationTask,
    *,
    category: LaunchKitFailureCategory,
    detail: str,
    retryable: bool,
) -> None:
    now = _now()
    task.status = JobStatus.FAILED
    task.failure_category = category.value
    task.failure_detail = detail
    task.is_retryable = retryable
    task.is_cancelable = False
    task.finished_at = now
    task.progress_updated_at = now
    launch_kit = session.get(LaunchKit, task.launch_kit_id)
    if launch_kit is not None:
        launch_kit.status = LaunchKitStatus.FAILED
        launch_kit.updated_at = now
    session.commit()


def _source_text(launch_kit: LaunchKit) -> str:
    references = launch_kit.source_references_json or {}
    parts = [
        launch_kit.product.name,
        str(references.get("pasted_reference_text") or ""),
        str(references.get("notes") or ""),
        " ".join(str(url) for url in references.get("reference_urls", [])),
    ]
    return "\n".join(part for part in parts if part).strip()


def _validate_launch_kit_input_budget(launch_kit: LaunchKit) -> None:
    reference_chars = len(_source_text(launch_kit))
    if reference_chars > MAX_LAUNCH_KIT_REFERENCE_CHARS:
        raise BusinessValidationError(
            "LaunchKit reference input is too long; keep product notes under "
            f"{MAX_LAUNCH_KIT_REFERENCE_CHARS:,} characters before generating."
        )


def _detect_prompt_injection_warnings(text: str) -> list[str]:
    lowered = text.lower()
    matched = [pattern for pattern in PROMPT_INJECTION_PATTERNS if pattern in lowered]
    if not matched:
        return []
    return [
        "Potential prompt-injection language detected in references; generated output ignored instruction-like text."
    ]


def _extract_keywords(text: str) -> list[str]:
    separators = [",", ".", ";", "\n", "|", "/", "-", "•", "·"]
    normalized = text.lower()
    for separator in separators:
        normalized = normalized.replace(separator, " ")
    stop_words = {"the", "and", "with", "cho", "của", "và", "for", "shop", "sale", "need", "copy"}
    keywords: list[str] = []
    for word in normalized.split():
        cleaned = word.strip("()[]{}:!?'\"")
        if len(cleaned) < 3 or cleaned in stop_words or cleaned in keywords:
            continue
        keywords.append(cleaned)
        if len(keywords) >= 10:
            break
    return keywords


def _build_generated_summary(launch_kit: LaunchKit) -> GeneratedSummaryPayload:
    references = launch_kit.source_references_json or {}
    text = _source_text(launch_kit)
    keywords = _extract_keywords(text)
    guardrail_warnings = _detect_prompt_injection_warnings(text)
    missing_facts = []
    if not references.get("pasted_reference_text"):
        missing_facts.append("Add supplier specs or competitor notes before publishing.")
    if not references.get("reference_urls"):
        missing_facts.append("Add at least one marketplace/reference URL for sharper benchmarking.")
    if not any(token in text.lower() for token in ["size", "kích", "ml", "cm", "màu", "color", "material", "chất"]):
        missing_facts.append("Confirm size/material/color facts.")

    return GeneratedSummaryPayload(
        product_facts={
            "product_name": launch_kit.product.name,
            "category_key": launch_kit.category_key,
            "target_platforms": launch_kit.target_platforms_json,
            "reference_url_count": len(references.get("reference_urls", [])),
            "observed_keywords": keywords,
            "seller_notes_present": bool(references.get("notes")),
            "reference_character_count": len(text),
            "input_guardrails": {
                "max_reference_characters": MAX_LAUNCH_KIT_REFERENCE_CHARS,
                "prompt_injection_warning_count": len(guardrail_warnings),
            },
        },
        missing_facts=missing_facts,
        buyer_objections=[],
        risky_claims=guardrail_warnings,
    )


def _build_selected_angle(launch_kit: LaunchKit, playbook: dict[str, Any]) -> SelectedAnglePayload:
    first_objection = (playbook.get("buyer_objections") or ["trust before purchase"])[0]
    tone = ", ".join(playbook.get("content_tone") or ["clear", "practical"])
    return SelectedAnglePayload(
        key="proof_first_practical_value",
        label="Proof-first practical value",
        why_it_might_work=f"Lead with concrete proof that reduces buyer worry about {first_objection}.",
        buyer_emotion="Feels safe to compare, trust, and buy without chatting the seller first.",
        platform_fit="Searchable enough for Shopee, hook-led enough for TikTok Shop.",
        risk=f"Keep claims grounded; use a {tone} tone and avoid unsupported guarantees.",
    )


def _platforms_for_output(launch_kit: LaunchKit) -> list[str]:
    values = launch_kit.target_platforms_json or [LaunchKitPlatform.SHOPEE.value]
    if LaunchKitPlatform.BOTH.value in values:
        return [LaunchKitPlatform.SHOPEE.value, LaunchKitPlatform.TIKTOK_SHOP.value]
    return [value for value in values if value in {LaunchKitPlatform.SHOPEE.value, LaunchKitPlatform.TIKTOK_SHOP.value}]


def _title_for_platform(product_name: str, platform: str, keywords: list[str]) -> str:
    suffix = " ".join(keywords[:4])
    if platform == LaunchKitPlatform.TIKTOK_SHOP.value:
        return f"{product_name} - dễ dùng, rõ lợi ích"[:120]
    return f"{product_name} {suffix}".strip()[:120]


def _build_variants(
    launch_kit: LaunchKit,
    playbook: dict[str, Any],
    selected_angle: SelectedAnglePayload,
) -> list[VariantContentPayload]:
    text = _source_text(launch_kit)
    keywords = _extract_keywords(text)
    objections = playbook.get("buyer_objections") or []
    proof = playbook.get("required_visual_proof") or []
    variants: list[VariantContentPayload] = []
    for platform in _platforms_for_output(launch_kit):
        platform_note = (playbook.get("platform_notes") or {}).get(platform, "Keep claims concrete and easy to verify.")
        title = _title_for_platform(launch_kit.product.name, platform, keywords)
        bullets = [
            f"Giảm băn khoăn về {objection}." for objection in objections[:3]
        ] or ["Nêu rõ lợi ích chính và bằng chứng trong ảnh."]
        variants.append(
            VariantContentPayload(
                content={
                    "platform": platform,
                    "platform_label": PLATFORM_LABELS[platform],
                    "title": title,
                    "hook": f"Bạn cần {launch_kit.product.name} rõ thông tin, dễ quyết định?",
                    "description": "\n".join(
                        [
                            f"{launch_kit.product.name} được định vị theo góc: {selected_angle.label}.",
                            platform_note,
                            "Điểm cần nhấn mạnh:",
                            *[f"- {bullet}" for bullet in bullets],
                            "Bằng chứng ảnh cần có:",
                            *[f"- {item}" for item in proof[:4]],
                        ]
                    ),
                    "bullet_points": bullets,
                    "hashtags": (
                        ["#shopeevn", "#tiktokshop", "#hangtot"]
                        if platform == LaunchKitPlatform.TIKTOK_SHOP.value
                        else []
                    ),
                    "manual_export_note": (
                        "Copy thủ công vào Seller Center; kiểm tra lại giá, tồn kho và claim trước khi đăng."
                    ),
                },
                why_it_should_convert="It turns category objections into visible proof and listing copy.",
                buyer_objection_addressed=", ".join(objections[:3]),
                platform_fit=platform_note,
                risk="Do not publish unsupported performance, medical, or guaranteed-result claims.",
            )
        )
    return variants


def _build_image_plan(launch_kit: LaunchKit, playbook: dict[str, Any]) -> VariantContentPayload:
    sequence = playbook.get("suggested_image_sequence") or ["main product", "detail", "use case"]
    proof = playbook.get("required_visual_proof") or []
    return VariantContentPayload(
        content={
            "platform": LaunchKitPlatform.BOTH.value,
            "platform_label": PLATFORM_LABELS[LaunchKitPlatform.BOTH.value],
            "image_sequence": [
                {"slot": index + 1, "purpose": purpose, "proof_required": proof[index] if index < len(proof) else None}
                for index, purpose in enumerate(sequence[:6])
            ],
            "cover_guidance": (
                f"Use {launch_kit.product.name} as the visual anchor with clean proof labels, "
                "not decorative AI clutter."
            ),
            "avoid": playbook.get("risky_claims") or [],
        },
        why_it_should_convert="Marketplace buyers scan images before reading; this sequence makes proof visible first.",
        buyer_objection_addressed=", ".join(playbook.get("buyer_objections") or []),
        platform_fit="Reusable for Shopee image carousel and TikTok Shop product cards.",
        risk="Requires real product image review before export.",
    )


def _build_quality_score(
    summary: GeneratedSummaryPayload,
    playbook: dict[str, Any],
    variants: list[VariantContentPayload],
) -> LaunchQualityScorePayload:
    missing_count = len(summary.missing_facts)
    image_coverage = 85 if playbook.get("required_visual_proof") else 50
    title_strength = 80 if variants else 0
    claim_risk = 90 if playbook.get("risky_claims") else 75
    if summary.risky_claims:
        claim_risk = min(claim_risk, 55)
    objection_coverage = 85 if playbook.get("buyer_objections") else 50
    platform_fit = 85 if len(variants) >= 2 else 72
    missing_score = max(35, 100 - missing_count * 18)
    overall = round(
        (missing_score + title_strength + image_coverage + claim_risk + objection_coverage + platform_fit) / 6
    )
    warnings = [*summary.missing_facts, *summary.risky_claims]
    if playbook.get("risky_claims"):
        warnings.append("Review risky claims list before publishing any generated copy.")
    return LaunchQualityScorePayload(
        overall=overall,
        missing_facts=missing_score,
        title_strength=title_strength,
        image_coverage=image_coverage,
        claim_risk=claim_risk,
        buyer_objection_coverage=objection_coverage,
        platform_fit=platform_fit,
        generic_wording_risk=25,
        warnings=warnings,
    )


def _persist_generated_outputs(
    session: Session,
    *,
    launch_kit: LaunchKit,
    summary: GeneratedSummaryPayload,
    selected_angle: SelectedAnglePayload,
    variants: list[VariantContentPayload],
    quality_score: LaunchQualityScorePayload,
    playbook: dict[str, Any],
) -> None:
    launch_kit.variants.clear()
    launch_kit.quality_scores.clear()
    launch_kit.exports.clear()
    session.flush()

    launch_kit.generated_summary_json = summary.model_dump(mode="json")
    launch_kit.selected_angle_json = selected_angle.model_dump(mode="json")
    launch_kit.buyer_angle_key = selected_angle.key

    variant_rows: list[LaunchKitVariant] = []
    for variant in variants:
        content = variant.content
        kind = LaunchKitVariantKind.IMAGE_PLAN if "image_sequence" in content else LaunchKitVariantKind.FULL_KIT
        platform = content.get("platform") or LaunchKitPlatform.BOTH.value
        row = LaunchKitVariant(
            launch_kit_id=launch_kit.id,
            kind=kind,
            platform=LaunchKitPlatform(platform),
            content_json=variant.model_dump(mode="json"),
            score_json={"quality_hint": quality_score.overall},
            selected=True,
        )
        session.add(row)
        variant_rows.append(row)
    session.flush()

    checklist = [
        "Verify price, stock, shipping fee, and variants in Seller Center.",
        "Replace any placeholder facts with supplier-confirmed details.",
        "Check every claim against product packaging and marketplace policy.",
        *[f"Guardrail: {item}" for item in summary.risky_claims],
        *[f"Image proof: {item}" for item in (playbook.get("required_visual_proof") or [])],
    ]
    snapshot = ExportSnapshotPayload(
        selected_variant_ids=[row.id for row in variant_rows],
        checklist_items=checklist,
    )
    launch_kit.export_snapshot_json = {
        **snapshot.model_dump(mode="json"),
        "manual_export": _build_manual_export_payload(launch_kit, variant_rows, checklist),
    }
    session.add(LaunchQualityScore(launch_kit_id=launch_kit.id, score_json=quality_score.model_dump(mode="json")))
    session.add(
        LaunchKitExport(
            launch_kit_id=launch_kit.id,
            export_type=LaunchKitExportType.MARKDOWN,
            status=LaunchKitExportStatus.READY,
            storage_path=None,
        )
    )
    session.add(
        LaunchKitExport(
            launch_kit_id=launch_kit.id,
            export_type=LaunchKitExportType.CHECKLIST,
            status=LaunchKitExportStatus.READY,
            storage_path=None,
        )
    )
    session.commit()


def _build_manual_export_payload(
    launch_kit: LaunchKit,
    variants: list[LaunchKitVariant],
    checklist: list[str],
) -> dict[str, Any]:
    platform_blocks = []
    for row in variants:
        content = row.content_json.get("content", {})
        if "title" not in content:
            continue
        platform_blocks.append(
            {
                "platform": content.get("platform"),
                "title": content.get("title"),
                "hook": content.get("hook"),
                "description": content.get("description"),
                "bullet_points": content.get("bullet_points", []),
                "hashtags": content.get("hashtags", []),
            }
        )
    return {
        "product_name": launch_kit.product.name,
        "platform_blocks": platform_blocks,
        "checklist": checklist,
    }
