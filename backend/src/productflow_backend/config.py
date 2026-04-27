from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from typing import Any, Literal

from pydantic import Field, ValidationError, ValidationInfo, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict
from sqlalchemy import select
from sqlalchemy.exc import SQLAlchemyError

ConfigInputType = Literal["text", "password", "number", "boolean", "select", "textarea"]
IMAGE_SIZE_PATTERN = re.compile(r"^\d+x\d+$")
DEFAULT_IMAGE_GENERATION_MAX_DIMENSION = 3840
IMAGE_GENERATION_MIN_MAX_DIMENSION = 512
IMAGE_GENERATION_MAX_MAX_DIMENSION = 8192
IMAGE_GENERATION_MAX_DIMENSION = DEFAULT_IMAGE_GENERATION_MAX_DIMENSION
IMAGE_GENERATION_MAX_PIXELS = DEFAULT_IMAGE_GENERATION_MAX_DIMENSION * DEFAULT_IMAGE_GENERATION_MAX_DIMENSION
IMAGE_SIZE_CONFIG_KEYS = {"image_main_image_size", "image_promo_poster_size"}
PROMPT_CONFIG_KEYS = {
    "prompt_brief_system",
    "prompt_copy_system",
    "prompt_poster_image_template",
    "prompt_poster_image_edit_template",
    "prompt_image_chat_template",
}
BACKEND_DIR = Path(__file__).resolve().parents[2]
DEFAULT_LOG_DIR = BACKEND_DIR / "storage" / "logs"
DEFAULT_PROMPT_BRIEF_SYSTEM = (
    "你是电商商品理解助手。请根据商品名称、类目、价格和用途，"
    "输出简洁、结构化的中文 JSON。不要输出 markdown。"
)
DEFAULT_PROMPT_COPY_SYSTEM = (
    "你是淘宝电商文案助手。请输出中文 JSON，不输出 markdown，"
    "语言要口语、直接、可用于主图和促销海报。"
)
DEFAULT_PROMPT_POSTER_IMAGE_TEMPLATE = """请根据本轮用户要求与显式连接的上游上下文生成图片。
用户要求：{instruction}
输出尺寸：{size}
上游上下文：
{context_block}
{kind_requirements}
请直接生成图片，不要返回说明文字。"""
DEFAULT_PROMPT_POSTER_IMAGE_EDIT_TEMPLATE = DEFAULT_PROMPT_POSTER_IMAGE_TEMPLATE
DEFAULT_PROMPT_IMAGE_CHAT_TEMPLATE = """请根据本轮用户要求生成图片。
输出尺寸：{size}
{history_block}
本轮用户要求：{prompt}
请直接生成图片，不要返回说明文字。"""


@dataclass(frozen=True, slots=True)
class ConfigOption:
    value: str
    label: str


@dataclass(frozen=True, slots=True)
class ConfigDefinition:
    key: str
    label: str
    category: str
    input_type: ConfigInputType
    description: str = ""
    options: tuple[ConfigOption, ...] = ()
    secret: bool = False
    minimum: int | None = None
    maximum: int | None = None
    optional: bool = False


