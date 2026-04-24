from __future__ import annotations

from abc import ABC, abstractmethod

from productflow_backend.application.contracts import (
    CopyPayload,
    CreativeBriefPayload,
    ProductInput,
    ReferenceImageInput,
)


class TextProvider(ABC):
    """文本生成器抽象接口：商品理解(brief) + 文案生成(copy)。"""

    provider_name: str
    prompt_version: str = "v1"

    @abstractmethod
    def generate_brief(self, product: ProductInput) -> tuple[CreativeBriefPayload, str]:
        raise NotImplementedError

    @abstractmethod
    def generate_copy(
        self,
        product: ProductInput,
        brief: CreativeBriefPayload,
        instruction: str | None = None,
        reference_images: list[ReferenceImageInput] | None = None,
    ) -> tuple[CopyPayload, str]:
        raise NotImplementedError
