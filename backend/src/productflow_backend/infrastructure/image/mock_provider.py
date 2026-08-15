from __future__ import annotations

from io import BytesIO

from PIL import Image

from productflow_backend.infrastructure.image.base import (
    ImageProvider,
    WorkflowGeneratedImage,
    WorkflowImageRequest,
    WorkflowImageResult,
    map_generation_spec_to_pixel_size,
    parse_size,
)


class MockImageProvider(ImageProvider):
    provider_name = "mock"
    prompt_version = "mock-image-v1"

    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        size = map_generation_spec_to_pixel_size(request.generation_spec)
        width, height = parse_size(size)
        image = Image.new("RGB", (width, height), (238, 240, 242))
        buffer = BytesIO()
        image.save(buffer, format="PNG")
        effective_parameters = {
            "adapter": "mock",
            "size": size,
            "aspect_ratio": request.generation_spec.aspect_ratio,
            "resolution_tier": request.generation_spec.resolution_tier,
            "quality": request.generation_spec.quality_intent,
            "reference_fidelity": request.generation_spec.reference_fidelity,
            "background": request.generation_spec.background_intent,
            "reference_image_count": len(request.references),
        }
        provider_request_json = {
            "model": "mock-image-v2",
            "prompt": request.compiled_prompt,
            "effective_parameters": effective_parameters,
            "references": [
                {
                    "asset_id": reference.asset_id,
                    "role": reference.role,
                    "label": reference.label,
                    "filename": reference.filename,
                    "mime_type": reference.mime_type,
                    "byte_count": len(reference.bytes_data),
                }
                for reference in request.references
            ],
        }
        return WorkflowImageResult(
            images=(WorkflowGeneratedImage(bytes_data=buffer.getvalue(), mime_type="image/png"),),
            model="mock-image-v2",
            provider_status="completed",
            effective_parameters=effective_parameters,
            provider_request_json=provider_request_json,
            provider_output_json={"status": "completed"},
        )