class Settings(BaseSettings):
    """应用配置：环境变量 + 数据库覆盖。

    基础设施配置（数据库 / Redis / Secret 等）仅从环境变量读取，
    业务配置（供应商 / 模型 / 上传限制）可在运行时通过 app_settings 表覆盖。
    """

    model_config = SettingsConfigDict(
        env_file=("../.env", ".env"),
        env_file_encoding="utf-8",
        extra="ignore",
    )

    app_env: str = "development"
    app_host: str = "0.0.0.0"
    app_port: int = 29280
    backend_cors_origins: str = "http://localhost:29281,http://127.0.0.1:29281"
    session_cookie_secure: bool = False

    admin_access_key: str = Field(min_length=8)
    settings_access_token: str | None = None
    session_secret: str = Field(min_length=16)

    database_url: str
    redis_url: str
    storage_root: Path = Path("./backend/storage")

    log_dir: Path = DEFAULT_LOG_DIR
    log_level: str = "INFO"
    log_max_bytes: int = 10 * 1024 * 1024
    log_backup_count: int = 5
    log_retention_days: int = 14

    text_provider_kind: str = "mock"
    text_api_key: str | None = None
    text_base_url: str | None = None
    text_brief_model: str = "gpt-4o"
    text_copy_model: str = "gpt-4o"

    image_provider_kind: str = "mock"
    image_api_key: str | None = None
    image_base_url: str | None = None
    image_generate_model: str = "gpt-5.4"
    image_tool_model: str | None = None
    image_tool_quality: str | None = None
    image_tool_output_format: str | None = None
    image_tool_output_compression: int | None = Field(default=None, ge=0, le=100)
    image_tool_background: str | None = None
    image_tool_moderation: str | None = None
    image_tool_action: str | None = None
    image_tool_input_fidelity: str | None = None
    image_tool_partial_images: int | None = Field(default=None, ge=0, le=3)
    image_tool_n: int | None = Field(default=None, ge=1, le=10)
    image_generation_max_dimension: int = Field(
        default=DEFAULT_IMAGE_GENERATION_MAX_DIMENSION,
        ge=IMAGE_GENERATION_MIN_MAX_DIMENSION,
        le=IMAGE_GENERATION_MAX_MAX_DIMENSION,
    )
    image_main_image_size: str = "1024x1024"
    image_promo_poster_size: str = "1024x1536"
    poster_generation_mode: str = "template"

    poster_font_path: Path = Path("/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc")

    prompt_brief_system: str = DEFAULT_PROMPT_BRIEF_SYSTEM
    prompt_copy_system: str = DEFAULT_PROMPT_COPY_SYSTEM
    prompt_poster_image_template: str = DEFAULT_PROMPT_POSTER_IMAGE_TEMPLATE
    prompt_poster_image_edit_template: str = DEFAULT_PROMPT_POSTER_IMAGE_EDIT_TEMPLATE
    prompt_image_chat_template: str = DEFAULT_PROMPT_IMAGE_CHAT_TEMPLATE

    upload_max_image_bytes: int = 10 * 1024 * 1024
    upload_max_reference_images: int = 6
    upload_max_pixels: int = 16_000_000
    upload_allowed_image_mime_types: str = "image/png,image/jpeg,image/webp"

    job_max_attempts: int = 3
    job_retry_delay_ms: int = 10_000
    generation_max_concurrent_tasks: int = Field(default=3, ge=1, le=20)
    deletion_enabled: bool = False

    @field_validator("image_main_image_size", "image_promo_poster_size")
    @classmethod
    def _normalize_image_generation_fallback_size(cls, value: str, info: ValidationInfo) -> str:
        max_dimension = int(info.data.get("image_generation_max_dimension") or DEFAULT_IMAGE_GENERATION_MAX_DIMENSION)
        return normalize_image_generation_size(value, max_dimension=max_dimension)

    @field_validator(
        "image_tool_model",
        "image_tool_quality",
        "image_tool_output_format",
        "image_tool_background",
        "image_tool_moderation",
        "image_tool_action",
        "image_tool_input_fidelity",
        mode="before",
    )
    @classmethod
    def _normalize_optional_image_tool_text(cls, value: Any) -> str | None:
        normalized = "" if value is None else str(value).strip()
        return normalized or None

    @field_validator("image_tool_output_compression", "image_tool_partial_images", "image_tool_n", mode="before")
    @classmethod
    def _normalize_optional_image_tool_int(cls, value: Any) -> int | None:
        if value is None:
            return None
        if isinstance(value, str) and not value.strip():
            return None
        return int(value)

    @model_validator(mode="after")
    def _validate_distinct_settings_token(self) -> Settings:
        if self.settings_access_token and self.settings_access_token.strip() == self.admin_access_key:
            raise ValueError("SETTINGS_ACCESS_TOKEN 必须与 ADMIN_ACCESS_KEY 分开设置")
        return self

    @property
    def cors_origins(self) -> list[str]:
        return [origin.strip() for origin in self.backend_cors_origins.split(",") if origin.strip()]

    @property
    def allowed_image_mime_types(self) -> set[str]:
        return {
            mime_type.strip().lower()
            for mime_type in self.upload_allowed_image_mime_types.split(",")
            if mime_type.strip()
        }


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    """Bootstrap settings loaded from env.

    Infrastructure settings such as database URL, Redis URL, session secret and
    admin key intentionally stay env-backed because the app needs them before it
    can read any database-stored configuration.
    """

    return Settings()


