from __future__ import annotations

from io import BytesIO
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import PosterKind


def _load_font(font_path: Path, size: int) -> ImageFont.FreeTypeFont | ImageFont.ImageFont:
    try:
        return ImageFont.truetype(str(font_path), size=size)
    except OSError:
        return ImageFont.load_default()


def _fit_source_image(source_path: Path, size: tuple[int, int]) -> Image.Image:
    image = Image.open(source_path).convert("RGBA")
    image.thumbnail(size, Image.Resampling.LANCZOS)
    canvas = Image.new("RGBA", size, (255, 255, 255, 0))
    left = (size[0] - image.width) // 2
    top = (size[1] - image.height) // 2
    canvas.alpha_composite(image, (left, top))
    return canvas


def _draw_wrapped_text(
    draw: ImageDraw.ImageDraw,
    text: str,
    font: ImageFont.ImageFont,
    box: tuple[int, int, int, int],
    fill: tuple[int, int, int],
    line_spacing: int = 8,
) -> None:
    x1, y1, x2, y2 = box
    max_width = x2 - x1
    lines: list[str] = []
    current = ""
    for char in text:
        probe = current + char
        probe_box = draw.textbbox((0, 0), probe, font=font)
        if probe_box[2] - probe_box[0] <= max_width:
            current = probe
            continue
        if current:
            lines.append(current)
        current = char
    if current:
        lines.append(current)

    cursor_y = y1
    for line in lines:
        bbox = draw.textbbox((0, 0), line, font=font)
        line_height = bbox[3] - bbox[1]
        if cursor_y + line_height > y2:
            break
        draw.text((x1, cursor_y), line, font=font, fill=fill)
        cursor_y += line_height + line_spacing


class PosterRenderer:
    def __init__(self, font_path: Path | None = None) -> None:
        self.font_path = font_path or get_runtime_settings().poster_font_path

    def render(self, payload: PosterGenerationInput, kind: PosterKind) -> bytes:
        if kind == PosterKind.MAIN_IMAGE:
            image = self._render_main_image(payload)
        else:
            image = self._render_promo_poster(payload)

        buffer = BytesIO()
        image.save(buffer, format="PNG")
        return buffer.getvalue()

    def _render_main_image(self, payload: PosterGenerationInput) -> Image.Image:
        width, height = 1080, 1080
        canvas = Image.new("RGBA", (width, height), (248, 247, 244, 255))
        draw = ImageDraw.Draw(canvas)

        draw.rectangle((0, 0, width, 180), fill=(18, 18, 18, 255))
        draw.rectangle((70, 220, 1010, 1010), fill=(255, 255, 255, 255))

        product_img = _fit_source_image(payload.source_image, (760, 540))
        canvas.alpha_composite(product_img, (160, 330))

        headline_font = _load_font(self.font_path, 58)
        point_font = _load_font(self.font_path, 34)
        tag_font = _load_font(self.font_path, 28)

        _draw_wrapped_text(
            draw,
            payload.poster_headline,
            headline_font,
            (70, 36, 830, 150),
            (255, 255, 255),
        )
        for index, point in enumerate(payload.selling_points[:3]):
            top = 880 + index * 54
            draw.rounded_rectangle((70, top, 1010, top + 40), radius=20, fill=(244, 75, 74, 255))
            _draw_wrapped_text(draw, point, tag_font, (96, top + 5, 980, top + 34), (255, 255, 255))

        if payload.price:
            draw.rounded_rectangle((760, 58, 980, 142), radius=30, fill=(244, 75, 74, 255))
            draw.text((796, 78), f"参考价 {payload.price}", font=point_font, fill=(255, 255, 255))
        return canvas

    def _render_promo_poster(self, payload: PosterGenerationInput) -> Image.Image:
        width, height = 1080, 1440
        canvas = Image.new("RGBA", (width, height), (252, 250, 246, 255))
        draw = ImageDraw.Draw(canvas)

        draw.rectangle((0, 0, width, 380), fill=(28, 28, 28, 255))
        draw.rounded_rectangle((70, 460, 1010, 1360), radius=42, fill=(255, 255, 255, 255))
        draw.ellipse((760, 70, 1040, 350), fill=(244, 75, 74, 255))

        title_font = _load_font(self.font_path, 74)
        body_font = _load_font(self.font_path, 36)
        cta_font = _load_font(self.font_path, 42)

        _draw_wrapped_text(draw, payload.poster_headline, title_font, (70, 60, 700, 250), (255, 255, 255))
        _draw_wrapped_text(draw, payload.title, body_font, (70, 260, 700, 340), (230, 230, 230))

        product_img = _fit_source_image(payload.source_image, (700, 640))
        canvas.alpha_composite(product_img, (190, 560))

        base_y = 1130
        for index, point in enumerate(payload.selling_points[:3]):
            draw.rounded_rectangle(
                (120, base_y + index * 62, 960, base_y + index * 62 + 46),
                radius=24,
                fill=(245, 239, 232, 255),
            )
            _draw_wrapped_text(
                draw,
                point,
                body_font,
                (150, base_y + 6 + index * 62, 920, base_y + 40 + index * 62),
                (60, 54, 48),
            )

        draw.rounded_rectangle((120, 1280, 960, 1360), radius=34, fill=(244, 75, 74, 255))
        _draw_wrapped_text(draw, payload.cta, cta_font, (180, 1298, 910, 1346), (255, 255, 255))
        return canvas
