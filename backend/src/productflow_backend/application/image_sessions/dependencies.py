from __future__ import annotations

from collections.abc import Callable

from productflow_backend.infrastructure.image.base import ImageChatProvider
from productflow_backend.infrastructure.image.factory import get_image_chat_provider

ImageChatProviderFactory = Callable[[], ImageChatProvider]
# Keep the existing application parameter type available for injected callers.
ImageSessionChatService = ImageChatProvider
ImageSessionChatServiceFactory = ImageChatProviderFactory


def default_image_session_chat_service_factory() -> ImageChatProvider:
    """Build the production image-session adapter."""

    return get_image_chat_provider()


__all__ = [
    "ImageChatProvider",
    "ImageChatProviderFactory",
    "ImageSessionChatService",
    "ImageSessionChatServiceFactory",
    "default_image_session_chat_service_factory",
]
