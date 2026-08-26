"""商品图片类型的无数据库目录与生成规则。"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

AgentProductImageTypeKey = Literal[
    "hero",
    "selling_point",
    "scene",
    "detail",
    "sku",
    "dimensions",
    "specifications",
    "after_sales",
    "brand_story",
    "precautions",
    "certification",
    "faq",
    "factory",
    "packaging",
    "shipping",
]


@dataclass(frozen=True, slots=True)
class AgentProductImageTypeOption:
    key: AgentProductImageTypeKey
    title: str
    description: str
    order: int


AGENT_PRODUCT_IMAGE_TYPE_CATALOG = (
    AgentProductImageTypeOption("hero", "首屏海报图", "搜索列表首图，商品够大能认", 0),
    AgentProductImageTypeOption("selling_point", "核心卖点图", "详情卖点图，层次清楚，不要空棚贴字", 1),
    AgentProductImageTypeOption("scene", "场景展示图", "使用场景里拍，商品是主角", 2),
    AgentProductImageTypeOption("detail", "细节展示图", "材质和工艺特写", 3),
    AgentProductImageTypeOption("sku", "SKU 展示图", "白底规格对照，方便选款", 4),
    AgentProductImageTypeOption("dimensions", "尺寸图", "尺寸线清晰的信息图", 5),
    AgentProductImageTypeOption("specifications", "规格参数图", "参数对照信息图", 6),
    AgentProductImageTypeOption("after_sales", "售后保障图", "质保退换说明图", 7),
    AgentProductImageTypeOption("brand_story", "品牌故事图", "品牌故事海报", 8),
    AgentProductImageTypeOption("precautions", "注意事项图", "使用保养说明图", 9),
    AgentProductImageTypeOption("certification", "资质认证图", "只用已提供的资质", 10),
    AgentProductImageTypeOption("faq", "常见问题图", "问答说明图", 11),
    AgentProductImageTypeOption("factory", "工厂实力图", "只用已提供的工厂画面", 12),
    AgentProductImageTypeOption("packaging", "包装展示图", "包装全貌", 13),
    AgentProductImageTypeOption("shipping", "发货物流图", "发货物流说明图", 14),
)

LISTING_LOOK_RULE = (
    "做成能点击的商业套图：商品是主角，层次清楚，卖点好读。"
    "不要极简大留白、浅灰空棚、杂志静物；也不要爆炸贴、满屏色块、牛皮癣标签。"
)
LISTING_LOOK_CONTEXT: dict[str, object] = {
    "rule": LISTING_LOOK_RULE,
    "product_share_percent": "55-75",
    "benefit_count": "2-4",
    "source_note_is_product_fact": True,
    "ignore_as_art_direction": ["极简", "浅灰", "静物", "干净", "留白", "高级", "苹果风"],
    "do_not_invert_into": ["爆炸贴", "满屏色块", "牛皮癣标签", "过饱和撞色"],
}
IMAGE_TYPE_GENERATION_JOBS: dict[str, str] = {
    "hero": (
        "搜索列表首图。商品约占画面 55%–75%，一眼能认出货，有类别合适的底和光影。"
        "最多一句超短主利益点。不要浅灰大海把商品挤到角落，也不要贴满角标。"
    ),
    "selling_point": (
        "详情卖点图，不是照片加字幕。抠出商品重新构图。"
        "一个主标题加 2 到 4 条对齐好读的短利益点，色块克制。商品仍是主角。"
        "不要原图贴字，不要大面积留白，也不要爆炸贴墙。"
    ),
    "scene": (
        "使用场景。把商品放进会用到的环境，环境为人服务、商品清晰可辨。"
        "不要空棚静物，也不要把场景堆满杂物，更不要编造资料里没有的生活道具品牌。"
    ),
    "detail": "材质或工艺特写。镜头贴近关键结构，光线强调质感，不要整件商品的远景棚拍。",
    "sku": "规格/颜色/款式对照图。纯色或白底，商品摆正、边缘干净，方便选款，不要装饰性大标题。",
    "dimensions": (
        "尺寸标注信息图。商品在画面中，尺寸线清楚。数字只能来自商品资料；没有数据就画结构关系，不要编造毫米数。"
    ),
    "specifications": "规格参数信息图。用短标签和对照模块呈现资料里已有的参数，不要编造参数。",
    "after_sales": "售后保障说明图。只写资料里有的质保、退换、运费政策，排版清楚，不要编造承诺。",
    "brand_story": "品牌故事海报。只使用资料或参考图里出现的品牌信息，做成可上详情的设计稿，不要空洞鸡汤。",
    "precautions": "使用与保养说明图。条目短、可读，内容来自资料，不要恐吓式极限词。",
    "certification": "资质认证图。只能使用用户提供的证书或标志照片，没有素材就留缺口，不要手绘公章。",
    "faq": "常见问题说明图。问句短、答句短，内容来自资料，不要编造售后话术。",
    "factory": "工厂实力图。只能使用用户提供的产线或厂房照片，没有素材就留缺口，不要生成假车间。",
    "packaging": "包装展示图。看清包装结构与内容物，商品可辨认，不要只拍一个模糊纸箱。",
    "shipping": "发货物流说明图。只写资料里有的发货与时效信息，排版清楚，不要编造快递品牌。",
}

AGENT_PRODUCT_IMAGE_TYPE_KEYS = frozenset(option.key for option in AGENT_PRODUCT_IMAGE_TYPE_CATALOG)
_IMAGE_TYPE_BY_KEY = {option.key: option for option in AGENT_PRODUCT_IMAGE_TYPE_CATALOG}
PHOTOGRAPHY_IMAGE_TYPE_KEYS = frozenset({"hero", "scene", "detail", "sku", "packaging"})
INFOGRAPHIC_IMAGE_TYPE_KEYS = frozenset(
    {
        "selling_point",
        "dimensions",
        "specifications",
        "after_sales",
        "precautions",
        "faq",
        "shipping",
        "brand_story",
    }
)
EVIDENCE_IMAGE_TYPE_KEYS = frozenset({"certification", "factory"})


def image_type_family(key: str) -> str:
    if key in EVIDENCE_IMAGE_TYPE_KEYS:
        return "evidence"
    if key in INFOGRAPHIC_IMAGE_TYPE_KEYS:
        return "infographic"
    return "photography"


def image_type_generation_job(key: str) -> str:
    option = _IMAGE_TYPE_BY_KEY.get(key)
    return IMAGE_TYPE_GENERATION_JOBS.get(key) or (option.description if option else "")


def image_type_prompt_goal(key: str) -> str:
    option = _IMAGE_TYPE_BY_KEY.get(key)
    title = option.title if option else key
    job = image_type_generation_job(key)
    return f"{title}：{job}" if job else title


def agent_product_image_type_option(key: str) -> AgentProductImageTypeOption | None:
    return _IMAGE_TYPE_BY_KEY.get(key)


__all__ = [
    "AGENT_PRODUCT_IMAGE_TYPE_CATALOG",
    "AGENT_PRODUCT_IMAGE_TYPE_KEYS",
    "AgentProductImageTypeKey",
    "AgentProductImageTypeOption",
    "EVIDENCE_IMAGE_TYPE_KEYS",
    "IMAGE_TYPE_GENERATION_JOBS",
    "INFOGRAPHIC_IMAGE_TYPE_KEYS",
    "LISTING_LOOK_CONTEXT",
    "LISTING_LOOK_RULE",
    "PHOTOGRAPHY_IMAGE_TYPE_KEYS",
    "agent_product_image_type_option",
    "image_type_family",
    "image_type_generation_job",
    "image_type_prompt_goal",
]
