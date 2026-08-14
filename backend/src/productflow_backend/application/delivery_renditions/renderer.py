from __future__ import annotations

import warnings
from io import BytesIO

from PIL import Image, ImageColor, ImageOps, UnidentifiedImageError

from productflow_backend.application.delivery_renditions.contracts import (
    DELIVERY_FORMAT_MIME_TYPES,
    DELIVERY_RENDITION_QUALITY_LEVELS,
    RenderedDeliveryRendition,
    normalize_delivery_spec,
)
from productflow_backend.application.media_assets import inspect_image_bytes
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec
from productflow_backend.domain.errors import BusinessValidationError

_CROP_CENTERING = {
    "center": (0.5, 0.5),
    "top": (0.5, 0.0),
    "bottom": (0.5, 1.0),
    "left": (0.0, 0.5),
    "right": (1.0, 0.5),
}


def render_delivery_rendition(
    source_bytes: bytes,
    delivery_spec: DeliverySpec | dict[str, object],
) -> RenderedDeliveryRendition:
    normalized = normalize_delivery_spec(delivery_spec)
    spec = normalized.spec
    image = _decode_source_image(source_bytes)
    try:
        rendered = _resize_image(image, spec)
        output = _encode_image(rendered, spec)
    finally:
        image.close()

    expected_mime = DELIVERY_FORMAT_MIME_TYPES[spec.format]
    metadata = inspect_image_bytes(output, expected_mime_type=expected_mime)
    if (metadata.width, metadata.height) != (spec.width, spec.height):
        raise BusinessValidationError("交付图编码后的实际尺寸不符合 DeliverySpec")
    if spec.max_byte_size is not None and metadata.byte_size > spec.max_byte_size:
        raise BusinessValidationError("交付图无法在保持尺寸和格式的前提下满足最大字节限制")
    return RenderedDeliveryRendition(bytes_data=output, metadata=metadata)


def _decode_source_image(source_bytes: bytes) -> Image.Image:
    if not source_bytes:
        raise BusinessValidationError("交付派生原图内容为空")
    try:
        with warnings.catch_warnings():
            warnings.simplefilter("error", Image.DecompressionBombWarning)
            with Image.open(BytesIO(source_bytes)) as source:
                source.load()
                transposed = ImageOps.exif_transpose(source)
                has_alpha = transposed.mode in {"RGBA", "LA"} or "transparency" in transposed.info
                return transposed.convert("RGBA" if has_alpha else "RGB")
    except (Image.DecompressionBombError, Image.DecompressionBombWarning) as exc:
        raise BusinessValidationError("交付派生原图像素规模过大") from exc
    except (OSError, UnidentifiedImageError) as exc:
        raise BusinessValidationError("交付派生原图不是可解码图片") from exc


def _resize_image(image: Image.Image, spec: DeliverySpec) -> Image.Image:
    target_size = (spec.width, spec.height)
    if spec.fit == "cover":
        centering = _CROP_CENTERING[spec.crop_anchor or "center"]
        return ImageOps.fit(image, target_size, method=Image.Resampling.LANCZOS, centering=centering)

    contained = ImageOps.contain(image, target_size, method=Image.Resampling.LANCZOS)
    if spec.background_color is not None:
        canvas_mode = "RGB"
        background = ImageColor.getrgb(spec.background_color)
    elif spec.format == "jpeg":
        canvas_mode = "RGB"
        background = (255, 255, 255)
    else:
        canvas_mode = "RGBA"
        background = (0, 0, 0, 0)
    canvas = Image.new(canvas_mode, target_size, background)
    offset = ((spec.width - contained.width) // 2, (spec.height - contained.height) // 2)
    if contained.mode == "RGBA" and canvas.mode == "RGB":
        canvas.paste(contained, offset, contained.getchannel("A"))
    else:
        canvas.paste(contained, offset, contained if contained.mode == "RGBA" else None)
    contained.close()
    return canvas


def _encode_image(image: Image.Image, spec: DeliverySpec) -> bytes:
    try:
        if spec.format == "png":
            encoded = _encode_once(image, image_format="PNG", compress_level=9)
            if spec.max_byte_size is not None and len(encoded) > spec.max_byte_size:
                raise BusinessValidationError("PNG 交付图无法在不量化颜色的前提下满足最大字节限制")
            return encoded

        image_format = "JPEG" if spec.format == "jpeg" else "WEBP"
        prepared = _flatten_for_jpeg(image) if image_format == "JPEG" else image
        try:
            if spec.max_byte_size is None:
                return _encode_lossy(prepared, image_format=image_format, quality=95)
            for quality in DELIVERY_RENDITION_QUALITY_LEVELS:
                encoded = _encode_lossy(prepared, image_format=image_format, quality=quality)
                if len(encoded) <= spec.max_byte_size:
                    return encoded
        finally:
            if prepared is not image:
                prepared.close()
        raise BusinessValidationError("交付图无法在保持尺寸和格式的前提下满足最大字节限制")
    finally:
        image.close()


def _flatten_for_jpeg(image: Image.Image) -> Image.Image:
    if image.mode == "RGB":
        return image
    flattened = Image.new("RGB", image.size, (255, 255, 255))
    if image.mode == "RGBA":
        flattened.paste(image, mask=image.getchannel("A"))
    else:
        flattened.paste(image.convert("RGB"))
    return flattened


def _encode_lossy(image: Image.Image, *, image_format: str, quality: int) -> bytes:
    if image_format == "JPEG":
        return _encode_once(
            image,
            image_format=image_format,
            quality=quality,
            optimize=True,
            progressive=False,
            subsampling=0,
        )
    return _encode_once(
        image,
        image_format=image_format,
        quality=quality,
        method=6,
        exact=True,
    )


def _encode_once(image: Image.Image, *, image_format: str, **options: object) -> bytes:
    output = BytesIO()
    try:
        image.save(output, format=image_format, **options)
    except (KeyError, OSError, ValueError) as exc:
        raise BusinessValidationError(f"当前运行环境不支持 {image_format} 交付编码") from exc
    return output.getvalue()
