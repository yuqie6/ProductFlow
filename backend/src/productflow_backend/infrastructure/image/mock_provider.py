from __future__ import annotations

from io import BytesIO

from PIL import Image, ImageDraw, ImageFont

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import PosterKind
from productflow_backend.infrastructure.image.base import GeneratedImagePayload, ImageProvider, parse_size


def _load_font(size: int) -> ImageFont.FreeTypeFont | ImageFont.ImageFont:
    try:
        return ImageFont.truetype(str(get_runtime_settings().poster_font_path), size=size)
    except OSError:
        return ImageFont.load_default()


class MockImageProvider(ImageProvider):
    provider_name = "mock"
    prompt_version = "mock-image-v1"

    def generate_poster_image(
        self,
        poster: PosterGenerationInput,
        kind: PosterKind,
    ) -> tuple[GeneratedImagePayload, str]:
        settings = get_runtime_settings()
        size = poster.image_size or (
            settings.image_main_image_size if kind == PosterKind.MAIN_IMAGE else settings.image_promo_poster_size
        )
        width, height = parse_size(size)

        background = (28, 28, 28, 255) if kind == PosterKind.PROMO_POSTER else (248, 247, 244, 255)
        foreground = (255, 255, 255) if kind == PosterKind.PROMO_POSTER else (24, 24, 27)
        accent = (244, 75, 74, 255)

        image = Image.new("RGBA", (width, height), background)
        draw = ImageDraw.Draw(image)
        title_font = _load_font(48 if kind == PosterKind.MAIN_IMAGE else 56)
        body_font = _load_font(28 if kind == PosterKind.MAIN_IMAGE else 32)

        draw.rounded_rectangle((48, 48, width - 48, height - 48), radius=36, outline=accent, width=6)
        draw.text((84, 90), poster.poster_headline[:28], font=title_font, fill=foreground)
        draw.text((84, 180), poster.product_name[:32], font=body_font, fill=foreground)

        for index, point in enumerate(poster.selling_points[:3]):
            top = 280 + index * 72
            draw.rounded_rectangle((84, top, width - 84, top + 48), radius=18, fill=accent)
            draw.text((108, top + 10), point[:28], font=body_font, fill=(255, 255, 255))

        draw.rounded_rectangle((84, height - 140, width - 84, height - 72), radius=28, fill=foreground)
        draw.text((120, height - 126), poster.cta[:30], font=body_font, fill=background)
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
