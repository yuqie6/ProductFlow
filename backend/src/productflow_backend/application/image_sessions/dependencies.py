from __future__ import annotations

from collections.abc import Callable

from productflow_backend.infrastructure.image.base import ImageChatProvider
from productflow_backend.infrastructure.image.factory import get_image_chat_provider

ImageChatProviderFactory = Callable[[], ImageChatProvider]


def default_image_session_chat_service_factory() -> ImageChatProvider:
    """构建生产环境的 image-session 适配器。"""

    return get_image_chat_provider()


__all__ = [
    "ImageChatProvider",
    "ImageChatProviderFactory",
    "default_image_session_chat_service_factory",
]
