from __future__ import annotations

from typing import Any

from pydantic import ValidationError

from productflow_backend.application.copy_payloads import normalize_copy_node_config
from productflow_backend.application.image_generation_core import normalize_image_generation_tool_options
from productflow_backend.application.product_workflow.context import image_size_from_config, optional_config_text
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec
from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError


def normalize_workflow_node_config(
    node_type: WorkflowNodeType,
    config_json: dict[str, Any] | None,
) -> dict[str, Any]:
    config = dict(config_json or {})
    if node_type == WorkflowNodeType.COPY_GENERATION:
        try:
            normalized_copy_config = normalize_copy_node_config(config).model_dump(mode="json")
        except ValueError as exc:
            raise BusinessValidationError(str(exc)) from exc
        return {**config, **normalized_copy_config}
    if node_type == WorkflowNodeType.IMAGE_GENERATION:
        if "delivery_spec" in config and config["delivery_spec"] is not None:
            try:
                config["delivery_spec"] = DeliverySpec.model_validate(config["delivery_spec"]).model_dump(
                    mode="json"
                )
            except ValidationError as exc:
                raise BusinessValidationError("DeliverySpec 不符合 schema") from exc
        try:
            normalized_size = image_size_from_config(config)
        except ValueError as exc:
            raise BusinessValidationError(str(exc)) from exc
        if normalized_size is not None:
            config["size"] = normalized_size
        if "visible_text_language_hint" in config:
            visible_text_language_hint = optional_config_text(config, "visible_text_language_hint")
            if visible_text_language_hint:
                config["visible_text_language_hint"] = visible_text_language_hint
            else:
                config.pop("visible_text_language_hint", None)
        if "tool_options" in config:
            raw_tool_options = config.get("tool_options")
            config["tool_options"] = normalize_image_generation_tool_options(
                raw_tool_options if isinstance(raw_tool_options, dict) else None
            )
    return config