CONFIG_DEFINITIONS: tuple[ConfigDefinition, ...] = (
    ConfigDefinition(
        key="text_provider_kind",
        label="文案供应商",
        category="文案生成",
        input_type="select",
        options=(ConfigOption("mock", "Mock"), ConfigOption("openai", "OpenAI Responses")),
        description="控制商品理解和文案生成走 mock 还是真实 OpenAI 兼容接口。",
    ),
    ConfigDefinition(
        key="text_api_key",
        label="文案 API Key",
        category="文案生成",
        input_type="password",
        secret=True,
        description="仅在文案供应商为 OpenAI 时使用；接口不会回显已有密钥。",
    ),
    ConfigDefinition(
        key="text_base_url",
        label="文案 Base URL",
        category="文案生成",
        input_type="text",
        description="OpenAI 兼容接口地址，留空则使用 SDK 默认地址。",
    ),
    ConfigDefinition(
        key="text_brief_model",
        label="商品理解模型",
        category="文案生成",
        input_type="text",
    ),
    ConfigDefinition(
        key="text_copy_model",
        label="文案生成模型",
        category="文案生成",
        input_type="text",
    ),
    ConfigDefinition(
        key="image_provider_kind",
        label="图片供应商",
        category="图片生成",
        input_type="select",
        options=(ConfigOption("mock", "Mock"), ConfigOption("openai_responses", "OpenAI Responses")),
        description="控制连续生图和 AI 生成海报使用的图片供应商。",
    ),
    ConfigDefinition(
        key="image_api_key",
        label="图片 API Key",
        category="图片生成",
        input_type="password",
        secret=True,
        description="仅在图片供应商为 OpenAI Responses 时使用；接口不会回显已有密钥。",
    ),
    ConfigDefinition(
        key="image_base_url",
        label="图片 Base URL",
        category="图片生成",
        input_type="text",
        description="OpenAI 兼容接口地址，留空则使用 SDK 默认地址。",
    ),
    ConfigDefinition(
        key="image_generate_model",
        label="图片模型",
        category="图片生成",
        input_type="text",
    ),
    ConfigDefinition(
        key="image_tool_model",
        label="Tool 模型",
        category="图片工具参数",
        input_type="text",
        description="留空不发送；需要 provider 支持。",
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_quality",
        label="质量",
        category="图片工具参数",
        input_type="select",
        options=(
            ConfigOption("", "默认"),
            ConfigOption("auto", "Auto"),
            ConfigOption("low", "Low"),
            ConfigOption("medium", "Medium"),
            ConfigOption("high", "High"),
        ),
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_output_format",
        label="格式",
        category="图片工具参数",
        input_type="select",
        options=(
            ConfigOption("", "默认"),
            ConfigOption("png", "PNG"),
            ConfigOption("jpeg", "JPEG"),
            ConfigOption("webp", "WebP"),
        ),
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_output_compression",
        label="压缩",
        category="图片工具参数",
        input_type="number",
        description="0-100；留空不发送。",
        minimum=0,
        maximum=100,
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_background",
        label="背景",
        category="图片工具参数",
        input_type="select",
        options=(
            ConfigOption("", "默认"),
            ConfigOption("auto", "Auto"),
            ConfigOption("opaque", "Opaque"),
            ConfigOption("transparent", "Transparent"),
        ),
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_moderation",
        label="审核",
        category="图片工具参数",
        input_type="select",
        options=(ConfigOption("", "默认"), ConfigOption("auto", "Auto"), ConfigOption("low", "Low")),
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_action",
        label="Action",
        category="图片工具参数",
        input_type="select",
        options=(
            ConfigOption("", "默认"),
            ConfigOption("auto", "Auto"),
            ConfigOption("generate", "Generate"),
            ConfigOption("edit", "Edit"),
        ),
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_input_fidelity",
        label="Input fidelity",
        category="图片工具参数",
        input_type="select",
        options=(ConfigOption("", "默认"), ConfigOption("low", "Low"), ConfigOption("high", "High")),
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_partial_images",
        label="Partial",
        category="图片工具参数",
        input_type="number",
        description="0-3；留空不发送。",
        minimum=0,
        maximum=3,
        optional=True,
    ),
    ConfigDefinition(
        key="image_tool_n",
        label="Provider n",
        category="图片工具参数",
        input_type="number",
        description="高级字段；不改变连续生图候选数量语义。",
        minimum=1,
        maximum=10,
        optional=True,
    ),
    ConfigDefinition(
        key="image_generation_max_dimension",
        label="生图最大单边",
        category="图片生成",
        input_type="number",
        description="连续生图和工作流生图的最大宽/高像素；最大面积同步使用该值的平方。",
        minimum=IMAGE_GENERATION_MIN_MAX_DIMENSION,
        maximum=IMAGE_GENERATION_MAX_MAX_DIMENSION,
    ),
    ConfigDefinition(
        key="image_main_image_size",
        label="主图尺寸（兼容默认）",
        category="图片生成",
        input_type="text",
        description=(
            "高级/兼容默认值：仅当图片 provider 输入未显式传入 image_size，"
            "且生成类型为 MAIN_IMAGE 时使用。新工作流生图节点通常会传入明确尺寸，"
            "请优先使用节点里的尺寸选择器。"
        ),
    ),
    ConfigDefinition(
        key="image_promo_poster_size",
        label="促销海报尺寸（兼容默认）",
        category="图片生成",
        input_type="text",
        description=(
            "高级/兼容默认值：仅当图片 provider 输入未显式传入 image_size，"
            "且生成类型为 PROMO_POSTER 时使用。新工作流生图节点通常会传入明确尺寸，"
            "请优先使用节点里的尺寸选择器。"
        ),
    ),
    ConfigDefinition(
        key="poster_generation_mode",
        label="海报生成模式",
        category="海报与上传",
        input_type="select",
        options=(ConfigOption("template", "模板渲染"), ConfigOption("generated", "AI 生成")),
        description="模板渲染不消耗图片模型；AI 生成会调用图片供应商。",
    ),
    ConfigDefinition(
        key="poster_font_path",
        label="海报字体路径",
        category="海报与上传",
        input_type="text",
        description="模板海报和 mock 图片中用于中文文字渲染的字体文件。",
    ),
    ConfigDefinition(
        key="prompt_brief_system",
        label="商品理解系统提示词",
        category="提示词",
        input_type="textarea",
        description="用于商品资料理解，要求模型输出 CreativeBrief JSON。",
    ),
    ConfigDefinition(
        key="prompt_copy_system",
        label="文案生成系统提示词",
        category="提示词",
        input_type="textarea",
        description="用于主图/海报文案生成，要求模型输出 Copy JSON。",
    ),
    ConfigDefinition(
        key="prompt_poster_image_template",
        label="海报生图提示词模板",
        category="提示词",
        input_type="textarea",
        description=(
            "用于工作台 AI 生图。可用占位符：instruction、size、context_block、kind、kind_label、kind_requirements。"
        ),
    ),
    ConfigDefinition(
        key="prompt_poster_image_edit_template",
        label="图片改图提示词模板",
        category="提示词",
        input_type="textarea",
        description=(
            "用于工作台参考图/生成图继续生图。可用占位符：instruction、size、context_block、kind、kind_label、kind_requirements。"
        ),
    ),
    ConfigDefinition(
        key="prompt_image_chat_template",
        label="连续生图提示词模板",
        category="提示词",
        input_type="textarea",
        description="用于连续生图对话。可用占位符：prompt、size、history_block。",
    ),
    ConfigDefinition(
        key="upload_max_image_bytes",
        label="单图最大字节数",
        category="海报与上传",
        input_type="number",
        minimum=1,
    ),
    ConfigDefinition(
        key="upload_max_reference_images",
        label="最多参考图数量",
        category="海报与上传",
        input_type="number",
        minimum=0,
    ),
    ConfigDefinition(
        key="upload_max_pixels",
        label="最大像素数",
        category="海报与上传",
        input_type="number",
        minimum=1,
    ),
    ConfigDefinition(
        key="upload_allowed_image_mime_types",
        label="允许图片 MIME",
        category="海报与上传",
        input_type="textarea",
        description="逗号分隔，例如 image/png,image/jpeg,image/webp。",
    ),
    ConfigDefinition(
        key="job_max_attempts",
        label="任务最大尝试次数",
        category="任务重试",
        input_type="number",
        minimum=1,
    ),
    ConfigDefinition(
        key="job_retry_delay_ms",
        label="任务重试延迟毫秒",
        category="任务重试",
        input_type="number",
        minimum=0,
    ),
    ConfigDefinition(
        key="generation_max_concurrent_tasks",
        label="全局生成并发上限",
        category="任务重试",
        input_type="number",
        description="全局资源保护阈值；文案、海报、工作流和连续生图达到上限时会提示稍后重试。",
        minimum=1,
        maximum=20,
    ),
    ConfigDefinition(
        key="deletion_enabled",
        label="启用业务删除",
        category="安全与运维",
        input_type="boolean",
        description="默认关闭，用于体验站禁止整条商品和连续生图会话被删除，保留溯源证据。",
    ),
)

