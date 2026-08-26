from __future__ import annotations

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from productflow_backend.application.delivery_renditions.contracts import normalize_delivery_spec
from productflow_backend.application.delivery_renditions.presets import (
    DELIVERY_PRESET_DISCLAIMER,
    DELIVERY_PRESET_REVIEWED_AT,
    DELIVERY_PRESET_SOURCE,
    DELIVERY_PRESETS,
    get_delivery_preset,
    list_delivery_presets,
)
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.presentation.deps import require_admin
from productflow_backend.presentation.errors import register_exception_handlers
from productflow_backend.presentation.routes.delivery_presets import router


def _catalog_app() -> FastAPI:
    app = FastAPI()
    register_exception_handlers(app)
    app.dependency_overrides[require_admin] = lambda: None
    app.include_router(router)
    return app


def test_delivery_preset_catalog_has_stable_order_and_neutral_specs() -> None:
    assert [preset.key for preset in list_delivery_presets()] == [
        "taobao_tmall_hero",
        "jd_hero",
        "amazon_hero",
        "detail_portrait",
        "scene_landscape",
    ]

    expected = {
        "taobao_tmall_hero": ("3:4", "hero", 1200, 1600),
        "jd_hero": ("1:1", "hero", 1200, 1200),
        "amazon_hero": ("1:1", "hero", 1200, 1200),
        "detail_portrait": ("3:4", "detail", 1200, 1600),
        "scene_landscape": ("4:3", "scene", 1600, 1200),
    }
    for preset in DELIVERY_PRESETS:
        aspect_ratio, image_type, width, height = expected[preset.key]
        assert (preset.aspect_ratio, preset.applicable_image_type) == (aspect_ratio, image_type)
        assert (preset.spec.width, preset.spec.height) == (width, height)
        assert preset.spec.format == "png"
        assert preset.spec.fit == "contain"
        assert preset.spec.background_color is None
        assert preset.spec.crop_anchor is None
        assert preset.spec.max_byte_size is None
        assert preset.reviewed_at == DELIVERY_PRESET_REVIEWED_AT
        assert preset.source == DELIVERY_PRESET_SOURCE
        assert preset.disclaimer == DELIVERY_PRESET_DISCLAIMER
        assert normalize_delivery_spec(preset.spec).spec == preset.spec
        assert set(preset.spec.model_dump()) == {
            "width",
            "height",
            "format",
            "max_byte_size",
            "fit",
            "background_color",
            "crop_anchor",
        }
        assert "generation_spec" not in preset.spec.model_dump()
        assert "provider" not in preset.spec.model_dump()


def test_delivery_preset_catalog_is_immutable() -> None:
    with pytest.raises(TypeError):
        DELIVERY_PRESETS[0] = DELIVERY_PRESETS[1]  # type: ignore[index]
    with pytest.raises(AttributeError):
        DELIVERY_PRESETS[0].key = "changed"  # type: ignore[misc]


def test_delivery_preset_lookup_rejects_unknown_key() -> None:
    with pytest.raises(NotFoundError):
        get_delivery_preset("unknown")


def test_delivery_preset_list_api_returns_exact_catalog_and_custom_support() -> None:
    client = TestClient(_catalog_app())
    response = client.get("/api/v3/delivery-presets")

    assert response.status_code == 200
    assert response.json() == {
        "supports_custom": True,
        "items": [
            {
                "key": "taobao_tmall_hero",
                "title": "淘宝/天猫首屏",
                "aspect_ratio": "3:4",
                "applicable_image_type": "hero",
                "reviewed_at": "2026-08-24",
                "source": "docs/specs/productflow-studio-requirements.md §18",
                "disclaimer": DELIVERY_PRESET_DISCLAIMER,
                "delivery_spec": {
                    "width": 1200,
                    "height": 1600,
                    "format": "png",
                    "max_byte_size": None,
                    "fit": "contain",
                    "background_color": None,
                    "crop_anchor": None,
                },
            },
            {
                "key": "jd_hero",
                "title": "京东主图",
                "aspect_ratio": "1:1",
                "applicable_image_type": "hero",
                "reviewed_at": "2026-08-24",
                "source": "docs/specs/productflow-studio-requirements.md §18",
                "disclaimer": DELIVERY_PRESET_DISCLAIMER,
                "delivery_spec": {
                    "width": 1200,
                    "height": 1200,
                    "format": "png",
                    "max_byte_size": None,
                    "fit": "contain",
                    "background_color": None,
                    "crop_anchor": None,
                },
            },
            {
                "key": "amazon_hero",
                "title": "Amazon 主图",
                "aspect_ratio": "1:1",
                "applicable_image_type": "hero",
                "reviewed_at": "2026-08-24",
                "source": "docs/specs/productflow-studio-requirements.md §18",
                "disclaimer": DELIVERY_PRESET_DISCLAIMER,
                "delivery_spec": {
                    "width": 1200,
                    "height": 1200,
                    "format": "png",
                    "max_byte_size": None,
                    "fit": "contain",
                    "background_color": None,
                    "crop_anchor": None,
                },
            },
            {
                "key": "detail_portrait",
                "title": "详情竖图",
                "aspect_ratio": "3:4",
                "applicable_image_type": "detail",
                "reviewed_at": "2026-08-24",
                "source": "docs/specs/productflow-studio-requirements.md §18",
                "disclaimer": DELIVERY_PRESET_DISCLAIMER,
                "delivery_spec": {
                    "width": 1200,
                    "height": 1600,
                    "format": "png",
                    "max_byte_size": None,
                    "fit": "contain",
                    "background_color": None,
                    "crop_anchor": None,
                },
            },
            {
                "key": "scene_landscape",
                "title": "场景横图",
                "aspect_ratio": "4:3",
                "applicable_image_type": "scene",
                "reviewed_at": "2026-08-24",
                "source": "docs/specs/productflow-studio-requirements.md §18",
                "disclaimer": DELIVERY_PRESET_DISCLAIMER,
                "delivery_spec": {
                    "width": 1600,
                    "height": 1200,
                    "format": "png",
                    "max_byte_size": None,
                    "fit": "contain",
                    "background_color": None,
                    "crop_anchor": None,
                },
            },
        ],
    }
