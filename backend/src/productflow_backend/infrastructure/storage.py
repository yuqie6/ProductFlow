from __future__ import annotations

import mimetypes
import shutil
from pathlib import Path
from typing import Literal
from uuid import uuid4

from PIL import Image, ImageOps, UnidentifiedImageError, features

from productflow_backend.config import get_settings

ImageVariantName = Literal["original", "preview", "thumbnail"]

_VARIANT_MAX_EDGE: dict[ImageVariantName, int] = {
    "original": 0,
    "preview": 1600,
    "thumbnail": 320,
}


class LocalStorage:
    """本地文件存储，按类型组织目录结构，自动派生缩略图。"""

    def __init__(self, root: Path | None = None) -> None:
        settings = get_settings()
        self.root = (root or settings.storage_root).resolve()
        self.root.mkdir(parents=True, exist_ok=True)

    def save_product_upload(
        self,
        product_id: str,
        filename: str,
        content: bytes,
    ) -> str:
        suffix = Path(filename).suffix.lower() or ".bin"
        relative = Path("products") / product_id / "source" / f"{uuid4()}{suffix}"
        self._write_relative(relative, content)
        self._warm_image_variants(relative.as_posix())
        return relative.as_posix()

    def save_reference_upload(
        self,
        product_id: str,
        filename: str,
        content: bytes,
    ) -> str:
        suffix = Path(filename).suffix.lower() or ".bin"
        relative = Path("products") / product_id / "reference" / f"{uuid4()}{suffix}"
        self._write_relative(relative, content)
        self._warm_image_variants(relative.as_posix())
        return relative.as_posix()

    def save_generated_image(
        self,
        product_id: str,
        poster_kind: str,
        content: bytes,
        suffix: str = ".png",
    ) -> str:
        relative = Path("products") / product_id / "posters" / f"{poster_kind}-{uuid4()}{suffix}"
        self._write_relative(relative, content)
        self._warm_image_variants(relative.as_posix())
        return relative.as_posix()

    def save_image_session_reference(
        self,
        session_id: str,
        filename: str,
        content: bytes,
    ) -> str:
        suffix = Path(filename).suffix.lower() or ".bin"
        relative = Path("image_sessions") / session_id / "reference" / f"{uuid4()}{suffix}"
        self._write_relative(relative, content)
        self._warm_image_variants(relative.as_posix())
        return relative.as_posix()

    def save_image_session_generated(
        self,
        session_id: str,
        content: bytes,
        suffix: str = ".png",
    ) -> str:
        relative = Path("image_sessions") / session_id / "generated" / f"{uuid4()}{suffix}"
        self._write_relative(relative, content)
        self._warm_image_variants(relative.as_posix())
        return relative.as_posix()

    def resolve(self, relative_path: str) -> Path:
        """相对路径转绝对路径，防路径穿越攻击。"""
        if Path(relative_path).is_absolute():
            raise ValueError("存储路径必须是相对路径")
        resolved = (self.root / relative_path).resolve()
        try:
            resolved.relative_to(self.root)
        except ValueError as exc:
            raise ValueError("存储路径越界") from exc
        return resolved

    def resolve_for_variant(
        self,
        relative_path: str,
        variant: ImageVariantName,
        *,
        fallback_media_type: str = "application/octet-stream",
    ) -> tuple[Path, str]:
        """解析资源路径，若 variant 不是 original 则按需生成缩略图。"""
        original = self.resolve(relative_path)
        if variant == "original":
            return original, self._guess_media_type(original, fallback=fallback_media_type)

        variant_path = self._variant_path(original, variant)
        if not variant_path.exists():
            try:
                self._generate_variant(original, variant_path, variant)
            except (OSError, UnidentifiedImageError, ValueError):
                return original, self._guess_media_type(original, fallback=fallback_media_type)

        return variant_path, self._guess_media_type(variant_path, fallback=fallback_media_type)

    def delete_image_with_variants(self, relative_path: str) -> None:
        original = self.resolve(relative_path)
        for variant in ("preview", "thumbnail"):
            variant_path = self._variant_path(original, variant)
            variant_path.unlink(missing_ok=True)
        original.unlink(missing_ok=True)
        self._remove_empty_variant_dir(original)

    def delete_image_session_tree(self, session_id: str) -> None:
        session_root = self.resolve((Path("image_sessions") / session_id).as_posix())
        if session_root.exists():
            shutil.rmtree(session_root)

    def delete_product_tree(self, product_id: str) -> None:
        product_root = self.resolve((Path("products") / product_id).as_posix())
        if product_root.exists():
            shutil.rmtree(product_root)

    def _write_relative(self, relative: Path, content: bytes) -> None:
        destination = self.root / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(content)

    def _warm_image_variants(self, relative_path: str) -> None:
        for variant in ("preview", "thumbnail"):
            try:
                self.resolve_for_variant(relative_path, variant)
            except ValueError:
                return

    def _variant_path(self, original_path: Path, variant: ImageVariantName) -> Path:
        relative = original_path.relative_to(self.root)
        output_suffix = self._variant_output_suffix()
        return self.root / relative.parent / ".variants" / f"{original_path.stem}.{variant}{output_suffix}"

    def _remove_empty_variant_dir(self, original_path: Path) -> None:
        variant_dir = original_path.parent / ".variants"
        try:
            variant_dir.rmdir()
        except OSError:
            return

    def _variant_output_suffix(self) -> str:
        if features.check("webp"):
            return ".webp"
        return ".jpg"

    def _generate_variant(
        self,
        original_path: Path,
        variant_path: Path,
        variant: ImageVariantName,
    ) -> None:
        max_edge = _VARIANT_MAX_EDGE[variant]
        if max_edge <= 0:
            raise ValueError("原图不需要派生缩略图")

        with Image.open(original_path) as opened:
            image = ImageOps.exif_transpose(opened)
            rendered = image.copy()

        if rendered.width <= 0 or rendered.height <= 0:
            raise ValueError("无效图片尺寸")

        rendered.thumbnail((max_edge, max_edge), Image.Resampling.LANCZOS)
        self._save_variant_image(rendered, variant_path)

    def _save_variant_image(self, image: Image.Image, destination: Path) -> None:
        destination.parent.mkdir(parents=True, exist_ok=True)
        temp_path = destination.parent / f".{destination.name}.{uuid4().hex}.tmp"

        if destination.suffix == ".webp":
            image.save(temp_path, format="WEBP", quality=84, method=6)
        else:
            prepared = image.convert("RGB") if image.mode not in {"RGB", "L"} else image
            prepared.save(temp_path, format="JPEG", quality=86, optimize=True, progressive=True)

        temp_path.replace(destination)

    def _guess_media_type(self, path: Path, *, fallback: str) -> str:
        return mimetypes.guess_type(path.name)[0] or fallback