CONFIG_DEFINITION_BY_KEY: dict[str, ConfigDefinition] = {
    definition.key: definition for definition in CONFIG_DEFINITIONS
}
RUNTIME_CONFIG_KEYS: set[str] = set(CONFIG_DEFINITION_BY_KEY)


def normalize_image_size(value: Any, *, label: str = "图片尺寸") -> str:
    """校验并标准化图片尺寸格式 宽x高。"""
    normalized = "" if value is None else str(value).strip().lower()
    if not IMAGE_SIZE_PATTERN.fullmatch(normalized):
        raise ValueError(f"{label} 必须使用 宽x高 格式，例如 1024x1024")
    width, height = (int(part) for part in normalized.split("x", maxsplit=1))
    if width <= 0 or height <= 0:
        raise ValueError(f"{label} 宽高必须大于 0")
    return normalized


def _runtime_image_generation_max_dimension() -> int:
    return int(get_runtime_settings().image_generation_max_dimension)


def normalize_image_generation_size(
    value: Any,
    *,
    label: str = "图片尺寸",
    max_dimension: int | None = None,
) -> str:
    """校验并校准生图尺寸，包含格式、正数和运行时安全边界。"""
    normalized = normalize_image_size(value, label=label)
    resolved_max_dimension = int(max_dimension or _runtime_image_generation_max_dimension())
    if (
        resolved_max_dimension < IMAGE_GENERATION_MIN_MAX_DIMENSION
        or resolved_max_dimension > IMAGE_GENERATION_MAX_MAX_DIMENSION
    ):
        raise ValueError(
            f"生图最大单边必须在 {IMAGE_GENERATION_MIN_MAX_DIMENSION}-{IMAGE_GENERATION_MAX_MAX_DIMENSION} 之间"
        )
    max_pixels = resolved_max_dimension * resolved_max_dimension
    width, height = (int(part) for part in normalized.split("x", maxsplit=1))
    scale = min(1.0, resolved_max_dimension / width, resolved_max_dimension / height)
    resolved_width = min(resolved_max_dimension, max(1, round(width * scale)))
    resolved_height = min(resolved_max_dimension, max(1, round(height * scale)))
    if resolved_width * resolved_height > max_pixels:
        pixel_scale = (max_pixels / (resolved_width * resolved_height)) ** 0.5
        resolved_width = max(1, int(resolved_width * pixel_scale))
        resolved_height = max(1, int(resolved_height * pixel_scale))
    return f"{resolved_width}x{resolved_height}"


