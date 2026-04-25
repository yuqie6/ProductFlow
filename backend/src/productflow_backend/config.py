from __future__ import annotations

import re
from collections.abc import Mapping
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from typing import Any, Literal

from pydantic import Field, ValidationError
from pydantic_settings import BaseSettings, SettingsConfigDict
from sqlalchemy import select
from sqlalchemy.exc import SQLAlchemyError

ConfigInputType = Literal["text", "password", "number", "boolean", "select", "textarea"]
IMAGE_SIZE_PATTERN = re.compile(r"^\d+x\d+$")
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
DEFAULT_PROMPT_POSTER_IMAGE_TEMPLATE = """你是中文电商海报生成助手。
请使用 Responses API 的 image_generation 工具生成图片，并优先继承输入参考图里的商品主体。
不要出现乱码、无关品牌、水印或大段不可读文字。
商品名：{product_name}
类目：{category}
价格：{price}
商品描述/补充说明：{source_note}
本轮图片要求：{instruction}
主标题：{poster_headline}
短标题：{title}
卖点：{selling_points}
CTA：{cta}
尺寸：{size}
{kind_requirements}"""
DEFAULT_PROMPT_POSTER_IMAGE_EDIT_TEMPLATE = """你是中文商品图片改图助手。
请使用 Responses API 的 image_generation 工具生成图片，并优先继承输入参考图里的商品主体、构图和材质。
这是基于已有图片继续改图/延展的任务，不要求商品文案字段，也不要主动添加主标题、卖点、CTA 或价格标签。
不要出现乱码、无关品牌、水印或大段不可读文字。
商品名：{product_name}
类目：{category}
价格：{price}
商品描述/补充说明：{source_note}
本轮改图要求：{instruction}
尺寸：{size}
{kind_requirements}"""
DEFAULT_PROMPT_IMAGE_CHAT_TEMPLATE = """你是一个中文图片生成助手。
当前任务是同一创作对话中的连续生图，请继承已经确定的主体、风格、构图与材质线索。
如果本轮用户明确要求改动，就在保留连续性的前提下做调整。
默认不要在图片中添加可读大段文字、UI 面板、水印或拼贴。
输出尺寸：{size}。
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
    image_main_image_size: str = "1024x1024"
    image_promo_poster_size: str = "1024x1536"
    image_allowed_sizes: str = "1024x1024,1024x1536,1536x1024"

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

    @property
    def cors_origins(self) -> list[str]:
        return [origin.strip() for origin in self.backend_cors_origins.split(",") if origin.strip()]

    @property
    def allowed_image_sizes(self) -> set[str]:
        return set(normalize_image_size_list(self.image_allowed_sizes, label="允许生图尺寸"))

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
        key="image_main_image_size",
        label="主图尺寸",
        category="图片生成",
        input_type="text",
        description="例如 1024x1024；需要同时包含在允许尺寸列表中。",
    ),
    ConfigDefinition(
        key="image_promo_poster_size",
        label="促销海报尺寸",
        category="图片生成",
        input_type="text",
        description="例如 1024x1536；需要同时包含在允许尺寸列表中。",
    ),
    ConfigDefinition(
        key="image_allowed_sizes",
        label="允许生图尺寸",
        category="图片生成",
        input_type="textarea",
        description="逗号分隔，例如 1024x1024,1024x1536,1536x1024。",
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
            "用于有文案输入的 AI 主图/海报生成。可用占位符：product_name、category、price、source_note、instruction、"
            "title、selling_points、poster_headline、cta、size、kind、kind_label、kind_requirements。"
        ),
    ),
    ConfigDefinition(
        key="prompt_poster_image_edit_template",
        label="图片改图提示词模板",
        category="提示词",
        input_type="textarea",
        description=(
            "用于无文案输入的参考图/生成图改图。可用占位符：product_name、category、price、source_note、"
            "instruction、size、kind、kind_label、kind_requirements。"
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


def normalize_image_size_list(value: Any, *, label: str = "允许生图尺寸") -> tuple[str, ...]:
    normalized_sizes: list[str] = []
    seen: set[str] = set()
    for raw_size in str(value or "").split(","):
        raw_size = raw_size.strip()
        if not raw_size:
            continue
        normalized_size = normalize_image_size(raw_size, label=label)
        if normalized_size not in seen:
            normalized_sizes.append(normalized_size)
            seen.add(normalized_size)
    return tuple(normalized_sizes)


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
        return normalize_image_size(value, label=definition.label)
    if key == "image_allowed_sizes":
        return ",".join(normalize_image_size_list(value, label=definition.label))

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
