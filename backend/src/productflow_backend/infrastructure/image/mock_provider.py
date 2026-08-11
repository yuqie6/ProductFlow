from __future__ import annotations

from io import BytesIO

from PIL import Image, ImageDraw, ImageFont

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.domain.enums import PosterKind
from productflow_backend.infrastructure.image.base import (
    GeneratedImagePayload,
    ImageProvider,
    WorkflowGeneratedImage,
    WorkflowImageRequest,
    WorkflowImageResult,
    map_generation_spec_to_pixel_size,
    parse_size,
)


def _load_font(font_path: str, size: int) -> ImageFont.FreeTypeFont | ImageFont.ImageFont:
    try:
        return ImageFont.truetype(font_path, size=size)
    except OSError:
        return ImageFont.load_default()


class MockImageProvider(ImageProvider):
    provider_name = "mock"
    prompt_version = "mock-image-v1"

    def __init__(self) -> None:
        settings = get_runtime_settings()
        self.main_image_size = settings.image_main_image_size
        self.promo_poster_size = settings.image_promo_poster_size
        self.poster_font_path = str(settings.poster_font_path)

    def generate_poster_image(
        self,
        poster: PosterGenerationInput,
        kind: PosterKind,
    ) -> tuple[GeneratedImagePayload, str]:
        size = poster.image_size or (self.main_image_size if kind == PosterKind.MAIN_IMAGE else self.promo_poster_size)
        width, height = parse_size(size)

        background = (28, 28, 28, 255) if kind == PosterKind.PROMO_POSTER else (248, 247, 244, 255)
        foreground = (255, 255, 255) if kind == PosterKind.PROMO_POSTER else (24, 24, 27)
        accent = (244, 75, 74, 255)

        image = Image.new("RGBA", (width, height), background)
        draw = ImageDraw.Draw(image)
        title_font = _load_font(self.poster_font_path, 48 if kind == PosterKind.MAIN_IMAGE else 56)
        body_font = _load_font(self.poster_font_path, 28 if kind == PosterKind.MAIN_IMAGE else 32)

        draw.rounded_rectangle((48, 48, width - 48, height - 48), radius=36, outline=accent, width=6)
        context_lines = [line.strip() for line in (poster.structured_copy_context or "").splitlines() if line.strip()]
        headline = context_lines[0].removeprefix("摘要：") if context_lines else poster.product_name
        points = context_lines[1:4]

        draw.text((84, 90), headline[:28], font=title_font, fill=foreground)
        draw.text((84, 180), poster.product_name[:32], font=body_font, fill=foreground)

        for index, point in enumerate(points[:3]):
            top = 280 + index * 72
            draw.rounded_rectangle((84, top, width - 84, top + 48), radius=18, fill=accent)
            draw.text((108, top + 10), point[:28], font=body_font, fill=(255, 255, 255))

        draw.rounded_rectangle((84, height - 140, width - 84, height - 72), radius=28, fill=foreground)
        draw.text(
            (120, height - 126),
            (poster.instruction or "生成图片")[:30],
            font=body_font,
            fill=background,
        )
        draw.text(
            (84, height - 210),
            f"Refs: {len(poster.reference_images)}",
            font=body_font,
            fill=foreground,
        )

        buffer = BytesIO()
        image.save(buffer, format="PNG")
        return (
            GeneratedImagePayload(
                kind=kind,
                bytes_data=buffer.getvalue(),
                mime_type="image/png",
                width=width,
                height=height,
                variant_label=f"mock-generated-r{len(poster.reference_images)}",
            ),
            "mock-image-v1",
        )

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
