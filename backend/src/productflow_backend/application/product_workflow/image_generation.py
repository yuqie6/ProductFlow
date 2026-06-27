from __future__ import annotations

import logging
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor, as_completed
from concurrent.futures import TimeoutError as FuturesTimeoutError
from pathlib import Path

from sqlalchemy.orm import Session

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.application.copy_payloads import copy_payload_context_text, validate_copy_payload
from productflow_backend.application.image_generation_core import build_stored_image_reference_payload
from productflow_backend.application.image_generation_failures import classify_image_generation_failure
from productflow_backend.application.product_workflow.artifacts import (
    GeneratedWorkflowImage,
    create_context_copy_set,
    fill_reference_node,
)
from productflow_backend.application.product_workflow.context import (
    collect_incoming_context,
    downstream_reference_nodes,
    effective_product_context,
    image_instruction_with_context,
    image_size_from_config,
    image_tool_options_from_config,
    optional_config_text,
    poster_kind_from_config,
    reference_assets_for_image_generation,
)
from productflow_backend.application.product_workflow.run_state import WorkflowSafeExecutionError
from productflow_backend.application.product_workflow_dependencies import (
    PosterRendererFactory,
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.time import now_utc
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import PosterKind, SourceAssetKind
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import (
    CopySet,
    PosterVariant,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
)
from productflow_backend.infrastructure.image.base import ImageProvider, infer_extension
from productflow_backend.infrastructure.provider_config import (
    is_real_image_provider_kind,
    resolve_image_provider_config,
)
from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)

WORKFLOW_IMAGE_GENERATION_FAILURE = "图片生成失败，请稍后重试"
WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE = "图片生成超时，请稍后重试"


class WorkflowImageGenerationTimeoutError(WorkflowSafeExecutionError):
    """Raised when workflow image provider calls exceed the project timeout boundary."""


class WorkflowImageGenerationProviderError(WorkflowSafeExecutionError):
    """Raised when workflow image provider failures must be hidden behind a safe user message."""


def workflow_image_generation_provider_timeout_seconds() -> float:
    return float(get_runtime_settings().workflow_image_generation_provider_timeout_seconds)


def effective_workflow_image_generation_mode(configured_mode: str, image_provider_kind: str | None) -> str:
    if is_real_image_provider_kind(image_provider_kind):
        return "generated"
    return configured_mode


def call_with_timeout[T](call: Callable[[], T], *, timeout_seconds: float, timeout_message: str) -> T:
    executor = ThreadPoolExecutor(max_workers=1)
    future = executor.submit(call)
    try:
        result = future.result(timeout=timeout_seconds)
    except FuturesTimeoutError as exc:
        future.cancel()
        executor.shutdown(wait=False, cancel_futures=True)
        raise WorkflowImageGenerationTimeoutError(timeout_message) from exc
    except BaseException:
        future.cancel()
        executor.shutdown(wait=False, cancel_futures=True)
        raise
    else:
        executor.shutdown(wait=True)
        return result


def _is_workflow_context_copy_set(copy_set: CopySet) -> bool:
    if copy_set.provider_name == "workflow_context":
        return True
    payload = copy_set.structured_payload
    return isinstance(payload, dict) and payload.get("purpose") == "workflow_context"