def normalize_config_value(key: str, value: Any) -> str:
    definition = CONFIG_DEFINITION_BY_KEY.get(key)
    if definition is None:
        raise ValueError(f"未知配置项: {key}")

    if definition.input_type == "boolean":
        if isinstance(value, bool):
            return "true" if value else "false"
        normalized_bool = str(value).strip().lower()
        if normalized_bool in {"1", "true", "yes", "on"}:
            return "true"
        if normalized_bool in {"0", "false", "no", "off"}:
            return "false"
        raise ValueError(f"{definition.label} 必须是布尔值")

    if definition.input_type == "number":
        if definition.optional and (value is None or str(value).strip() == ""):
            return ""
        try:
            normalized_int = int(value)
        except (TypeError, ValueError) as exc:
            raise ValueError(f"{definition.label} 必须是整数") from exc
        if definition.minimum is not None and normalized_int < definition.minimum:
            raise ValueError(f"{definition.label} 不能小于 {definition.minimum}")
        if definition.maximum is not None and normalized_int > definition.maximum:
            raise ValueError(f"{definition.label} 不能大于 {definition.maximum}")
        return str(normalized_int)

    if key in IMAGE_SIZE_CONFIG_KEYS:
        return normalize_image_generation_size(value, label=definition.label)
    normalized = "" if value is None else str(value).strip()
    if key in PROMPT_CONFIG_KEYS and not normalized:
        raise ValueError(f"{definition.label} 不能为空；如需回到默认值请使用恢复默认")
    if definition.input_type == "select":
        allowed_values = {option.value for option in definition.options}
        if normalized not in allowed_values:
            allowed_text = ", ".join(sorted(allowed_values))
            raise ValueError(f"{definition.label} 必须是以下之一: {allowed_text}")
    return normalized


def normalize_config_values(values: Mapping[str, Any]) -> dict[str, str]:
    return {key: normalize_config_value(key, value) for key, value in values.items()}


def build_settings_with_overrides(overrides: Mapping[str, str]) -> Settings:
    try:
        return Settings(**dict(overrides))
    except ValidationError as exc:
        first_error = exc.errors()[0] if exc.errors() else {}
        field = ".".join(str(part) for part in first_error.get("loc", []))
        message = first_error.get("msg") or str(exc)
        raise ValueError(f"配置校验失败 {field}: {message}") from exc


def _load_database_config_overrides() -> dict[str, str]:
    try:
        from productflow_backend.infrastructure.db.models import AppSetting
        from productflow_backend.infrastructure.db.session import get_session_factory

        session = get_session_factory()()
        try:
            rows = session.scalars(select(AppSetting).where(AppSetting.key.in_(RUNTIME_CONFIG_KEYS))).all()
            return {row.key: row.value for row in rows}
        finally:
            session.close()
    except Exception as exc:  # noqa: BLE001
        if exc.__class__.__name__ in {"OperationalError", "ProgrammingError"}:
            return {}
        if isinstance(exc, SQLAlchemyError):
            return {}
        raise


def get_runtime_settings() -> Settings:
    """Settings with database overrides applied.

    If a key does not exist in the database, env/default Settings remains the
    fallback. Missing app_settings table is tolerated so fresh databases can
    still start before migrations have run.
    """

    overrides = _load_database_config_overrides()
    if not overrides:
        return get_settings()
    return build_settings_with_overrides(overrides)