def execute_workflow_image_generation(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> dict[str, object]:
    dependencies = dependencies or default_workflow_execution_dependencies()
    product = workflow.product
    incoming_context = collect_incoming_context(workflow, node.id, include_transitive_product_context=True)
    product_context = effective_product_context(workflow, node.id, include_transitive=True)
    downstream_nodes = downstream_reference_nodes(workflow, node.id)
    if not downstream_nodes:
        raise BusinessValidationError("请先把生图节点连接到至少一个图片/参考图节点，再运行图片生成")

    linked_copy_set_id = optional_config_text(node.config_json, "copy_set_id") or incoming_context.copy_set_id
    copy_set = session.get(CopySet, linked_copy_set_id) if linked_copy_set_id else None
    has_linked_copy_input = (
        linked_copy_set_id is not None and copy_set is not None and copy_set.product_id == product.id
    )
    has_real_copy_context = (
        has_linked_copy_input
        and copy_set is not None
        and copy_set.product_id == product.id
        and not _is_workflow_context_copy_set(copy_set)
    )
    if copy_set is None or copy_set.product_id != product.id:
        copy_set = create_context_copy_set(session, product=product, product_context=product_context, node=node)
    structured_copy_context = None
    if has_real_copy_context and isinstance(copy_set.structured_payload, dict):
        try:
            structured_copy_context = copy_payload_context_text(validate_copy_payload(copy_set.structured_payload))
        except ValueError:
            structured_copy_context = None

    storage = LocalStorage()
    reference_assets = reference_assets_for_image_generation(
        session,
        workflow,
        incoming_context.image_asset_ids,
        incoming_context.poster_variant_ids,
    )
    reference_payload = build_stored_image_reference_payload(
        reference_assets,
        resolve_storage_path=storage.resolve,
    )
    render_input = PosterGenerationInput(
        copy_prompt_mode="copy" if structured_copy_context else "image_edit",
        product_name=product_context["name"] or "",
        category=product_context["category"],
        price=product_context["price"],
        source_note=product_context["source_note"],
        instruction=image_instruction_with_context(node, incoming_context.text_contexts),
        image_size=image_size_from_config(node.config_json),
        tool_options=image_tool_options_from_config(node.config_json),
        structured_copy_context=structured_copy_context,
        source_image=reference_payload.source_image,
        reference_images=reference_payload.reference_images,
    )
    poster_ids: list[str] = []
    filled_source_asset_ids: list[str] = []
    filled_reference_node_ids: list[str] = []
    provider_results: list[dict[str, object]] = []
    settings = get_runtime_settings()
    kind = poster_kind_from_config(node.config_json)
    image_provider_config = (
        None if settings.poster_generation_mode == "generated" else resolve_image_provider_config()
    )
    poster_generation_mode = effective_workflow_image_generation_mode(
        settings.poster_generation_mode,
        image_provider_config.provider_kind if image_provider_config is not None else None,
    )
    image_providers: list[ImageProvider] | None = None
    if poster_generation_mode == "generated":
        first_provider = dependencies.image_provider()
        if callable(getattr(first_provider, "generate_poster_images", None)):
            image_providers = [first_provider]
        else:
            image_providers = [first_provider, *[dependencies.image_provider() for _ in downstream_nodes[1:]]]
    generated_images = generate_workflow_images_concurrently(
        render_input=render_input,
        kind=kind,
        target_count=len(downstream_nodes),
        poster_generation_mode=poster_generation_mode,
        poster_font_path=settings.poster_font_path,
        image_providers=image_providers,
        renderer_factory=dependencies.poster_renderer,
    )
    for generated_image, target_node in zip(generated_images, downstream_nodes, strict=True):
        content = generated_image.content
        mime_type = generated_image.mime_type
        relative_path = storage.save_generated_image(
            product.id,
            f"workflow-{kind.value}-{generated_image.target_index}",
            content,
            suffix=infer_extension(mime_type),
        )
        poster = PosterVariant(
            product_id=product.id,
            copy_set_id=copy_set.id,
            kind=kind,
            template_name=generated_image.template_name,
            storage_path=relative_path,
            mime_type=mime_type,
            width=generated_image.width,
            height=generated_image.height,
        )
        session.add(poster)
        session.flush()
        poster_ids.append(poster.id)

        filename = f"reference-{generated_image.target_index}{infer_extension(mime_type)}"
        reference_path = storage.save_reference_upload(product.id, filename, content)
        asset = SourceAsset(
            product_id=product.id,
            kind=SourceAssetKind.REFERENCE_IMAGE,
            original_filename=filename,
            mime_type=mime_type,
            storage_path=reference_path,
            source_poster_variant_id=poster.id,
        )
        session.add(asset)
        session.flush()
        filled_source_asset_ids.append(asset.id)
        filled_reference_node_ids.append(target_node.id)
        fill_reference_node(target_node, asset, source_poster_variant_id=poster.id)
        provider_result = {
            "target_index": generated_image.target_index,
            "provider_name": generated_image.provider_name,
            "model_name": generated_image.model_name,
            "provider_response_id": generated_image.provider_response_id,
            "provider_response_status": generated_image.provider_response_status,
        }
        if isinstance(generated_image.provider_output_json, dict):
            metadata = generated_image.provider_output_json.get("_productflow")
            if isinstance(metadata, dict):
                actual_size = metadata.get("actual_size")
                notes = metadata.get("notes")
                if isinstance(actual_size, str):
                    provider_result["actual_size"] = actual_size
                if isinstance(notes, list):
                    provider_result["notes"] = [item for item in notes if isinstance(item, str)][:4]
        safe_provider_result = {key: value for key, value in provider_result.items() if value is not None}
        if safe_provider_result:
            provider_results.append(safe_provider_result)
    product.updated_at = now_utc()
    return {
        "copy_set_id": copy_set.id,
        "generated_poster_variant_ids": poster_ids,
        "filled_source_asset_ids": filled_source_asset_ids,
        "filled_reference_node_ids": filled_reference_node_ids,
        "provider_results": provider_results,
        "target_count": len(downstream_nodes),
        "size": image_size_from_config(node.config_json),
        "instruction": optional_config_text(node.config_json, "instruction"),
        "context_summary": {
            "product_context": product_context,
            "copy_set_id": copy_set.id,
            "copy_prompt_mode": render_input.copy_prompt_mode,
            "upstream_text_count": len(incoming_context.text_contexts),
            "reference_image_count": len(incoming_context.image_asset_ids),
            "poster_variant_count": len(incoming_context.poster_variant_ids),
        },
        "context_sources": incoming_context.text_sources[:8],
        "summary": f"已填充 {len(filled_reference_node_ids)} 个参考图",
    }


def generate_workflow_images_concurrently(
    *,
    render_input: PosterGenerationInput,
    kind: PosterKind,
    target_count: int,
    poster_generation_mode: str,
    poster_font_path: Path,
    image_providers: list[ImageProvider] | None,
    renderer_factory: PosterRendererFactory | None = None,
) -> list[GeneratedWorkflowImage]:
    if target_count <= 0:
        return []
    dependencies = default_workflow_execution_dependencies()
    renderer_factory = renderer_factory or dependencies.poster_renderer

    def generated_workflow_image_from_payload(
        *,
        target_index: int,
        image_provider: ImageProvider,
        generated_image,
        image_model: str,
    ) -> GeneratedWorkflowImage:
        return GeneratedWorkflowImage(
            target_index=target_index,
            content=generated_image.bytes_data,
            width=generated_image.width,
            height=generated_image.height,
            template_name=f"workflow:{image_provider.provider_name}:{generated_image.variant_label}:{image_model}",
            mime_type=generated_image.mime_type,
            provider_name=image_provider.provider_name,
            model_name=image_model,
            provider_response_id=generated_image.provider_response_id,
            provider_response_status=generated_image.provider_response_status,
            provider_output_json=generated_image.provider_output_json,
        )

    def raise_provider_error(exc: Exception, *, image_provider: ImageProvider, target_index: int) -> None:
        logger.warning(
            (
                "工作流图片供应商生成失败: target_index=%s provider=%s model=%s "
                "copy_prompt_mode=%s exception_class=%s"
            ),
            target_index,
            getattr(image_provider, "provider_name", None),
            getattr(image_provider, "model", None),
            render_input.copy_prompt_mode,
            type(exc).__name__,
        )
        decision = classify_image_generation_failure(exc, generic_message=WORKFLOW_IMAGE_GENERATION_FAILURE)
        raise WorkflowImageGenerationProviderError(
            decision.reason,
            retryable=decision.retryable,
            retry_hint=decision.retry_hint,
            failure_category=decision.category,
        ) from exc

    if poster_generation_mode == "generated" and target_count > 1 and image_providers:
        image_provider = image_providers[0]
        batch_generate = getattr(image_provider, "generate_poster_images", None)
        if callable(batch_generate):

            def generate_batch() -> list[GeneratedWorkflowImage]:
                try:
                    generated_payloads = batch_generate(render_input, kind, target_count)
                except WorkflowSafeExecutionError:
                    raise
                except Exception as exc:  # noqa: BLE001
                    raise_provider_error(exc, image_provider=image_provider, target_index=1)
                if len(generated_payloads) != target_count:
                    raise WorkflowImageGenerationProviderError(WORKFLOW_IMAGE_GENERATION_FAILURE, retryable=True)
                return [
                    generated_workflow_image_from_payload(
                        target_index=target_index,
                        image_provider=image_provider,
                        generated_image=generated_image,
                        image_model=image_model,
                    )
                    for target_index, (generated_image, image_model) in enumerate(generated_payloads, start=1)
                ]

            return call_with_timeout(
                generate_batch,
                timeout_seconds=workflow_image_generation_provider_timeout_seconds(),
                timeout_message=WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE,
            )

    def generate_one(target_index: int) -> GeneratedWorkflowImage:
        if poster_generation_mode == "generated":
            if image_providers is None:
                raise RuntimeError("图片生成供应商未初始化")
            image_provider = image_providers[target_index - 1]
            try:
                generated_image, image_model = image_provider.generate_poster_image(render_input, kind)
            except WorkflowSafeExecutionError:
                raise
            except Exception as exc:  # noqa: BLE001
                raise_provider_error(exc, image_provider=image_provider, target_index=target_index)
            return generated_workflow_image_from_payload(
                target_index=target_index,
                image_provider=image_provider,
                generated_image=generated_image,
                image_model=image_model,
            )

        renderer = renderer_factory(poster_font_path)
        return GeneratedWorkflowImage(
            target_index=target_index,
            content=renderer.render(render_input, kind),
            width=1080,
            height=1080 if kind == PosterKind.MAIN_IMAGE else 1440,
            template_name=f"workflow:{'default-main' if kind == PosterKind.MAIN_IMAGE else 'default-promo'}",
            mime_type="image/png",
        )

    if target_count == 1:
        if poster_generation_mode == "generated":
            return [
                call_with_timeout(
                    lambda: generate_one(1),
                    timeout_seconds=workflow_image_generation_provider_timeout_seconds(),
                    timeout_message=WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE,
                )
            ]
        return [generate_one(1)]
    executor = ThreadPoolExecutor(max_workers=target_count)
    futures = {executor.submit(generate_one, target_index): target_index for target_index in range(1, target_count + 1)}
    results: dict[int, GeneratedWorkflowImage] = {}
    try:
        timeout = (
            workflow_image_generation_provider_timeout_seconds() if poster_generation_mode == "generated" else None
        )
        for future in as_completed(futures, timeout=timeout):
            target_index = futures[future]
            results[target_index] = future.result()
    except FuturesTimeoutError as exc:
        for future in futures:
            future.cancel()
        executor.shutdown(wait=False, cancel_futures=True)
        raise WorkflowImageGenerationTimeoutError(WORKFLOW_IMAGE_GENERATION_TIMEOUT_FAILURE) from exc
    except BaseException:
        for future in futures:
            future.cancel()
        executor.shutdown(wait=False, cancel_futures=True)
        raise
    else:
        executor.shutdown(wait=True)
    return [results[target_index] for target_index in range(1, target_count + 1)]
