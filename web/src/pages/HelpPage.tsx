import {
  ArrowRight,
  BookOpen,
  Box,
  ChevronRight,
  CircleHelp,
  GalleryHorizontalEnd,
  GitBranch,
  Image,
  Layers3,
  Search,
  Settings,
  Sparkles,
  TerminalSquare,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { SelectField } from "../components/SelectField";
import { TopNav } from "../components/TopNav";
import type { Locale } from "../lib/i18n";
import { useI18n } from "../lib/preferences";

type SectionBlock =
  | {
      type: "paragraph";
      text: string;
    }
  | {
      type: "list";
      items: string[];
    }
  | {
      type: "steps";
      items: string[];
    }
  | {
      type: "table";
      headers: [string, string];
      rows: [string, string][];
    }
  | {
      type: "code";
      text: string;
    }
  | {
      type: "callout";
      title: string;
      text: string;
    };

interface DocSection {
  id: string;
  title: string;
  blocks: SectionBlock[];
}

interface DocPage {
  slug: string;
  title: string;
  description: string;
  category: string;
  icon: LucideIcon;
  sections: DocSection[];
}

interface NavGroup {
  title: string;
  pages: string[];
}

interface SearchResult {
  page: DocPage;
  matchedSectionTitle: string | null;
  preview: string;
  score: number;
}

const DOC_PAGES: DocPage[] = [
  {
    slug: "overview",
    title: "ProductFlow 文档概览",
    description: "ProductFlow 是单管理员自托管的商品素材工作台，用于把商品资料、参考图、文案和图片生成流程组织在一个可追踪的工作台中。",
    category: "入门",
    icon: BookOpen,
    sections: [
      {
        id: "what-is-productflow",
        title: "ProductFlow 是什么",
        blocks: [
          {
            type: "paragraph",
            text: "ProductFlow 面向单人商家、小团队运营者和希望自托管 AI 素材链路的开发者。它把商品信息、图片素材、文案生成、图片生成、运行状态和画廊收藏放在同一个私有工作台中。",
          },
          {
            type: "paragraph",
            text: "当前版本不是公开注册平台，也不是多租户 SaaS。部署者自己管理数据库、Redis、存储目录和模型密钥。",
          },
        ],
      },
      {
        id: "main-surfaces",
        title: "主要页面",
        blocks: [
          {
            type: "table",
            headers: ["页面", "用途"],
            rows: [
              ["商品/工作台", "创建商品，进入商品详情工作台，组织节点和运行工作流。"],
              ["文/图生图", "围绕图片结果连续生成、改图、比较候选，并回写商品。"],
              ["画廊", "集中保存满意的生成图，保留来源、提示词、尺寸、模型和下载入口。"],
              ["配置", "管理 provider、模型、尺寸、提示词模板、上传限制和密钥。"],
              ["帮助", "查看当前产品内操作文档。"],
            ],
          },
        ],
      },
      {
        id: "recommended-path",
        title: "推荐阅读路径",
        blocks: [
          {
            type: "steps",
            items: [
              "先阅读“快速开始”，完成一次从商品主图到生成图片的流程。",
              "再阅读“商品工作台”“画布和节点”“模板”和“生成文案和图片”，理解画布工作台。",
              "需要收藏和复查生成图时阅读“画廊”。",
              "需要连续改图时阅读“文/图生图概览”“基图和参考图”“生成设置”和“任务与结果”。",
              "部署、配置或排障时阅读“配置概览”“模型供应商”“图片工具参数”“提示词模板”和“故障排查”。",
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "quickstart",
    title: "快速开始",
    description: "用最短路径创建一个商品，选择初始画布模板，并生成第一张商品图片。",
    category: "入门",
    icon: TerminalSquare,
    sections: [
      {
        id: "before-you-start",
        title: "开始前",
        blocks: [
          {
            type: "list",
            items: [
              "后端 API、worker、PostgreSQL 和 Redis 应处于可用状态。",
              "如果开启登录门禁，先使用管理员密钥登录。",
              "准备一张清楚的商品主图，推荐使用 JPG、PNG 或 WebP。",
            ],
          },
        ],
      },
      {
        id: "create-product",
        title: "创建商品",
        blocks: [
          {
            type: "steps",
            items: [
              "打开顶部导航中的“商品/工作台”。",
              "点击“新建商品”。",
              "上传商品主图。",
              "填写商品名称。",
              "选择画布模板。首次使用推荐选择“商品主图”。",
              "点击“创建并继续”。",
            ],
          },
          {
            type: "callout",
            title: "模板可继续复用",
            text: "创建商品后，商品工作台右侧“模板”面板仍可继续添加同一套场景模板；模板中的商品资料会自动连接到当前画布的商品节点。",
          },
        ],
      },
      {
        id: "first-run",
        title: "生成第一张图片",
        blocks: [
          {
            type: "steps",
            items: [
              "进入商品工作台后，点击商品节点，在右侧“详情”中补充类目、价格和商品说明。",
              "选中文案节点，填写生成要求，运行当前节点。",
              "检查文案输出。需要时直接编辑文案结果。",
              "选中生图节点，确认它连接到至少一个下游参考图节点。",
              "填写图片要求，运行当前节点或运行工作流。",
              "在下游参考图节点或右侧“图库”面板查看并下载结果。",
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "workbench",
    title: "商品工作台",
    description: "商品工作台是 ProductFlow 的核心操作界面。桌面端中间是画布、右侧是检查器和辅助面板；移动端保留画布为主界面。",
    category: "画布工作台",
    icon: GitBranch,
    sections: [
      {
        id: "layout",
        title: "界面结构",
        blocks: [
          {
            type: "table",
            headers: ["区域", "说明"],
            rows: [
              ["画布", "展示商品、参考图、文案和生图节点，以及节点之间的连接线。"],
              ["详情", "编辑选中节点的配置和输出。"],
              ["日志", "查看工作流运行记录、失败原因和可重试运行。"],
              ["图库", "查看商品素材、生成图和可填充到参考图节点的图片。"],
              ["模板", "插入内置场景模板，或管理用户保存的模板。"],
              ["移动端底部工具栏", "切换浏览、编辑和选择模式，并打开运行、单节点、模板、详情、日志和图库入口。"],
              ["移动端底部面板", "在手机上承载桌面右侧面板的单节点、模板、详情、日志和图库内容。"],
            ],
          },
        ],
      },
      {
        id: "mobile-workbench",
        title: "移动端工作台",
        blocks: [
          {
            type: "paragraph",
            text: "手机上进入商品详情后，画布仍是主操作区。底部模式切换提供“浏览”“编辑”“选择”：浏览用于拖动画布、点选节点和双指缩放；编辑用于拖动节点和创建连接；选择用于点按节点加入或移出多选。",
          },
          {
            type: "paragraph",
            text: "底部工具栏还提供运行工作流、单节点、模板、详情、日志和图库入口。点开这些入口时，内容从底部面板展开，关闭后回到画布。",
          },
        ],
      },
      {
        id: "node-types",
        title: "节点类型",
        blocks: [
          {
            type: "table",
            headers: ["节点", "说明"],
            rows: [
              ["商品", "商品资料入口，保存名称、类目、价格和说明。"],
              ["参考图", "单张图片槽位，可手动上传，也可由上游生图节点填充。"],
              ["文案", "生成并编辑结构化文案：可是一段自由正文，也可以是文案块或布局分区；后续生图会读取结构化文案上下文。"],
              ["生图", "触发图片生成。生成结果写入下游参考图节点，不在生图节点自身下载。"],
            ],
          },
        ],
      },
      {
        id: "run-model",
        title: "运行模型",
        blocks: [
          {
            type: "paragraph",
            text: "节点连接方向决定下游运行时读取哪些上下文。把 A 连到 B，表示 B 运行时可以参考 A。",
          },
          {
            type: "callout",
            title: "运行前先保存",
            text: "工作流运行读取的是已保存内容。若“详情”中还有草稿，运行按钮会先尝试保存；如果保存失败，运行不会继续提交。",
          },
        ],
      },
    ],
  },
  {
    slug: "canvas-nodes",
    title: "画布和节点",
    description: "画布支持缩放、平移、节点拖拽、连接、框选和多选操作。",
    category: "画布工作台",
    icon: Box,
    sections: [
      {
        id: "canvas-controls",
        title: "画布操作",
        blocks: [
          {
            type: "table",
            headers: ["操作", "说明"],
            rows: [
              ["桌面缩放", "鼠标滚轮缩放画布；右下角的 - / 百分比 / + 可以缩小、重置和放大。"],
              ["桌面平移", "不按任何快捷键时，在画布空白区域按住左键拖动。拖节点、点击按钮、上传和拖连线不会触发平移。"],
              ["桌面移动单个节点", "直接按住节点主体或标题区域拖动，松手后位置保存。"],
              ["桌面移动多个节点", "多选节点后，按住其中任意一个已选节点拖动，整组选中节点会一起移动。"],
              ["桌面创建连线", "按住节点右侧输出连接点，拖到目标节点左侧输入连接点后松开。"],
              ["选择单个节点", "直接点击节点主体或节点左侧输入连接点。详情面板会显示该节点。"],
              ["桌面追加/移除多选", "按住 Ctrl 点击节点；macOS 也可以按住 Cmd 点击节点。按住 Shift 点击节点同样会切换该节点的选中状态。"],
              ["桌面框选节点", "按住 Shift，从画布空白区域按住左键拖出选择框，松开后框内节点会成为当前多选组。"],
            ],
          },
        ],
      },
      {
        id: "mobile-canvas-controls",
        title: "移动端画布操作",
        blocks: [
          {
            type: "table",
            headers: ["模式", "说明"],
            rows: [
              ["浏览", "默认模式。单指拖动画布空白处会平移视野，点按节点会选中节点，双指捏合会缩放画布。"],
              ["编辑", "触控或触控笔可以拖动节点，也可以从节点输出连接点拖到目标节点创建连接。"],
              ["选择", "点按节点会加入或移出多选组；点按空白画布会退出临时选择模式。"],
              ["底部工具栏", "提供运行工作流、单节点、模板、详情、日志和图库入口。面板内容从底部展开。"],
            ],
          },
        ],
      },
      {
        id: "multi-select",
        title: "多选节点",
        blocks: [
          {
            type: "paragraph",
            text: "选中多个节点后，画布顶部会出现操作浮层。浮层显示已选数量，并提供“保存模板”“删除”和清空选择按钮。",
          },
          {
            type: "steps",
            items: [
              "按住 Ctrl / Cmd / Shift 点击第一个节点，把它加入多选。",
              "继续按住 Ctrl / Cmd / Shift 点击其他节点，追加或移除选中状态。",
              "如果节点很多，按住 Shift 后从画布空白处拖出选择框，框住目标节点。",
              "多选完成后，按住任意一个已选节点拖动，可以移动整组节点。",
              "点击顶部浮层的“保存模板”，可以把当前多选节点保存为用户模板。",
              "点击顶部浮层的“删除”，会删除选中节点及相关连线；点击右侧 X 可以清空多选。",
            ],
          },
          {
            type: "callout",
            title: "Shift 的两种用法",
            text: "Shift 点击节点会切换该节点的选中状态；Shift 从画布空白处拖动会触发框选。不按 Shift 从空白处拖动则是平移画布。",
          },
          {
            type: "list",
            items: [
              "保存为模板时，选中组不能包含商品节点。",
              "点击画布空白处会清空多选，保留当前主选节点。",
              "如果当前节点有未保存草稿，切换选择前页面会先尝试保存。",
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "templates",
    title: "模板",
    description: "模板按电商出图场景组织。创建商品时可用模板初始化画布，进入工作台后也可继续添加同一套场景模板。",
    category: "画布工作台",
    icon: Layers3,
    sections: [
      {
        id: "template-usage",
        title: "模板用法",
        blocks: [
          {
            type: "table",
            headers: ["位置", "用途"],
            rows: [
              ["新建商品", "选择一个场景模板初始化画布。"],
              ["商品工作台", "继续添加同一套内置场景模板；商品资料节点会自动复用当前画布已有节点。"],
              ["用户模板", "从当前画布多选节点后保存，用于商品工作台内追加流程。"],
            ],
          },
        ],
      },
      {
        id: "built-in-templates",
        title: "内置场景模板",
        blocks: [
          {
            type: "list",
            items: [
              "空白画布：只保留商品资料入口，适合自由编排。",
              "平台首图：电商主图、淘宝主图和白底图，用于搜索、推荐、详情首屏和平台规范素材。",
              "详情说服：SKU/变体、多角度、功能卖点、尺寸规格、尺度参照、包装清单、使用步骤、对比和细节/材质图。",
              "场景图册：模特/生活方式图和使用场景图，适合详情页图册、穿搭、家居、空间和搭配展示。",
              "内容种草：小红书图和短视频封面图，适合笔记封面、内容流和直播预热入口。",
              "活动投放：活动/促销图，适合促销位、活动入口和广告素材。",
            ],
          },
        ],
      },
      {
        id: "save-user-template",
        title: "保存用户模板",
        blocks: [
          {
            type: "steps",
            items: [
              "在画布中多选两个或更多节点：按住 Ctrl / Cmd / Shift 点击节点，或按住 Shift 从空白处框选。",
              "确认选中节点不包含商品节点。",
              "点击顶部多选浮层中的“保存模板”。",
              "填写模板名称和可选描述。",
              "保存后在右侧“模板”面板中查看自定义模板。",
            ],
          },
          {
            type: "callout",
            title: "模板不会保存产物",
            text: "用户模板只保存可复用配置和选中节点之间的内部连线，不保存商品资料、已生成图片或文案结果，当前不会出现在新建商品页。",
          },
        ],
      },
    ],
  },
  {
    slug: "generate-assets",
    title: "生成文案和图片",
    description: "文案节点负责生成文字资产，生图节点负责触发图片生成并填充下游参考图节点。",
    category: "画布工作台",
    icon: Image,
    sections: [
      {
        id: "copy-generation",
        title: "生成文案",
        blocks: [
          {
            type: "steps",
            items: [
              "选中商品节点，确认商品资料已保存。",
              "选中文案节点。",
              "填写生成要求，例如目标人群、语气、重点卖点。",
              "运行当前节点。",
              "检查并编辑生成后的摘要、正文、文案块或布局分区。空的可选字段默认收起，需要时再添加。",
            ],
          },
          {
            type: "callout",
            title: "文案不再强制四个字段",
            text: "文案节点保存 CopyPayloadV2。模型可以按场景输出自由正文、短标签块、视觉建议或布局说明；后续生图直接读取结构化文案上下文。",
          },
        ],
      },
      {
        id: "image-generation",
        title: "生成图片",
        blocks: [
          {
            type: "steps",
            items: [
              "选中生图节点。",
              "确认生图节点连接到至少一个下游参考图节点。",
              "填写图片要求，包括主体、背景、光线、构图和用途。",
              "运行当前节点或运行工作流。",
              "在下游参考图节点或“图库”面板查看结果。",
            ],
          },
          {
            type: "callout",
            title: "生图节点不是图片槽位",
            text: "生成图片会写入下游参考图节点。若没有下游参考图，系统会提示先连接图片/参考图节点。",
          },
        ],
      },
      {
        id: "prompt-pattern",
        title: "提示词写法",
        blocks: [
          {
            type: "code",
            text: "白色托特包放在通勤桌面，旁边有笔记本电脑和咖啡，干净自然光，商品主体完整，纹理清晰，适合电商主图。",
          },
          {
            type: "paragraph",
            text: "每轮只改一两个因素，例如背景、构图、光线或主体细节。一次改太多会很难判断哪句话影响了结果。",
          },
        ],
      },
    ],
  },
  {
    slug: "image-chat",
    title: "文/图生图概览",
    description: "文/图生图用于独立图片会话、连续改图、多候选比较、参考图控制，以及把结果回写商品或投至画廊。",
    category: "文/图生图",
    icon: Sparkles,
    sections: [
      {
        id: "layout",
        title: "页面结构",
        blocks: [
          {
            type: "table",
            headers: ["区域", "用途"],
            rows: [
              ["桌面左侧会话列表", "新建、选择、重命名或删除文/图生图会话。每个会话保留自己的历史、参考图和生成任务。"],
              ["桌面中间结果区", "展示当前选中的生成候选、生成中占位、失败状态、下载按钮和“投至画廊”按钮。"],
              ["桌面底部历史记录", "按分支展示历史结果。点击已完成图片会把它选为当前结果，并作为下一轮基图。"],
              ["桌面右侧生成设置", "管理关联商品、保存到商品、会话参考图、画面描述、尺寸、候选数量和高级图片工具参数。"],
              ["移动端顶部栏", "左侧按钮打开会话抽屉，中间显示当前会话标题；铅笔按钮用于重命名，右侧按钮打开历史抽屉。"],
              ["移动端左侧会话抽屉", "新建、选择和删除会话；会话卡片显示最近缩略图、轮数和更新时间。"],
              ["移动端右侧历史抽屉", "以窄抽屉展示分支和候选。点已完成图片会选为当前结果和下一轮基图，点占位会查看该候选状态。"],
              ["移动端底部快捷条", "始终提供生成入口；选中已完成结果后，还提供下载和投至画廊。"],
              ["移动端底部生成面板", "用生成设置/高级标签页管理商品关联、商品/会话参考图、画面描述、尺寸、候选数量和图片工具参数。"],
            ],
          },
        ],
      },
      {
        id: "create-session",
        title: "创建和选择会话",
        blocks: [
          {
            type: "steps",
            items: [
              "打开顶部导航中的“文/图生图”。",
              "桌面端点击左侧会话区域的“新建”按钮；移动端点击顶部左侧菜单，在会话抽屉中点击加号创建会话。",
              "如果从商品详情进入，页面会进入商品关联模式；如果从全局入口进入，可以先自由生成，也可以在右侧选择目标商品。",
              "点击会话卡片切换会话。卡片会显示最近结果缩略图、轮数和更新时间；移动端选择后抽屉会关闭并回到主视图。",
              "需要改会话名时，桌面端在右侧“生成设置”点击“重命名”；移动端点击顶部栏的铅笔，输入名称后点击保存按钮。",
              "删除会话使用会话卡片上的删除按钮；如果配置关闭了业务删除，按钮会禁用并提示当前不可删除。",
            ],
          },
          {
            type: "callout",
            title: "会话和商品不是同一个对象",
            text: "文/图生图会话可以关联商品，也可以自由生成。只有点击“加入参考图”“保存为参考图”或“设为商品主图参考”这类保存按钮时，当前候选才会写回商品素材库。",
          },
        ],
      },
    ],
  },
  {
    slug: "image-chat-references",
    title: "基图和参考图",
    description: "说明文/图生图里基图、会话参考图、商品参考图的区别，以及单轮图片上下文数量限制。",
    category: "文/图生图",
    icon: Sparkles,
    sections: [
      {
        id: "base-image",
        title: "基图和参考图的区别",
        blocks: [
          {
            type: "paragraph",
            text: "文/图生图每一轮可以同时使用“基图”和“参考图”。基图来自历史记录中选中的已完成图片，用于表达“在这张图基础上继续改”。参考图来自会话参考图或商品参考图，用于补充风格、材质、姿态、背景等上下文。",
          },
          {
            type: "steps",
            items: [
              "第一轮没有历史图时，直接在“画面描述”里写想生成的画面。",
              "生成完成后，在底部历史记录点击一张已完成图片；移动端从右侧历史抽屉点选。中间结果区会显示“已选基图”。",
              "如需更多视觉参考，在右侧“会话参考图”上传图片，或从商品参考图区域选择已有素材；移动端在底部生成面板的生成设置标签页操作这些区域。",
              "勾选参考图后再提交生成。系统会把基图和已选参考图一起作为本轮上下文。",
            ],
          },
        ],
      },
      {
        id: "reference-limit",
        title: "图片上下文数量",
        blocks: [
          {
            type: "callout",
            title: "单轮最多 6 张",
            text: "单轮最多选择 6 张图片上下文，这个数量包含历史基图和显式选择的参考图。如果已经选了基图，最多还能再选 5 张参考图。",
          },
          {
            type: "list",
            items: [
              "想保留主体角度时，优先选择历史结果作为基图。",
              "想补充材质、风格、背景或姿态时，选择会话参考图或商品参考图。",
              "如果结果偏离太多，减少参考图数量通常比继续堆参考图更容易定位问题。",
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "image-chat-generation",
    title: "生成设置",
    description: "说明画面描述、尺寸、候选数量和高级图片工具参数如何影响文/图生图任务。",
    category: "文/图生图",
    icon: Sparkles,
    sections: [
      {
        id: "generation-settings",
        title: "字段说明",
        blocks: [
          {
            type: "table",
            headers: ["设置", "说明"],
            rows: [
              ["商品关联", "商品详情进入时自动关联当前商品；全局入口进入时，可在生成设置里选择目标商品，之后才能把结果保存为商品参考图。"],
              ["商品参考图", "显示目标商品已有参考图和主图参考；选中已完成候选后，可把候选加入参考图或设为商品主图参考。"],
              ["会话参考图", "上传本会话可复用的参考图，并勾选参与本轮生成。单轮图片上下文数量仍受 6 张限制。"],
              ["画面描述", "本轮真正提交给生成任务的用户要求。写清主体、保留项、变化项、背景、构图、光线和用途。"],
              ["尺寸", "选择常用 1K / 2K / 4K 预设，或输入自定义宽高。提交前会按后端最大单边限制校准。"],
              ["候选数量", "决定本轮创建多少张候选。多候选会在历史记录中显示多个占位，完成后分别替换为结果。"],
              ["生成设置 / 高级标签页", "生成设置包含商品、参考图、描述、尺寸和候选数量；高级包含供应商图片工具参数。"],
              ["图片工具参数", "只显示配置页“可用 Tool 字段”中启用的字段，例如质量、格式、背景、输入保真度等。未启用字段不会提交。"],
              ["提交按钮", "桌面端在右侧设置底部，移动端在底部生成面板底部。按钮文案会按候选数量显示本轮要提交的数量。"],
            ],
          },
        ],
      },
      {
        id: "prompt-pattern",
        title: "连续改图写法",
        blocks: [
          {
            type: "code",
            text: "保持包的角度不变，背景换成更明亮的办公室。减少桌面杂物，只保留电脑和咖啡；包身纹理要清晰，阴影柔和。",
          },
          {
            type: "paragraph",
            text: "连续改图时，建议明确写“保持什么不变”和“只修改什么”。如果只写一个很宽泛的新描述，模型可能会把它当成重新生成，而不是局部调整。",
          },
        ],
      },
    ],
  },
  {
    slug: "image-chat-tasks",
    title: "任务与结果",
    description: "说明文/图生图任务状态、重试、取消、下载、投至画廊和保存回商品的规则。",
    category: "文/图生图",
    icon: Sparkles,
    sections: [
      {
        id: "run-status",
        title: "运行状态、重试和取消",
        blocks: [
          {
            type: "table",
            headers: ["状态", "页面表现"],
            rows: [
              ["排队中", "中间结果区和历史记录会显示占位，可能显示队列位置、前方任务数和全局活跃数量。"],
              ["生成中", "占位会显示当前候选序号、候选总数、最近进度和供应商状态。"],
              ["生成完成", "占位替换为真实候选图，页面提示“新候选已生成”。"],
              ["失败", "显示失败原因；如果任务可重试，会出现“重试生成”。"],
              ["已取消", "显示任务已取消，不再写入新的候选结果。"],
            ],
          },
          {
            type: "list",
            items: [
              "运行中的任务可点击“取消生成”。",
              "失败且可重试的任务可点击“重试生成”。重试复用原任务的提示词、尺寸、参考图和高级参数。",
              "如果你已经修改了画面描述、尺寸或参考图，应提交新一轮生成，而不是重试旧失败任务。",
              "页面运行中只轮询轻量状态，任务结束后再刷新完整会话详情。",
            ],
          },
        ],
      },
      {
        id: "save-results",
        title: "保存结果",
        blocks: [
          {
            type: "table",
            headers: ["操作", "结果"],
            rows: [
              ["下载", "下载当前选中候选的原图。"],
              ["投至画廊", "把当前候选保存到全局画廊，保留来源会话、商品、提示词、尺寸、模型和下载入口。"],
              ["加入参考图 / 保存为参考图", "把当前候选写入目标商品的参考图素材，之后商品工作台和文/图生图都可以继续引用。"],
              ["设为商品主图参考", "把当前候选保存为商品主图参考素材，用于后续商品素材链路。"],
            ],
          },
          {
            type: "callout",
            title: "保存动作需要先选中候选",
            text: "只有中间结果区显示已完成图片时，下载、投至画廊和保存到商品才有明确目标。选中生成中占位或没有结果时，这些动作不会提交。",
          },
        ],
      },
      {
        id: "mobile-layout",
        title: "移动端布局",
        blocks: [
          {
            type: "table",
            headers: ["位置", "手机上的行为"],
            rows: [
              ["顶部栏", "左侧按钮打开会话抽屉，中间显示当前会话标题；铅笔进入重命名，右侧历史按钮打开窄历史抽屉。"],
              ["主视图", "生成状态、当前结果、失败原因和供应商提示保留可见。点当前结果可打开预览。"],
              ["右侧历史抽屉", "显示分支、候选和生成中占位。多候选提交后会先出现对应数量的占位；任务结束后刷新为真实候选或失败/取消状态。"],
              ["底部快捷条", "生成入口一直可用。选中已完成图片后，快捷条增加下载和投至画廊。"],
              ["底部生成面板", "生成设置标签页管理商品关联、商品参考图、会话参考图、画面描述、尺寸和候选数量；高级标签页管理图片工具参数。面板底部按钮提交本轮生成。"],
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "gallery",
    title: "画廊",
    description: "画廊用于收藏满意的文/图生图结果，方便集中浏览和下载。",
    category: "画廊",
    icon: GalleryHorizontalEnd,
    sections: [
      {
        id: "save-to-gallery",
        title: "保存到画廊",
        blocks: [
          {
            type: "paragraph",
            text: "文/图生图结果可以保存到画廊。画廊条目保留来源会话、关联商品、提示词、尺寸、模型和下载入口。",
          },
          {
            type: "list",
            items: [
              "适合保存暂时不挂回商品、但以后可能复用的背景或构图。",
              "适合保存需要集中给别人挑选的候选图。",
              "适合保存调参过程中效果不错但不是当前最终稿的图片。",
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "settings",
    title: "配置概览",
    description: "配置页用于管理运行时业务配置。基础设施配置仍由环境变量控制，不在设置页覆盖。",
    category: "配置",
    icon: Settings,
    sections: [
      {
        id: "settings-access",
        title: "访问和保存规则",
        blocks: [
          {
            type: "list",
            items: [
              "配置页需要先登录；如果设置页要求二次解锁，还需要输入 `SETTINGS_ACCESS_TOKEN`。",
              "配置项会显示来源。数据库覆盖值会标记为数据库来源；未覆盖时使用 env/default。",
              "只提交发生变化的字段。密钥字段留空不会覆盖已有值。",
              "点击恢复默认会删除数据库覆盖值，让该字段回到 env/default。",
            ],
          },
        ],
      },
      {
        id: "env-only",
        title: "Env-only 配置",
        blocks: [
          {
            type: "list",
            items: [
              "`DATABASE_URL`、`REDIS_URL`、`SESSION_SECRET`、`ADMIN_ACCESS_KEY` 等基础设施配置不支持设置页覆盖。",
              "设置页二次解锁由 `SETTINGS_ACCESS_TOKEN` 保护。",
              "关闭登录门禁不会关闭设置页二次解锁。",
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "settings-providers",
    title: "模型供应商",
    description: "说明供应商档案、文案/图片用途绑定、模型和图片生成基础参数。",
    category: "配置",
    icon: Settings,
    sections: [
      {
        id: "text-settings",
        title: "文案生成",
        blocks: [
          {
            type: "table",
            headers: ["字段", "说明"],
            rows: [
              ["供应商档案", "保存供应商类型、连接信息、API Key 和能力。Google Gemini 使用官方 SDK endpoint，不配置 Base URL；密钥不会回显，编辑档案时留空 API Key 会保留旧值。"],
              ["文案用途绑定", "选择 `mock` 或真实 OpenAI Responses 兼容接口，并选择具备文案能力的供应商档案。"],
              ["商品理解模型", "用于把商品名称、类目、价格、说明等整理成 CreativeBrief。"],
              ["文案生成模型", "用于生成 CopyPayloadV2 结构化文案，可包含自由正文、文案块、布局分区和视觉建议。"],
            ],
          },
        ],
      },
      {
        id: "image-settings",
        title: "图片生成",
        blocks: [
          {
            type: "table",
            headers: ["字段", "说明"],
            rows: [
              ["供应商档案", "OpenAI 兼容档案可以同时声明文案、Responses 图片和 Images API 图片能力；Google Gemini 档案只声明 Gemini 图片能力。"],
              ["图片用途绑定", "选择 `mock`、OpenAI Responses、OpenAI Images API 或 Google Gemini Image，并选择具备对应图片能力的供应商档案。"],
              ["图片模型", "发送给图片 provider 的默认图片模型。Responses、Images API 与 Gemini 支持范围不同。"],
              ["Responses 后台响应模式", "只属于 OpenAI Responses 图片绑定。开启后长任务先拿到 response_id 再轮询状态；如果网关明确不支持，会按同步请求重试。"],
              ["Images API Quality / Style", "只属于 OpenAI Images API 图片绑定。兼容网关不支持可选字段时会按基础参数重试。"],
              ["Gemini API 版本 / 输出 MIME", "只属于 Google Gemini 图片绑定。API 版本默认 `v1beta`，输出 MIME 留空时使用供应商默认值。"],
              ["生图最大单边", "工作台生图和文/图生图的最大宽/高像素。最大面积同步使用该值平方。"],
              ["主图尺寸（兼容默认）", "高级兼容值。只有当 provider 输入未明确传入 image_size 且类型为主图时才使用。新工作流优先看节点里的尺寸选择器。"],
              ["促销海报尺寸（兼容默认）", "高级兼容值。只有当 provider 输入未明确传入 image_size 且类型为促销海报时才使用。"],
              ["海报生成模式", "`模板渲染` 不消耗图片模型；`AI 生成` 会调用图片供应商。"],
              ["海报字体路径", "模板海报和 mock 图片中用于中文文字渲染的字体文件。"],
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "settings-image-tool",
    title: "图片工具参数",
    description: "说明 Responses 图片工具高级字段的含义，以及它们和前端可见控件、后端持久化的关系。",
    category: "配置",
    icon: Settings,
    sections: [
      {
        id: "tool-settings",
        title: "字段说明",
        blocks: [
          {
            type: "paragraph",
            text: "图片工具参数是发送给 Responses `image_generation` tool 的高级字段。配置页的“可用 Tool 字段”决定前端哪些高级控件可见，也决定后端哪些字段可以持久化并发送给 provider。",
          },
          {
            type: "table",
            headers: ["字段", "说明"],
            rows: [
              ["可用 Tool 字段", "多选字段。未勾选的高级字段不会在前端显示，也不会发送给 provider。"],
              ["Tool 模型", "发送到 image_generation tool 内部的模型字段。留空不发送，需要 provider 支持。"],
              ["质量", "可选默认、Auto、Low、Medium、High。用于支持质量参数的 provider。"],
              ["格式", "可选默认、PNG、JPEG、WebP。影响 provider 输出格式。"],
              ["压缩", "0-100；留空不发送。通常只对 JPEG/WebP 等格式有意义。"],
              ["背景", "可选默认、Auto、Opaque、Transparent。仅在可用 Tool 字段勾选 background 后发送。"],
              ["审核", "可选默认、Auto、Low。是否生效取决于 provider 支持。"],
              ["Action", "可选默认、Auto、Generate、Edit。用于提示 provider 当前更像生成还是编辑。"],
              ["Input fidelity", "可选默认、Low、High。用于控制输入参考图保真度，需 provider 支持。"],
              ["Partial", "0-3；留空不发送。用于支持 partial images 的 provider。"],
              ["Provider n", "高级 provider 字段，不改变 ProductFlow 文/图生图“候选数量”的产品语义。"],
            ],
          },
          {
            type: "callout",
            title: "候选数量和 Provider n 不等价",
            text: "文/图生图右侧的“候选数量”会创建 ProductFlow 自己的候选任务语义；`Provider n` 是透传给 provider 的高级字段，默认不应把它当成页面候选数量来用。",
          },
        ],
      },
    ],
  },
  {
    slug: "settings-prompts",
    title: "提示词模板",
    description: "说明全局提示词模板负责哪些默认行为，以及哪些要求应该留在单次节点或文/图生图输入里。",
    category: "配置",
    icon: Settings,
    sections: [
      {
        id: "prompt-settings",
        title: "字段说明",
        blocks: [
          {
            type: "table",
            headers: ["字段", "说明"],
            rows: [
              ["商品理解系统提示词", "用于商品资料理解，要求模型输出 CreativeBrief JSON。"],
              ["文案生成系统提示词", "用于主图/海报文案生成，要求模型输出 CopyPayloadV2 JSON；后端会兼容常见的自由文案、块状文案和布局说明变体。"],
              ["海报生图提示词模板", "用于工作台 AI 生图。常用占位符包括 `instruction`、`size`、`context_block`、`reference_policy`、`kind` 等。"],
              ["图片改图提示词模板", "用于工作台参考图/生成图继续生图。适合带上游文案或参考图上下文的场景。"],
              ["工作台视觉参考规则", "填入工作台生图模板的 `reference_policy` 占位符，用于控制视觉参考优先级规则。"],
              ["文/图生图提示词模板", "用于文/图生图对话。可用占位符：`prompt`、`size`、`history_block`。"],
            ],
          },
          {
            type: "callout",
            title: "单次要求不要写进全局模板",
            text: "如果只是这一次想要某种背景、构图或语气，应写在节点要求或文/图生图的画面描述里。提示词模板适合长期默认行为。",
          },
        ],
      },
    ],
  },
  {
    slug: "settings-operations",
    title: "上传、队列与安全",
    description: "说明上传限制、生成并发、任务恢复、provider 超时和安全开关这些运维类配置。",
    category: "配置",
    icon: Settings,
    sections: [
      {
        id: "upload-and-queue",
        title: "上传、队列和恢复",
        blocks: [
          {
            type: "table",
            headers: ["字段", "说明"],
            rows: [
              ["单图最大字节数", "限制单张上传图片大小。"],
              ["最多参考图数量", "限制参考图数量，文/图生图单轮上下文还会受到 6 张图片上下文限制。"],
              ["最大像素数", "限制上传图片的像素面积。"],
              ["允许图片 MIME", "逗号分隔，例如 `image/png,image/jpeg,image/webp`。"],
              ["全局生成并发上限", "工作流和文/图生图共享的资源保护阈值。达到上限时页面会提示稍后重试。"],
              ["文/图生图进度闲置恢复阈值", "worker 启动恢复时，running 文/图生图任务会按最近 progress heartbeat 判断是否闲置。"],
              ["工作流生图 Provider 超时", "工作流 AI 生图节点单次 provider 调用的项目级超时上界。超时后任务安全失败并释放队列容量。"],
            ],
          },
        ],
      },
      {
        id: "security-settings",
        title: "安全与运维",
        blocks: [
          {
            type: "paragraph",
            text: "密钥字段不会在 API 响应和页面中回显。留空保存不会覆盖已有密钥；只有输入新值才会写入数据库覆盖。",
          },
          {
            type: "table",
            headers: ["字段", "说明"],
            rows: [
              ["要求登录访问密钥", "默认开启。普通工作台和私有 API 需要 `ADMIN_ACCESS_KEY` 登录；关闭后仍需 `SETTINGS_ACCESS_TOKEN` 才能查看和修改系统配置。"],
              ["启用业务删除", "默认关闭。用于体验站禁止整条商品和文/图生图会话被删除，保留溯源证据。工作流节点/连线编辑和参考图删除不受该开关影响。"],
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "troubleshooting",
    title: "故障排查",
    description: "先看页面上的失败原因，再决定重试、取消、修改提示词、调整参数或检查供应商配置。",
    category: "配置",
    icon: TriangleAlert,
    sections: [
      {
        id: "failure-categories",
        title: "失败分类",
        blocks: [
          {
            type: "table",
            headers: ["提示", "处理方式"],
            rows: [
              ["配额或限流", "稍后重试，或降低并发。"],
              ["内容策略", "调整提示词或参考图。"],
              ["网络中断", "检查网络、代理和供应商可用性。"],
              ["请求超时", "稍后重试；重复出现时检查供应商状态和超时配置。"],
              ["参数不支持", "检查尺寸、模型和高级参数。"],
            ],
          },
        ],
      },
      {
        id: "retry-or-new-run",
        title: "重试还是重新运行",
        blocks: [
          {
            type: "paragraph",
            text: "重试适合临时失败，通常复用本次任务的提示词、尺寸、参考图和高级参数。如果你已经修改商品资料、文案、参考图或图片要求，应发起新的运行。",
          },
        ],
      },
      {
        id: "stuck-running",
        title: "任务长时间运行中",
        blocks: [
          {
            type: "list",
            items: [
              "运行中页面只轮询轻量 status，任务结束后才刷新完整详情。",
              "可取消的运行会显示取消入口。",
              "API 和 worker 启动时会恢复未完成任务。",
              "如果刷新后仍没有变化，检查后端、worker、Redis 和供应商日志。",
            ],
          },
        ],
      },
    ],
  },
];

const NAV_GROUPS: NavGroup[] = [
  {
    title: "入门",
    pages: ["overview", "quickstart"],
  },
  {
    title: "画布工作台",
    pages: ["workbench", "canvas-nodes", "templates", "generate-assets"],
  },
  {
    title: "画廊",
    pages: ["gallery"],
  },
  {
    title: "文/图生图",
    pages: ["image-chat", "image-chat-references", "image-chat-generation", "image-chat-tasks"],
  },
  {
    title: "配置",
    pages: [
      "settings",
      "settings-providers",
      "settings-image-tool",
      "settings-prompts",
      "settings-operations",
      "troubleshooting",
    ],
  },
];

const DOC_PAGES_EN: DocPage[] = [
  {
    slug: "overview",
    title: "ProductFlow Docs Overview",
    description: "ProductFlow is a self-hosted, single-admin product asset workbench for organizing product data, references, copy, and image generation in one traceable workspace.",
    category: "Getting started",
    icon: BookOpen,
    sections: [
      {
        id: "what-is-productflow",
        title: "What ProductFlow is",
        blocks: [
          { type: "paragraph", text: "ProductFlow is built for solo merchants, small operations teams, and developers who want a private AI asset pipeline. It keeps product information, image assets, copy generation, image generation, run state, and gallery saves in one private workbench." },
          { type: "paragraph", text: "This version is not a public registration platform or a multi-tenant SaaS product. The deployer manages the database, Redis, storage directory, and model credentials." },
        ],
      },
      {
        id: "main-surfaces",
        title: "Main surfaces",
        blocks: [
          {
            type: "table",
            headers: ["Page", "Purpose"],
            rows: [
              ["Products", "Create products, open the product workbench, organize nodes, and run workflows."],
              ["Image chat", "Continue generating and editing around image results, compare candidates, and write results back to products."],
              ["Gallery", "Save selected generated images with source, prompt, size, model, and download metadata."],
              ["Settings", "Manage providers, models, sizes, prompt templates, upload limits, and secrets."],
              ["Help", "Read the built-in product operation docs."],
            ],
          },
        ],
      },
      {
        id: "recommended-path",
        title: "Recommended reading path",
        blocks: [
          {
            type: "steps",
            items: [
              "Start with Quick start to complete one flow from product image to generated image.",
              "Read Product workbench, Canvas and nodes, Templates, and Generate copy and images to understand the canvas workflow.",
              "Read Gallery when you need to save and review generated images.",
              "Read Image chat overview, Base and reference images, Generation settings, and Tasks and results for iterative image editing.",
              "Read Settings overview, Model providers, Image tool parameters, Prompt templates, Operations and safety, and Troubleshooting for deployment and operations.",
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "quickstart",
    title: "Quick start",
    description: "Create a product, choose an initial canvas template, and generate the first product image with the shortest path.",
    category: "Getting started",
    icon: TerminalSquare,
    sections: [
      { id: "before-you-start", title: "Before you start", blocks: [{ type: "list", items: ["Backend API, worker, PostgreSQL, and Redis should be available.", "If login protection is enabled, sign in with the admin key first.", "Prepare a clear product main image. JPG, PNG, and WebP are recommended."] }] },
      {
        id: "create-product",
        title: "Create a product",
        blocks: [
          { type: "steps", items: ["Open Products from the top navigation.", "Click New product.", "Upload the product main image.", "Enter the product name.", "Choose a canvas template. For first use, choose a product hero template.", "Click Create and continue."] },
          { type: "callout", title: "Templates can be reused", text: "After the product is created, the Templates panel in the product workbench can still add scene templates. Product context from the template is automatically connected to the current canvas product node." },
        ],
      },
      { id: "first-run", title: "Generate the first image", blocks: [{ type: "steps", items: ["In the product workbench, click the product node and complete category, price, and description in Details.", "Select a copy node, enter generation requirements, and run the node.", "Review the copy output and edit it directly when needed.", "Select an image generation node and confirm that it connects to at least one downstream reference image node.", "Enter image requirements and run the node or the full workflow.", "View and download results from downstream reference image nodes or the Library panel."] }] },
    ],
  },
  {
    slug: "workbench",
    title: "Product workbench",
    description: "The product workbench is the core operating surface. Desktop keeps the canvas in the center and inspectors on the right; mobile keeps the canvas as the main surface.",
    category: "Canvas workbench",
    icon: GitBranch,
    sections: [
      { id: "layout", title: "Layout", blocks: [{ type: "table", headers: ["Area", "Description"], rows: [["Canvas", "Shows product, reference image, copy, and image generation nodes plus their edges."], ["Details", "Edits the selected node configuration and output."], ["Runs", "Shows workflow run history, failure reasons, and retryable runs."], ["Library", "Shows product assets, generated images, and images that can fill reference nodes."], ["Templates", "Inserts built-in scene templates or manages user-saved templates."], ["Mobile bottom toolbar", "Switches Browse, Edit, and Select modes, and opens workflow run, Single node, Templates, Details, Runs, and Library entrypoints."], ["Mobile bottom sheet", "Carries the desktop right-panel Single node, Templates, Details, Runs, and Library content on phones."]] }] },
      { id: "mobile-workbench", title: "Mobile workbench", blocks: [{ type: "paragraph", text: "On phones, the product detail page keeps the canvas as the main operating area. The bottom mode control provides Browse, Edit, and Select. Browse pans the canvas, selects nodes, and supports two-finger pinch zoom. Edit allows node dragging and edge creation. Select lets taps add or remove nodes from multi-select." }, { type: "paragraph", text: "The bottom toolbar also provides workflow run, Single node, Templates, Details, Runs, and Library entrypoints. Opening one of these entrypoints expands a bottom sheet; closing it returns to the canvas." }] },
      { id: "node-types", title: "Node types", blocks: [{ type: "table", headers: ["Node", "Description"], rows: [["Product", "Product context entry for name, category, price, and description."], ["Reference image", "A single image slot that can be uploaded manually or filled by an upstream image generation node."], ["Copy", "Generates and edits structured copy. It can be freeform text, copy blocks, or layout sections, and downstream image generation reads the structured copy context."], ["Image generation", "Triggers image generation. Results are written into downstream reference image nodes and are not downloaded from the generation node itself."]] }] },
      { id: "run-model", title: "Run model", blocks: [{ type: "paragraph", text: "Edge direction controls which context a downstream node can read at run time. Connecting A to B means B can reference A." }, { type: "callout", title: "Save before running", text: "Workflow runs read saved content. If Details contains an unsaved draft, the run button first attempts to save it. If saving fails, the run is not submitted." }] },
    ],
  },
  {
    slug: "canvas-nodes",
    title: "Canvas and nodes",
    description: "The canvas supports zooming, panning, node dragging, edge creation, box selection, and multi-select operations.",
    category: "Canvas workbench",
    icon: Box,
    sections: [
      { id: "canvas-controls", title: "Canvas controls", blocks: [{ type: "table", headers: ["Action", "Description"], rows: [["Desktop zoom", "Use the mouse wheel, or the bottom-right - / percentage / + controls."], ["Desktop pan", "Drag an empty canvas area with the left mouse button when no modifier is pressed. Dragging nodes, clicking controls, uploading, and drawing edges do not pan the canvas."], ["Desktop move one node", "Drag the node body or title area. The position is saved after release."], ["Desktop move multiple nodes", "Select multiple nodes, then drag any selected node to move the group."], ["Desktop create an edge", "Drag from the output handle on the right of a node to the input handle on the left of the target node."], ["Select one node", "Click the node body or the input handle. The Details panel shows that node."], ["Desktop add or remove from multi-select", "Ctrl-click a node. On macOS, Cmd-click also works. Shift-click also toggles selection."], ["Desktop box select", "Hold Shift, drag from an empty canvas area, and release to select nodes inside the box."]] }] },
      { id: "mobile-canvas-controls", title: "Mobile canvas controls", blocks: [{ type: "table", headers: ["Mode", "Description"], rows: [["Browse", "Default mode. One-finger dragging on blank canvas pans the view, tapping a node selects it, and two-finger pinch zooms the canvas."], ["Edit", "Touch and pen input can drag nodes and create edges by dragging from an output handle to a target node."], ["Select", "Tapping nodes adds or removes them from multi-select. Tapping blank canvas exits the temporary selection mode."], ["Bottom toolbar", "Provides workflow run, Single node, Templates, Details, Runs, and Library entrypoints. Panel content opens from the bottom sheet."]] }] },
      { id: "multi-select", title: "Multi-select nodes", blocks: [{ type: "paragraph", text: "After selecting multiple nodes, an action bar appears at the top of the canvas. It shows the selected count and provides Save template, Delete, and clear selection controls." }, { type: "steps", items: ["Ctrl / Cmd / Shift-click the first node to add it to the selection.", "Keep using Ctrl / Cmd / Shift-click to add or remove nodes.", "For dense canvases, hold Shift and drag a box from an empty area.", "After selection, drag any selected node to move the group.", "Click Save template in the top action bar to save selected nodes as a user template.", "Click Delete to remove selected nodes and related edges; click X to clear multi-select."] }, { type: "callout", title: "Two uses of Shift", text: "Shift-click toggles a node. Shift-drag from an empty area starts box selection. Dragging an empty area without Shift pans the canvas." }, { type: "list", items: ["A saved template cannot include the product node.", "Clicking an empty canvas area clears multi-select while preserving the primary selected node.", "If the current node has an unsaved draft, the page attempts to save it before switching selection."] }] },
    ],
  },
  {
    slug: "templates",
    title: "Templates",
    description: "Templates are organized by ecommerce image scenarios. They can initialize a new product canvas or be added inside the workbench.",
    category: "Canvas workbench",
    icon: Layers3,
    sections: [
      { id: "template-usage", title: "How templates are used", blocks: [{ type: "table", headers: ["Location", "Use"], rows: [["New product", "Choose one scene template to initialize the canvas."], ["Product workbench", "Add more built-in scene templates; the product context node reuses the existing product node on the canvas."], ["User templates", "Save selected nodes from the current canvas for later insertion inside the product workbench."]] }] },
      { id: "built-in-templates", title: "Built-in scene templates", blocks: [{ type: "list", items: ["Blank canvas: keeps only the product context entry for free arrangement.", "Listing hero: ecommerce hero, marketplace main image, and white-background assets for search, recommendations, first-screen detail, and platform requirements.", "Detail persuasion: SKU and variants, multiple angles, feature benefits, size specs, scale references, packing lists, usage steps, comparisons, and material/detail images.", "Scene gallery: model/lifestyle and usage-scene images for detail galleries, styling, home, space, and pairing displays.", "Content seeding: social note images and short-video covers for content feeds and live previews.", "Campaign: promotion images for campaign slots, event entry points, and ads."] }] },
      { id: "save-user-template", title: "Save a user template", blocks: [{ type: "steps", items: ["Select two or more nodes on the canvas with Ctrl / Cmd / Shift-click, or hold Shift and box-select from an empty area.", "Confirm that the selected group does not include the product node.", "Click Save template in the top multi-select action bar.", "Enter a template name and optional description.", "After saving, view the custom template in the right Templates panel."] }, { type: "callout", title: "Templates do not save artifacts", text: "User templates save reusable configuration and internal edges among selected nodes. They do not save product data, generated images, or copy outputs, and they currently do not appear on the new product page." }] },
    ],
  },
  {
    slug: "generate-assets",
    title: "Generate copy and images",
    description: "Copy nodes generate text assets. Image generation nodes trigger image generation and fill downstream reference image nodes.",
    category: "Canvas workbench",
    icon: Image,
    sections: [
      { id: "copy-generation", title: "Generate copy", blocks: [{ type: "steps", items: ["Select the product node and confirm product data is saved.", "Select a copy node.", "Enter requirements such as audience, tone, and key selling points.", "Run the current node.", "Review and edit the generated summary, body, copy blocks, or layout sections. Empty optional fields stay collapsed until needed."] }, { type: "callout", title: "Copy is no longer forced into four fields", text: "Copy nodes save CopyPayloadV2. The model can output freeform text, short copy blocks, visual guidance, or layout notes by scenario. Downstream image generation reads the structured copy context directly." }] },
      { id: "image-generation", title: "Generate images", blocks: [{ type: "steps", items: ["Select an image generation node.", "Confirm that it connects to at least one downstream reference image node.", "Enter image requirements, including subject, background, lighting, composition, and purpose.", "Run the current node or the workflow.", "View results in downstream reference image nodes or the Library panel."] }, { type: "callout", title: "Image generation nodes are not image slots", text: "Generated images are written into downstream reference image nodes. If no downstream reference image exists, the system asks you to connect an image/reference node first." }] },
      { id: "prompt-pattern", title: "Prompt pattern", blocks: [{ type: "code", text: "Place a white tote bag on a commuter desk beside a laptop and coffee. Use clean natural light, keep the full product visible, preserve clear texture, and make it suitable for an ecommerce hero image." }, { type: "paragraph", text: "Change only one or two factors per run, such as background, composition, lighting, or product details. Changing too much at once makes it difficult to identify which phrase affected the result." }] },
    ],
  },
  {
    slug: "image-chat",
    title: "Image chat overview",
    description: "Image chat supports independent image sessions, iterative edits, multi-candidate comparison, reference control, and saving results back to products or the gallery.",
    category: "Image chat",
    icon: Sparkles,
    sections: [
      { id: "layout", title: "Page layout", blocks: [{ type: "table", headers: ["Area", "Use"], rows: [["Desktop left session list", "Create, select, rename, or delete image chat sessions. Each session keeps its own history, references, and generation tasks."], ["Desktop center result area", "Shows the selected generated candidate, running placeholders, failed state, download button, and Send to gallery button."], ["Desktop bottom history", "Shows results by branch. Clicking a completed image selects it as the current result and base image for the next round."], ["Desktop right generation settings", "Manages linked product, save-to-product actions, session references, image description, size, candidate count, and advanced image tool parameters."], ["Mobile top bar", "The left button opens the session drawer, the center shows the current session title, the pencil renames it, and the right button opens history."], ["Mobile left session drawer", "Creates, selects, and deletes sessions. Session cards show the latest thumbnail, round count, and update time."], ["Mobile right history drawer", "Shows branches and candidates in a narrow drawer. Tapping a completed image selects it as the current result and next base image; tapping a placeholder shows that candidate state."], ["Mobile bottom action bar", "Always exposes generation. After a completed result is selected, it also exposes download and send-to-gallery."], ["Mobile bottom generation sheet", "Uses Generation / Advanced tabs for product linking, product/session references, image description, size, candidate count, and image tool parameters."]] }] },
      { id: "create-session", title: "Create and select sessions", blocks: [{ type: "steps", items: ["Open Image chat from the top navigation.", "On desktop, click New in the left session area. On mobile, tap the top-left menu and use the plus button in the session drawer.", "If you entered from a product detail page, the page uses product-linked mode. From the global entry, you can generate freely or select a target product in settings.", "Click or tap any session card to switch sessions. Cards show the latest thumbnail, round count, and update time; on mobile, selection closes the drawer and returns to the main view.", "To rename a session, use Rename in desktop Generation settings or tap the pencil in the mobile top bar, enter a name, and save.", "Delete a session from the session card delete button. If business deletion is disabled in Settings, the delete button is disabled and explains why."] }, { type: "callout", title: "Sessions and products are different objects", text: "An image chat session can be linked to a product or used freely. The current candidate is written back to the product asset library only when you click actions such as Add reference image, Save as reference, or Set as product main image." }] },
    ],
  },
  {
    slug: "image-chat-references",
    title: "Base and reference images",
    description: "Explains the difference between base images, session references, product references, and the per-round image context limit.",
    category: "Image chat",
    icon: Sparkles,
    sections: [
      { id: "base-image", title: "Base image vs reference image", blocks: [{ type: "paragraph", text: "Each image chat round can use both a base image and reference images. The base image comes from a selected completed history result and means continue editing from this image. Reference images come from session references or product references and provide extra context such as style, material, pose, or background." }, { type: "steps", items: ["For the first round, write the desired image directly in Image description.", "After generation completes, click a completed image in bottom history; on mobile, select it from the right history drawer. The center result area shows Base selected.", "For more visual context, upload images in Session references or select existing assets from Product references; on mobile, these controls live in the Generation tab of the bottom generation sheet.", "Select references before submitting. The system sends the base image and selected references together as this round's context."] }] },
      { id: "reference-limit", title: "Image context count", blocks: [{ type: "callout", title: "Up to 6 images per round", text: "One round can select up to 6 image contexts. This count includes the history base image and explicitly selected references. If a base image is selected, at most 5 more references can be selected." }, { type: "list", items: ["Use a history result as the base when you want to preserve the product angle.", "Use session or product references when you need extra material, style, background, or pose context.", "When results drift too much, reducing references is often easier than adding more."] }] },
    ],
  },
  {
    slug: "image-chat-generation",
    title: "Generation settings",
    description: "Explains how image description, size, candidate count, and advanced image tool parameters affect image chat tasks.",
    category: "Image chat",
    icon: Sparkles,
    sections: [
      { id: "generation-settings", title: "Fields", blocks: [{ type: "table", headers: ["Setting", "Description"], rows: [["Product linking", "Product-detail entry links the current product automatically. Global entry can select a target product before saving a result as product reference material."], ["Product references", "Shows the target product's existing references and main-image reference. After a completed candidate is selected, it can be added as a reference or set as the product main-image reference."], ["Session references", "Uploads reusable references for this session and selects them for the next round. The per-round image context limit is still 6 images."], ["Image description", "The actual user requirement submitted for this round. State subject, what to preserve, what to change, background, composition, lighting, and purpose."], ["Size", "Choose common 1K / 2K / 4K presets or enter a custom width and height. Values are calibrated against the backend maximum single-edge limit before submission."], ["Candidate count", "Controls how many candidates are created for this round. Multiple candidates appear as placeholders in history and are replaced individually when complete."], ["Generation / Advanced tabs", "Generation contains product, references, description, size, and candidate count. Advanced contains provider image tool parameters."], ["Image tool parameters", "Only fields enabled in Settings under available tool fields are visible, such as quality, format, background, and input fidelity. Disabled fields are not submitted."], ["Submit button", "On desktop it stays at the bottom of the right settings panel; on mobile it stays at the bottom of the bottom generation sheet. Its label reflects the current candidate count."]] }] },
      { id: "prompt-pattern", title: "Iterative editing pattern", blocks: [{ type: "code", text: "Keep the bag angle unchanged and change the background to a brighter office. Reduce desk clutter, keep only the laptop and coffee, preserve clear bag texture, and use soft shadows." }, { type: "paragraph", text: "For iterative editing, explicitly say what to keep unchanged and what to modify. A broad new description may be treated as a fresh generation rather than a controlled edit." }] },
    ],
  },
  {
    slug: "image-chat-tasks",
    title: "Tasks and results",
    description: "Explains image chat task states, retry, cancel, download, send to gallery, and save-back-to-product rules.",
    category: "Image chat",
    icon: Sparkles,
    sections: [
      { id: "run-status", title: "Run status, retry, and cancel", blocks: [{ type: "table", headers: ["Status", "Page behavior"], rows: [["Queued", "The center result area and history show placeholders and may show queue position, tasks ahead, and global active count."], ["Generating", "The placeholder shows candidate index, total candidates, latest progress, and provider status."], ["Complete", "The placeholder is replaced by the real candidate image and the page reports that a new candidate was generated."], ["Failed", "The failure reason is shown. Retry generation appears when the task is retryable."], ["Cancelled", "The page shows the task as cancelled and no new candidate result is written."]] }, { type: "list", items: ["Running tasks can be cancelled with Cancel generation.", "Failed retryable tasks can use Retry generation. Retry reuses the original prompt, size, references, and advanced parameters.", "If you changed description, size, or references, submit a new generation instead of retrying the old failed task.", "While running, the page polls lightweight status and refreshes full session detail after the task ends."] }] },
      { id: "save-results", title: "Save results", blocks: [{ type: "table", headers: ["Action", "Result"], rows: [["Download", "Downloads the currently selected candidate original image."], ["Send to gallery", "Saves the current candidate to the global gallery with source session, product, prompt, size, model, and download entry."], ["Add reference / Save as reference", "Writes the current candidate into the target product's reference image assets for later use in product workbench and image chat."], ["Set as product main image", "Saves the current candidate as a product main image reference asset for later product asset workflows."]] }, { type: "callout", title: "Save actions require a selected candidate", text: "Download, Send to gallery, and save-to-product actions have a clear target only when the center result area shows a completed image. Selecting a running placeholder or having no result will not submit these actions." }] },
      { id: "mobile-layout", title: "Mobile layout", blocks: [{ type: "table", headers: ["Location", "Behavior on phones"], rows: [["Top bar", "The left button opens the session drawer, the center shows the current session title, the pencil starts rename, and the history button opens the narrow history drawer."], ["Main view", "Generation status, current result, failure reason, and provider notes remain visible. Tapping the current result opens preview."], ["Right history drawer", "Shows branches, candidates, and running placeholders. Multi-candidate submissions first create matching placeholders; after completion they refresh into real candidates or failed/cancelled states."], ["Bottom action bar", "Generation is always available. After a completed image is selected, the bar adds Download and Send to gallery."], ["Bottom generation sheet", "Generation manages product linking, product references, session references, image description, size, and candidate count; Advanced manages image tool parameters. The bottom button submits the next generation round."]] }] },
    ],
  },
  {
    slug: "gallery",
    title: "Gallery",
    description: "Gallery stores selected image chat results for centralized browsing and download.",
    category: "Gallery",
    icon: GalleryHorizontalEnd,
    sections: [
      { id: "save-to-gallery", title: "Save to gallery", blocks: [{ type: "paragraph", text: "Image chat results can be saved to the gallery. Each gallery entry keeps source session, linked product, prompt, size, model, and download access." }, { type: "list", items: ["Useful for reusable backgrounds or compositions before attaching them to a product.", "Useful for collecting candidates for others to review.", "Useful for saving good parameter-exploration results that are not the current final draft."] }] },
    ],
  },
  {
    slug: "settings",
    title: "Settings overview",
    description: "Settings manage runtime business configuration. Infrastructure configuration is still controlled by environment variables and is not overridden in the settings page.",
    category: "Settings",
    icon: Settings,
    sections: [
      { id: "settings-access", title: "Access and save rules", blocks: [{ type: "list", items: ["Settings require login. If the settings page requires secondary unlock, enter `SETTINGS_ACCESS_TOKEN` as well.", "Each setting shows its source. Database overrides are marked as database source; otherwise env/default is used.", "Only changed fields are submitted. Leaving secret fields blank does not overwrite existing values.", "Restore default removes the database override so the field falls back to env/default."] }] },
      { id: "env-only", title: "Env-only settings", blocks: [{ type: "list", items: ["Infrastructure settings such as `DATABASE_URL`, `REDIS_URL`, `SESSION_SECRET`, and `ADMIN_ACCESS_KEY` cannot be overridden in Settings.", "Settings secondary unlock is protected by `SETTINGS_ACCESS_TOKEN`.", "Disabling login protection does not disable the settings secondary unlock."] }] },
    ],
  },
  {
    slug: "settings-providers",
    title: "Model providers",
    description: "Explains provider profiles, copy/image purpose bindings, models, and base image generation parameters.",
    category: "Settings",
    icon: Settings,
    sections: [
      { id: "text-settings", title: "Copy generation", blocks: [{ type: "table", headers: ["Field", "Description"], rows: [["Provider profile", "Stores provider type, connection data, API key, and capabilities. Google Gemini uses the official SDK endpoint and does not configure a Base URL. Secrets are not returned; leaving API key blank while editing preserves the old value."], ["Copy purpose binding", "Selects `mock` or a real OpenAI Responses-compatible interface, and points to a provider profile with copy capability."], ["Product understanding model", "Organizes product name, category, price, and description into a CreativeBrief."], ["Copy generation model", "Generates CopyPayloadV2 structured copy, which can contain freeform text, copy blocks, layout sections, and visual guidance."]] }] },
      { id: "image-settings", title: "Image generation", blocks: [{ type: "table", headers: ["Field", "Description"], rows: [["Provider profile", "OpenAI-compatible profiles can declare copy, Responses image, and Images API image capabilities. Google Gemini profiles declare only Gemini image capability."], ["Image purpose binding", "Selects `mock`, OpenAI Responses, OpenAI Images API, or Google Gemini Image, and points to a provider profile with the matching image capability."], ["Image model", "Default image model sent to the image provider. Responses, Images API, and Gemini support different model sets."], ["Responses background mode", "Only belongs to the OpenAI Responses image binding. When enabled, long tasks first receive a response_id and then poll status; gateways that clearly do not support it retry as synchronous requests."], ["Images API Quality / Style", "Only belongs to the OpenAI Images API image binding. Compatible gateways that reject optional fields retry with the base parameters."], ["Gemini API version / output MIME", "Only belongs to the Google Gemini image binding. API version defaults to `v1beta`; blank output MIME uses the provider default."], ["Image max single edge", "Maximum width or height in pixels for workbench image generation and image chat. Maximum area uses this value squared."], ["Main image size (compat default)", "Advanced compatibility value used only when provider input does not explicitly send image_size and kind is main image. New workflows prefer the node size picker."], ["Promo poster size (compat default)", "Advanced compatibility value used only when provider input does not explicitly send image_size and kind is promo poster."], ["Poster generation mode", "`Template render` does not consume the image model; `AI generation` calls the image provider."], ["Poster font path", "Font file used for Chinese text rendering in template posters and mock images."]] }] },
    ],
  },
  {
    slug: "settings-image-tool",
    title: "Image tool parameters",
    description: "Explains advanced Responses image_generation tool fields and their relationship with visible controls and backend persistence.",
    category: "Settings",
    icon: Settings,
    sections: [
      { id: "tool-settings", title: "Fields", blocks: [{ type: "paragraph", text: "Image tool parameters are advanced fields sent to the Responses `image_generation` tool. The available tool fields in Settings decide which advanced controls appear in the frontend and which fields the backend can persist and send to the provider." }, { type: "table", headers: ["Field", "Description"], rows: [["Available tool fields", "Multi-select field. Unselected advanced fields are hidden in the frontend and are not sent to the provider."], ["Tool model", "Model field sent inside the image_generation tool. Leave blank to omit; requires provider support."], ["Quality", "Optional default, Auto, Low, Medium, or High for providers that support quality."], ["Format", "Optional default, PNG, JPEG, or WebP. Affects provider output format."], ["Compression", "0-100; blank means not sent. Usually meaningful only for JPEG/WebP."], ["Background", "Optional default, Auto, Opaque, or Transparent. Sent only when background is enabled in available tool fields."], ["Moderation", "Optional default, Auto, or Low. Effect depends on provider support."], ["Action", "Optional default, Auto, Generate, or Edit. Hints whether the task is closer to generation or editing."], ["Input fidelity", "Optional default, Low, or High for controlling reference image fidelity when supported."], ["Partial", "0-3; blank means not sent. Used by providers that support partial images."], ["Provider n", "Advanced provider field. It does not change ProductFlow's own candidate-count semantics in image chat."]] }, { type: "callout", title: "Candidate count and Provider n are not equivalent", text: "Candidate count in image chat creates ProductFlow candidate task semantics. `Provider n` is an advanced passthrough provider field and should not be treated as the page candidate count by default." }] },
    ],
  },
  {
    slug: "settings-prompts",
    title: "Prompt templates",
    description: "Explains which defaults global prompt templates control and which requirements should remain in one-off node or image chat inputs.",
    category: "Settings",
    icon: Settings,
    sections: [
      { id: "prompt-settings", title: "Fields", blocks: [{ type: "table", headers: ["Field", "Description"], rows: [["Product understanding system prompt", "Used for product data understanding and asks the model to output CreativeBrief JSON."], ["Copy generation system prompt", "Used for main image/poster copy generation and asks for CopyPayloadV2 JSON. The backend accepts common freeform, block, and layout variants."], ["Poster image prompt template", "Used for workbench AI image generation. Common placeholders include `instruction`, `size`, `context_block`, `reference_policy`, and `kind`."], ["Image edit prompt template", "Used for continuing generation from workbench reference or generated images. Suitable for scenarios that carry upstream copy or reference context."], ["Workbench visual reference policy", "Fills the `reference_policy` placeholder in workbench image templates to control visual-reference priority rules."], ["Image chat prompt template", "Used for image chat. Available placeholders include `prompt`, `size`, and `history_block`."]] }, { type: "callout", title: "Keep one-off requirements out of global templates", text: "If a background, composition, or tone is needed only for this run, put it in the node requirement or image chat description. Prompt templates are better for long-term default behavior." }] },
    ],
  },
  {
    slug: "settings-operations",
    title: "Operations and safety",
    description: "Explains upload limits, generation concurrency, task recovery, provider timeout, and safety switches.",
    category: "Settings",
    icon: Settings,
    sections: [
      { id: "upload-and-queue", title: "Upload, queue, and recovery", blocks: [{ type: "table", headers: ["Field", "Description"], rows: [["Max bytes per image", "Limits the size of one uploaded image."], ["Max reference images", "Limits reference image count. Image chat also has a 6-image context limit per round."], ["Max pixels", "Limits the pixel area of uploaded images."], ["Allowed image MIME", "Comma-separated list such as `image/png,image/jpeg,image/webp`."], ["Global generation concurrency", "Shared protection threshold for workflow and image chat generation. When reached, the page asks users to retry later."], ["Image chat progress stale recovery threshold", "During worker startup recovery, running image chat tasks are checked by recent progress heartbeat."], ["Workflow image provider timeout", "Project-level timeout ceiling for one workflow AI image generation provider call. Timeout safely fails the task and releases queue capacity."]] }] },
      { id: "security-settings", title: "Security and operations", blocks: [{ type: "paragraph", text: "Secrets are not returned by API responses or shown in the page. Leaving a secret field blank keeps the existing secret; only entering a new value writes a database override." }, { type: "table", headers: ["Field", "Description"], rows: [["Require login access key", "Enabled by default. The normal workbench and private APIs require `ADMIN_ACCESS_KEY` login; when disabled, `SETTINGS_ACCESS_TOKEN` is still required for system settings."], ["Enable business deletion", "Disabled by default. Used by demo deployments to prevent deleting whole products and image chat sessions, preserving traceability. Workflow node/edge editing and reference deletion are not controlled by this switch."]] }] },
    ],
  },
  {
    slug: "troubleshooting",
    title: "Troubleshooting",
    description: "Start with the failure reason shown in the page, then decide whether to retry, cancel, adjust the prompt, change parameters, or inspect provider configuration.",
    category: "Settings",
    icon: TriangleAlert,
    sections: [
      { id: "failure-categories", title: "Failure categories", blocks: [{ type: "table", headers: ["Message", "Action"], rows: [["Quota or rate limit", "Retry later or lower concurrency."], ["Content policy", "Adjust prompt or reference images."], ["Network interruption", "Check network, proxy, and provider availability."], ["Request timeout", "Retry later; if repeated, check provider status and timeout settings."], ["Unsupported parameter", "Check size, model, and advanced parameters."]] }] },
      { id: "retry-or-new-run", title: "Retry or run again", blocks: [{ type: "paragraph", text: "Retry is suitable for temporary failures and usually reuses the task's prompt, size, references, and advanced parameters. If product data, copy, references, or image requirements changed, start a new run." }] },
      { id: "stuck-running", title: "Task stays running for a long time", blocks: [{ type: "list", items: ["Running pages poll lightweight status and refresh full detail only after the task ends.", "Cancelable runs show a cancel control.", "API and worker startup recover unfinished tasks.", "If refresh still shows no change, inspect backend, worker, Redis, and provider logs."] }] },
    ],
  },
];

const NAV_GROUPS_EN: NavGroup[] = [
  { title: "Getting started", pages: ["overview", "quickstart"] },
  { title: "Canvas workbench", pages: ["workbench", "canvas-nodes", "templates", "generate-assets"] },
  { title: "Gallery", pages: ["gallery"] },
  { title: "Image chat", pages: ["image-chat", "image-chat-references", "image-chat-generation", "image-chat-tasks"] },
  {
    title: "Settings",
    pages: [
      "settings",
      "settings-providers",
      "settings-image-tool",
      "settings-prompts",
      "settings-operations",
      "troubleshooting",
    ],
  },
];

const HELP_DOC_JA_TRANSLATIONS: Record<string, string> = {
  "ProductFlow 文档概览": "ProductFlow ドキュメント概要",
  "ProductFlow 是单管理员自托管的商品素材工作台，用于把商品资料、参考图、文案和图片生成流程组织在一个可追踪的工作台中。":
    "ProductFlow は単一管理者で自ホストする商品素材ワークベンチです。商品データ、参考画像、コピー、画像生成フローを、追跡できる1つのワークスペースに整理します。",
  "入门": "はじめに",
  "ProductFlow 是什么": "ProductFlow とは",
  "ProductFlow 面向单人商家、小团队运营者和希望自托管 AI 素材链路的开发者。它把商品信息、图片素材、文案生成、图片生成、运行状态和画廊收藏放在同一个私有工作台中。":
    "ProductFlow は、個人商店、小規模運用チーム、自ホストの AI 素材パイプラインを求める開発者向けです。商品情報、画像素材、コピー生成、画像生成、実行状態、ギャラリー保存を1つの非公開ワークベンチで扱えます。",
  "当前版本不是公开注册平台，也不是多租户 SaaS。部署者自己管理数据库、Redis、存储目录和模型密钥。":
    "現在のバージョンは公開登録型プラットフォームでもマルチテナント SaaS でもありません。デプロイする人がデータベース、Redis、保存ディレクトリ、モデル認証情報を管理します。",
  "主要页面": "主要ページ",
  "页面": "ページ",
  "用途": "用途",
  "商品/工作台": "商品/ワークベンチ",
  "创建商品，进入商品详情工作台，组织节点和运行工作流。":
    "商品を作成し、商品詳細ワークベンチでノードを整理し、ワークフローを実行します。",
  "文/图生图": "画像生成チャット",
  "围绕图片结果连续生成、改图、比较候选，并回写商品。":
    "画像結果を起点に連続生成や編集、候補比較を行い、結果を商品へ書き戻します。",
  "画廊": "ギャラリー",
  "集中保存满意的生成图，保留来源、提示词、尺寸、模型和下载入口。":
    "満足した生成画像を集約保存し、ソース、プロンプト、サイズ、モデル、ダウンロード入口を保持します。",
  "配置": "設定",
  "管理 provider、模型、尺寸、提示词模板、上传限制和密钥。":
    "プロバイダー、モデル、サイズ、プロンプトテンプレート、アップロード制限、シークレットを管理します。",
  "帮助": "ヘルプ",
  "查看当前产品内操作文档。": "現在の製品内操作ドキュメントを確認します。",
  "推荐阅读路径": "推奨読書順",
  "先阅读“快速开始”，完成一次从商品主图到生成图片的流程。":
    "まず「クイックスタート」を読み、商品メイン画像から生成画像までの流れを1回完了します。",
  "再阅读“商品工作台”“画布和节点”“模板”和“生成文案和图片”，理解画布工作台。":
    "次に「商品ワークベンチ」「キャンバスとノード」「テンプレート」「コピーと画像を生成」を読み、キャンバスワークベンチを理解します。",
  "需要收藏和复查生成图时阅读“画廊”。": "生成画像を保存・見直ししたい場合は「ギャラリー」を読みます。",
  "需要连续改图时阅读“文/图生图概览”“基图和参考图”“生成设置”和“任务与结果”。":
    "継続的に画像を編集したい場合は「画像生成チャット概要」「ベース画像と参考画像」「生成設定」「タスクと結果」を読みます。",
  "部署、配置或排障时阅读“配置概览”“模型供应商”“图片工具参数”“提示词模板”和“故障排查”。":
    "デプロイ、設定、トラブルシューティングでは「設定概要」「モデルプロバイダー」「画像ツールパラメータ」「プロンプトテンプレート」「トラブルシューティング」を読みます。",
  "快速开始": "クイックスタート",
  "用最短路径创建一个商品，选择初始画布模板，并生成第一张商品图片。":
    "最短手順で商品を作成し、初期キャンバステンプレートを選び、最初の商品画像を生成します。",
  "开始前": "始める前に",
  "后端 API、worker、PostgreSQL 和 Redis 应处于可用状态。":
    "バックエンド API、worker、PostgreSQL、Redis が利用可能な状態である必要があります。",
  "如果开启登录门禁，先使用管理员密钥登录。": "ログイン保護が有効な場合は、先に管理者キーでログインします。",
  "准备一张清楚的商品主图，推荐使用 JPG、PNG 或 WebP。":
    "鮮明な商品メイン画像を用意します。JPG、PNG、WebP を推奨します。",
  "创建商品": "商品を作成",
  "打开顶部导航中的“商品/工作台”。": "上部ナビゲーションの「商品/ワークベンチ」を開きます。",
  "点击“新建商品”。": "「新規商品」をクリックします。",
  "上传商品主图": "商品メイン画像をアップロードします。",
  "上传商品主图。": "商品メイン画像をアップロードします。",
  "填写商品名称": "商品名を入力します。",
  "填写商品名称。": "商品名を入力します。",
  "选择画布模板。首次使用推荐选择“商品主图”。":
    "キャンバステンプレートを選びます。初回は「商品メイン画像」テンプレートを推奨します。",
  "点击“创建并继续”。": "「作成して続行」をクリックします。",
  "模板可继续复用": "テンプレートは引き続き再利用できます",
  "创建商品后，商品工作台右侧“模板”面板仍可继续添加同一套场景模板；模板中的商品资料会自动连接到当前画布的商品节点。":
    "商品作成後も、商品ワークベンチ右側の「テンプレート」パネルから同じシーンテンプレートを追加できます。テンプレート内の商品データは現在のキャンバスの商品ノードへ自動接続されます。",
  "生成第一张图片": "最初の画像を生成",
  "进入商品工作台后，点击商品节点，在右侧“详情”中补充类目、价格和商品说明。":
    "商品ワークベンチに入ったら商品ノードをクリックし、右側の「詳細」でカテゴリ、価格、商品説明を補足します。",
  "选中文案节点，填写生成要求，运行当前节点。": "コピーノードを選択し、生成要件を入力して現在のノードを実行します。",
  "检查文案输出。需要时直接编辑文案结果。": "コピー出力を確認します。必要に応じてコピー結果を直接編集します。",
  "选中生图节点，确认它连接到至少一个下游参考图节点。":
    "画像生成ノードを選択し、少なくとも1つの下流参考画像ノードに接続されていることを確認します。",
  "填写图片要求，运行当前节点或运行工作流。": "画像要件を入力し、現在のノードまたはワークフローを実行します。",
  "在下游参考图节点或右侧“图库”面板查看并下载结果。":
    "下流の参考画像ノードまたは右側の「ライブラリ」パネルで結果を確認し、ダウンロードします。",
  "商品工作台": "商品ワークベンチ",
  "商品工作台是 ProductFlow 的核心操作界面。桌面端中间是画布、右侧是检查器和辅助面板；移动端保留画布为主界面。":
    "商品ワークベンチは ProductFlow の中心的な操作画面です。デスクトップでは中央にキャンバス、右側にインスペクターと補助パネルを配置し、モバイルではキャンバスを主画面として保持します。",
  "画布工作台": "キャンバスワークベンチ",
  "界面结构": "画面構成",
  "区域": "領域",
  "说明": "説明",
  "画布": "キャンバス",
  "展示商品、参考图、文案和生图节点，以及节点之间的连接线。":
    "商品、参考画像、コピー、画像生成ノードと、ノード間の接続線を表示します。",
  "详情": "詳細",
  "编辑选中节点的配置和输出。": "選択したノードの設定と出力を編集します。",
  "日志": "ログ",
  "查看工作流运行记录、失败原因和可重试运行。": "ワークフロー実行履歴、失敗理由、再試行可能な実行を確認します。",
  "图库": "ライブラリ",
  "查看商品素材、生成图和可填充到参考图节点的图片。":
    "商品素材、生成画像、参考画像ノードへ入力できる画像を確認します。",
  "模板": "テンプレート",
  "插入内置场景模板，或管理用户保存的模板。": "組み込みシーンテンプレートを挿入するか、ユーザー保存テンプレートを管理します。",
  "移动端底部工具栏": "モバイル下部ツールバー",
  "切换浏览、编辑和选择模式，并打开运行、单节点、模板、详情、日志和图库入口。":
    "閲覧、編集、選択モードを切り替え、実行、単一ノード、テンプレート、詳細、ログ、ライブラリの入口を開きます。",
  "移动端底部面板": "モバイル下部パネル",
  "在手机上承载桌面右侧面板的单节点、模板、详情、日志和图库内容。":
    "スマートフォン上で、デスクトップ右側パネルの単一ノード、テンプレート、詳細、ログ、ライブラリ内容を表示します。",
  "移动端工作台": "モバイルワークベンチ",
  "手机上进入商品详情后，画布仍是主操作区。底部模式切换提供“浏览”“编辑”“选择”：浏览用于拖动画布、点选节点和双指缩放；编辑用于拖动节点和创建连接；选择用于点按节点加入或移出多选。":
    "スマートフォンで商品詳細に入っても、キャンバスが主な操作領域です。下部のモード切替には「閲覧」「編集」「選択」があり、閲覧はキャンバスのドラッグ、ノード選択、二本指ズームに使います。編集はノード移動と接続作成に使います。選択はタップでノードを複数選択へ追加または解除します。",
  "底部工具栏还提供运行工作流、单节点、模板、详情、日志和图库入口。点开这些入口时，内容从底部面板展开，关闭后回到画布。":
    "下部ツールバーには、ワークフロー実行、単一ノード、テンプレート、詳細、ログ、ライブラリの入口もあります。入口を開くと内容が下部パネルから展開し、閉じるとキャンバスに戻ります。",
  "节点类型": "ノードタイプ",
  "节点": "ノード",
  "商品": "商品",
  "商品资料入口，保存名称、类目、价格和说明。": "商品データの入口で、名称、カテゴリ、価格、説明を保存します。",
  "参考图": "参考画像",
  "单张图片槽位，可手动上传，也可由上游生图节点填充。":
    "単一画像スロットです。手動アップロードも、上流の画像生成ノードからの入力もできます。",
  "文案": "コピー",
  "生成并编辑结构化文案：可是一段自由正文，也可以是文案块或布局分区；后续生图会读取结构化文案上下文。":
    "構造化コピーを生成・編集します。自由本文、コピーブロック、レイアウトセクションにでき、後続の画像生成は構造化コピーのコンテキストを読み取ります。",
  "生图": "画像生成",
  "触发图片生成。生成结果写入下游参考图节点，不在生图节点自身下载。":
    "画像生成をトリガーします。生成結果は下流の参考画像ノードへ書き込まれ、画像生成ノード自体からはダウンロードしません。",
  "运行模型": "実行モデル",
  "节点连接方向决定下游运行时读取哪些上下文。把 A 连到 B，表示 B 运行时可以参考 A。":
    "ノード接続の方向により、下流実行時に読み取れるコンテキストが決まります。A を B に接続すると、B の実行時に A を参照できます。",
  "运行前先保存": "実行前に保存してください",
  "工作流运行读取的是已保存内容。若“详情”中还有草稿，运行按钮会先尝试保存；如果保存失败，运行不会继续提交。":
    "ワークフロー実行は保存済みの内容を読み取ります。「詳細」に未保存の下書きがある場合、実行ボタンは先に保存を試みます。保存に失敗すると、実行は送信されません。",
  "画布和节点": "キャンバスとノード",
  "画布支持缩放、平移、节点拖拽、连接、框选和多选操作。":
    "キャンバスはズーム、パン、ノードドラッグ、接続、範囲選択、複数選択操作に対応しています。",
  "画布操作": "キャンバス操作",
  "操作": "操作",
  "桌面缩放": "デスクトップのズーム",
  "鼠标滚轮缩放画布；右下角的 - / 百分比 / + 可以缩小、重置和放大。":
    "マウスホイールでキャンバスをズームします。右下の - / パーセント / + で縮小、リセット、拡大できます。",
  "桌面平移": "デスクトップのパン",
  "不按任何快捷键时，在画布空白区域按住左键拖动。拖节点、点击按钮、上传和拖连线不会触发平移。":
    "修飾キーを押さずにキャンバスの空白領域を左クリックでドラッグします。ノードのドラッグ、ボタンクリック、アップロード、接続線のドラッグではパンしません。",
  "桌面移动单个节点": "デスクトップで単一ノードを移動",
  "直接按住节点主体或标题区域拖动，松手后位置保存。": "ノード本体またはタイトル領域をドラッグし、離すと位置が保存されます。",
  "桌面移动多个节点": "デスクトップで複数ノードを移動",
  "多选节点后，按住其中任意一个已选节点拖动，整组选中节点会一起移动。":
    "複数ノードを選択した後、選択済みノードのどれかをドラッグすると、選択グループ全体が一緒に移動します。",
  "桌面创建连线": "デスクトップで接続線を作成",
  "按住节点右侧输出连接点，拖到目标节点左侧输入连接点后松开。":
    "ノード右側の出力ハンドルを押したまま、対象ノード左側の入力ハンドルへドラッグして離します。",
  "选择单个节点": "単一ノードを選択",
  "直接点击节点主体或节点左侧输入连接点。详情面板会显示该节点。":
    "ノード本体または左側の入力ハンドルを直接クリックします。詳細パネルにそのノードが表示されます。",
  "桌面追加/移除多选": "デスクトップで複数選択へ追加/解除",
  "按住 Ctrl 点击节点；macOS 也可以按住 Cmd 点击节点。按住 Shift 点击节点同样会切换该节点的选中状态。":
    "Ctrl を押しながらノードをクリックします。macOS では Cmd クリックも使えます。Shift クリックでもそのノードの選択状態を切り替えられます。",
  "桌面框选节点": "デスクトップで範囲選択",
  "按住 Shift，从画布空白区域按住左键拖出选择框，松开后框内节点会成为当前多选组。":
    "Shift を押しながらキャンバスの空白領域から左ドラッグで選択枠を作り、離すと枠内のノードが現在の複数選択グループになります。",
  "移动端画布操作": "モバイルキャンバス操作",
  "模式": "モード",
  "浏览": "閲覧",
  "默认模式。单指拖动画布空白处会平移视野，点按节点会选中节点，双指捏合会缩放画布。":
    "既定モードです。空白キャンバスを一本指でドラッグすると表示範囲をパンし、ノードをタップすると選択し、二本指ピンチでズームします。",
  "编辑": "編集",
  "触控或触控笔可以拖动节点，也可以从节点输出连接点拖到目标节点创建连接。":
    "タッチまたはペン入力でノードをドラッグでき、ノードの出力ハンドルから対象ノードへドラッグして接続を作成できます。",
  "选择": "選択",
  "点按节点会加入或移出多选组；点按空白画布会退出临时选择模式。":
    "ノードをタップすると複数選択グループへ追加または解除されます。空白キャンバスをタップすると一時選択モードを終了します。",
  "底部工具栏": "下部ツールバー",
  "提供运行工作流、单节点、模板、详情、日志和图库入口。面板内容从底部展开。":
    "ワークフロー実行、単一ノード、テンプレート、詳細、ログ、ライブラリの入口を提供します。パネル内容は下部から展開します。",
  "多选节点": "複数ノード選択",
  "选中多个节点后，画布顶部会出现操作浮层。浮层显示已选数量，并提供“保存模板”“删除”和清空选择按钮。":
    "複数ノードを選択すると、キャンバス上部に操作バーが表示されます。選択数が表示され、「テンプレートを保存」「削除」「選択をクリア」ボタンを使えます。",
  "按住 Ctrl / Cmd / Shift 点击第一个节点，把它加入多选。":
    "Ctrl / Cmd / Shift を押しながら最初のノードをクリックし、複数選択に追加します。",
  "继续按住 Ctrl / Cmd / Shift 点击其他节点，追加或移除选中状态。":
    "Ctrl / Cmd / Shift を押したまま他のノードをクリックし、選択状態を追加または解除します。",
  "如果节点很多，按住 Shift 后从画布空白处拖出选择框，框住目标节点。":
    "ノードが多い場合は、Shift を押しながら空白キャンバスから選択枠をドラッグし、対象ノードを囲みます。",
  "多选完成后，按住任意一个已选节点拖动，可以移动整组节点。":
    "複数選択が完了したら、選択済みノードのどれかをドラッグしてグループ全体を移動できます。",
  "点击顶部浮层的“保存模板”，可以把当前多选节点保存为用户模板。":
    "上部バーの「テンプレートを保存」をクリックすると、現在の複数選択ノードをユーザーテンプレートとして保存できます。",
  "点击顶部浮层的“删除”，会删除选中节点及相关连线；点击右侧 X 可以清空多选。":
    "上部バーの「削除」をクリックすると、選択ノードと関連接続線が削除されます。右側の X をクリックすると複数選択をクリアできます。",
  "Shift 的两种用法": "Shift の2つの使い方",
  "Shift 点击节点会切换该节点的选中状态；Shift 从画布空白处拖动会触发框选。不按 Shift 从空白处拖动则是平移画布。":
    "Shift クリックはそのノードの選択状態を切り替えます。Shift を押しながら空白キャンバスからドラッグすると範囲選択になります。Shift なしで空白領域をドラッグするとキャンバスをパンします。",
  "保存为模板时，选中组不能包含商品节点。": "テンプレートとして保存する場合、選択グループに商品ノードを含めることはできません。",
  "点击画布空白处会清空多选，保留当前主选节点。": "空白キャンバスをクリックすると複数選択をクリアし、現在の主選択ノードを保持します。",
  "如果当前节点有未保存草稿，切换选择前页面会先尝试保存。":
    "現在のノードに未保存の下書きがある場合、選択を切り替える前にページが保存を試みます。",
  "模板按电商出图场景组织。创建商品时可用模板初始化画布，进入工作台后也可继续添加同一套场景模板。":
    "テンプレートは EC 画像シーン別に整理されています。商品作成時にキャンバスを初期化でき、ワークベンチ内でも同じシーンテンプレートを追加できます。",
  "模板用法": "テンプレートの使い方",
  "位置": "場所",
  "新建商品": "新規商品",
  "选择一个场景模板初始化画布。": "シーンテンプレートを1つ選んでキャンバスを初期化します。",
  "继续添加同一套内置场景模板；商品资料节点会自动复用当前画布已有节点。":
    "同じ組み込みシーンテンプレートを追加できます。商品データノードは現在のキャンバスにある既存ノードを自動的に再利用します。",
  "用户模板": "ユーザーテンプレート",
  "从当前画布多选节点后保存，用于商品工作台内追加流程。":
    "現在のキャンバスで複数選択したノードを保存し、商品ワークベンチ内でフローを追加するために使います。",
  "内置场景模板": "組み込みシーンテンプレート",
  "空白画布：只保留商品资料入口，适合自由编排。":
    "空白キャンバス：商品データ入口だけを残し、自由な構成に適しています。",
  "平台首图：电商主图、淘宝主图和白底图，用于搜索、推荐、详情首屏和平台规范素材。":
    "プラットフォーム主画像：EC メイン画像、淘宝メイン画像、白背景画像。検索、推薦、詳細ファーストビュー、プラットフォーム規定素材に使います。",
  "详情说服：SKU/变体、多角度、功能卖点、尺寸规格、尺度参照、包装清单、使用步骤、对比和细节/材质图。":
    "詳細訴求：SKU/バリエーション、多角度、機能訴求、サイズ仕様、スケール参照、同梱品、使用手順、比較、ディテール/素材画像。",
  "场景图册：模特/生活方式图和使用场景图，适合详情页图册、穿搭、家居、空间和搭配展示。":
    "シーンギャラリー：モデル/ライフスタイル画像と使用シーン画像。詳細ページの画像集、コーディネート、家具、空間、組み合わせ展示に適しています。",
  "内容种草：小红书图和短视频封面图，适合笔记封面、内容流和直播预热入口。":
    "コンテンツ訴求：小紅書画像とショート動画カバー。投稿カバー、コンテンツフィード、ライブ予告入口に適しています。",
  "活动投放：活动/促销图，适合促销位、活动入口和广告素材。":
    "キャンペーン配信：イベント/販促画像。販促枠、イベント入口、広告素材に適しています。",
  "保存用户模板": "ユーザーテンプレートを保存",
  "在画布中多选两个或更多节点：按住 Ctrl / Cmd / Shift 点击节点，或按住 Shift 从空白处框选。":
    "キャンバスで2つ以上のノードを複数選択します。Ctrl / Cmd / Shift を押しながらノードをクリックするか、Shift を押しながら空白領域から範囲選択します。",
  "确认选中节点不包含商品节点。": "選択ノードに商品ノードが含まれていないことを確認します。",
  "点击顶部多选浮层中的“保存模板”。": "上部の複数選択バーにある「テンプレートを保存」をクリックします。",
  "填写模板名称和可选描述。": "テンプレート名と任意の説明を入力します。",
  "保存后在右侧“模板”面板中查看自定义模板。": "保存後、右側の「テンプレート」パネルでカスタムテンプレートを確認します。",
  "模板不会保存产物": "テンプレートは生成物を保存しません",
  "用户模板只保存可复用配置和选中节点之间的内部连线，不保存商品资料、已生成图片或文案结果，当前不会出现在新建商品页。":
    "ユーザーテンプレートは再利用可能な設定と選択ノード間の内部接続だけを保存します。商品データ、生成済み画像、コピー結果は保存せず、現在は新規商品ページには表示されません。",
  "生成文案和图片": "コピーと画像を生成",
  "文案节点负责生成文字资产，生图节点负责触发图片生成并填充下游参考图节点。":
    "コピーノードはテキスト素材を生成し、画像生成ノードは画像生成をトリガーして下流の参考画像ノードを埋めます。",
  "生成文案": "コピーを生成",
  "选中商品节点，确认商品资料已保存。": "商品ノードを選択し、商品データが保存済みであることを確認します。",
  "选中文案节点。": "コピーノードを選択します。",
  "填写生成要求，例如目标人群、语气、重点卖点。": "ターゲット層、トーン、重要な訴求点などの生成要件を入力します。",
  "运行当前节点。": "現在のノードを実行します。",
  "运行当前节点或运行工作流。": "現在のノードまたはワークフローを実行します。",
  "检查并编辑生成后的摘要、正文、文案块或布局分区。空的可选字段默认收起，需要时再添加。":
    "生成後の要約、本文、コピーブロック、レイアウトセクションを確認・編集します。空の任意項目は既定で折りたたまれ、必要なときに追加します。",
  "文案不再强制四个字段": "コピーは4項目固定ではありません",
  "文案节点保存 CopyPayloadV2。模型可以按场景输出自由正文、短标签块、视觉建议或布局说明；后续生图直接读取结构化文案上下文。":
    "コピーノードは CopyPayloadV2 を保存します。モデルはシーンに応じて自由本文、短いタグブロック、視覚提案、レイアウト説明を出力でき、後続の画像生成は構造化コピーコンテキストを直接読み取ります。",
  "生成图片": "画像を生成",
  "选中生图节点。": "画像生成ノードを選択します。",
  "确认生图节点连接到至少一个下游参考图节点。":
    "画像生成ノードが少なくとも1つの下流参考画像ノードへ接続されていることを確認します。",
  "填写图片要求，包括主体、背景、光线、构图和用途。": "被写体、背景、光、構図、用途を含む画像要件を入力します。",
  "在下游参考图节点或“图库”面板查看结果。": "下流の参考画像ノードまたは「ライブラリ」パネルで結果を確認します。",
  "生图节点不是图片槽位": "画像生成ノードは画像スロットではありません",
  "生成图片会写入下游参考图节点。若没有下游参考图，系统会提示先连接图片/参考图节点。":
    "生成画像は下流の参考画像ノードへ書き込まれます。下流参考画像がない場合、システムは先に画像/参考画像ノードを接続するよう促します。",
  "提示词写法": "プロンプトの書き方",
  "白色托特包放在通勤桌面，旁边有笔记本电脑和咖啡，干净自然光，商品主体完整，纹理清晰，适合电商主图。":
    "白いトートバッグを通勤用デスクに置き、横にノートPCとコーヒーを配置。清潔な自然光、商品全体が見え、質感が明瞭で、ECメイン画像に適した構図。",
  "每轮只改一两个因素，例如背景、构图、光线或主体细节。一次改太多会很难判断哪句话影响了结果。":
    "各ラウンドでは背景、構図、光、主体ディテールなど1〜2要素だけを変えるのがおすすめです。一度に変えすぎると、どの文が結果に影響したのか判断しにくくなります。",
  "文图生图概览": "画像生成チャット概要",
  "文/图生图概览": "画像生成チャット概要",
  "文/图生图用于独立图片会话、连续改图、多候选比较、参考图控制，以及把结果回写商品或投至画廊。":
    "画像生成チャットは、独立した画像セッション、継続的な画像編集、複数候補の比較、参考画像制御、結果の商品への書き戻しやギャラリー投入に使います。",
  "页面结构": "ページ構成",
  "桌面左侧会话列表": "デスクトップ左側のセッション一覧",
  "新建、选择、重命名或删除文/图生图会话。每个会话保留自己的历史、参考图和生成任务。":
    "画像生成チャットセッションを新規作成、選択、名前変更、削除します。各セッションは自分の履歴、参考画像、生成タスクを保持します。",
  "桌面中间结果区": "デスクトップ中央の結果領域",
  "展示当前选中的生成候选、生成中占位、失败状态、下载按钮和“投至画廊”按钮。":
    "現在選択中の生成候補、生成中プレースホルダー、失敗状態、ダウンロードボタン、「ギャラリーへ送る」ボタンを表示します。",
  "桌面底部历史记录": "デスクトップ下部の履歴",
  "按分支展示历史结果。点击已完成图片会把它选为当前结果，并作为下一轮基图。":
    "履歴結果をブランチ別に表示します。完了画像をクリックすると現在結果として選択され、次ラウンドのベース画像になります。",
  "桌面右侧生成设置": "デスクトップ右側の生成設定",
  "管理关联商品、保存到商品、会话参考图、画面描述、尺寸、候选数量和高级图片工具参数。":
    "関連商品、商品への保存、セッション参考画像、画像説明、サイズ、候補数、高度な画像ツールパラメータを管理します。",
  "移动端顶部栏": "モバイル上部バー",
  "左侧按钮打开会话抽屉，中间显示当前会话标题；铅笔按钮用于重命名，右侧按钮打开历史抽屉。":
    "左ボタンでセッションドロワーを開き、中央に現在のセッション名を表示します。鉛筆ボタンは名前変更、右ボタンは履歴ドロワーを開きます。",
  "移动端左侧会话抽屉": "モバイル左側セッションドロワー",
  "新建、选择和删除会话；会话卡片显示最近缩略图、轮数和更新时间。":
    "セッションの新規作成、選択、削除を行います。セッションカードには最新サムネイル、ラウンド数、更新時刻が表示されます。",
  "移动端右侧历史抽屉": "モバイル右側履歴ドロワー",
  "以窄抽屉展示分支和候选。点已完成图片会选为当前结果和下一轮基图，点占位会查看该候选状态。":
    "細いドロワーでブランチと候補を表示します。完了画像をタップすると現在結果と次ラウンドのベース画像として選択され、プレースホルダーをタップするとその候補状態を確認できます。",
  "移动端底部快捷条": "モバイル下部ショートカットバー",
  "始终提供生成入口；选中已完成结果后，还提供下载和投至画廊。":
    "生成入口を常に表示します。完了結果を選択すると、ダウンロードとギャラリー投入も表示されます。",
  "移动端底部生成面板": "モバイル下部生成パネル",
  "用生成设置/高级标签页管理商品关联、商品/会话参考图、画面描述、尺寸、候选数量和图片工具参数。":
    "生成設定/高度タブで商品関連付け、商品/セッション参考画像、画像説明、サイズ、候補数、画像ツールパラメータを管理します。",
  "创建和选择会话": "セッションの作成と選択",
  "打开顶部导航中的“文/图生图”。": "上部ナビゲーションの「画像生成チャット」を開きます。",
  "桌面端点击左侧会话区域的“新建”按钮；移动端点击顶部左侧菜单，在会话抽屉中点击加号创建会话。":
    "デスクトップでは左側セッション領域の「新規」ボタンをクリックします。モバイルでは上部左メニューをタップし、セッションドロワー内のプラスボタンでセッションを作成します。",
  "如果从商品详情进入，页面会进入商品关联模式；如果从全局入口进入，可以先自由生成，也可以在右侧选择目标商品。":
    "商品詳細から入った場合、ページは商品関連付けモードになります。全体入口から入った場合は、先に自由生成することも、右側で対象商品を選ぶこともできます。",
  "点击会话卡片切换会话。卡片会显示最近结果缩略图、轮数和更新时间；移动端选择后抽屉会关闭并回到主视图。":
    "セッションカードをクリックしてセッションを切り替えます。カードには最新結果のサムネイル、ラウンド数、更新時刻が表示されます。モバイルでは選択後にドロワーが閉じてメインビューに戻ります。",
  "需要改会话名时，桌面端在右侧“生成设置”点击“重命名”；移动端点击顶部栏的铅笔，输入名称后点击保存按钮。":
    "セッション名を変更する場合、デスクトップでは右側の「生成設定」で「名前変更」をクリックします。モバイルでは上部バーの鉛筆をタップし、名前を入力して保存ボタンを押します。",
  "删除会话使用会话卡片上的删除按钮；如果配置关闭了业务删除，按钮会禁用并提示当前不可删除。":
    "セッション削除はセッションカード上の削除ボタンを使います。設定で業務削除が無効な場合、ボタンは無効化され、現在削除できないことが表示されます。",
  "会话和商品不是同一个对象": "セッションと商品は同じオブジェクトではありません",
  "文/图生图会话可以关联商品，也可以自由生成。只有点击“加入参考图”“保存为参考图”或“设为商品主图参考”这类保存按钮时，当前候选才会写回商品素材库。":
    "画像生成チャットセッションは商品に関連付けることも、自由生成することもできます。現在の候補が商品素材庫へ書き戻されるのは、「参考画像に追加」「参考画像として保存」「商品メイン画像参考に設定」などの保存ボタンをクリックした場合だけです。",
  "基图和参考图": "ベース画像と参考画像",
  "说明文/图生图里基图、会话参考图、商品参考图的区别，以及单轮图片上下文数量限制。":
    "画像生成チャットにおけるベース画像、セッション参考画像、商品参考画像の違いと、1ラウンドの画像コンテキスト数制限を説明します。",
  "基图和参考图的区别": "ベース画像と参考画像の違い",
  "文/图生图每一轮可以同时使用“基图”和“参考图”。基图来自历史记录中选中的已完成图片，用于表达“在这张图基础上继续改”。参考图来自会话参考图或商品参考图，用于补充风格、材质、姿态、背景等上下文。":
    "画像生成チャットの各ラウンドでは「ベース画像」と「参考画像」を同時に使用できます。ベース画像は履歴で選択した完了画像で、「この画像をもとに続けて編集する」ことを表します。参考画像はセッション参考画像または商品参考画像から来て、スタイル、素材、ポーズ、背景などのコンテキストを補足します。",
  "第一轮没有历史图时，直接在“画面描述”里写想生成的画面。":
    "最初のラウンドで履歴画像がない場合は、「画像説明」に生成したい画面を直接書きます。",
  "生成完成后，在底部历史记录点击一张已完成图片；移动端从右侧历史抽屉点选。中间结果区会显示“已选基图”。":
    "生成完了後、下部履歴で完了画像をクリックします。モバイルでは右側履歴ドロワーから選択します。中央結果領域に「ベース画像を選択済み」と表示されます。",
  "如需更多视觉参考，在右侧“会话参考图”上传图片，或从商品参考图区域选择已有素材；移动端在底部生成面板的生成设置标签页操作这些区域。":
    "より多くの視覚参考が必要な場合は、右側の「セッション参考画像」に画像をアップロードするか、商品参考画像領域から既存素材を選びます。モバイルでは下部生成パネルの生成設定タブで操作します。",
  "勾选参考图后再提交生成。系统会把基图和已选参考图一起作为本轮上下文。":
    "参考画像にチェックを入れてから生成を送信します。システムはベース画像と選択済み参考画像をこのラウンドのコンテキストとしてまとめて使います。",
  "图片上下文数量": "画像コンテキスト数",
  "单轮最多 6 张": "1ラウンド最大6枚",
  "单轮最多选择 6 张图片上下文，这个数量包含历史基图和显式选择的参考图。如果已经选了基图，最多还能再选 5 张参考图。":
    "1ラウンドで選択できる画像コンテキストは最大6枚です。この数には履歴ベース画像と明示的に選択した参考画像が含まれます。ベース画像を選択済みの場合、追加できる参考画像は最大5枚です。",
  "想保留主体角度时，优先选择历史结果作为基图。": "主体の角度を保ちたい場合は、履歴結果をベース画像として優先的に選びます。",
  "想补充材质、风格、背景或姿态时，选择会话参考图或商品参考图。":
    "素材、スタイル、背景、ポーズを補足したい場合は、セッション参考画像または商品参考画像を選びます。",
  "如果结果偏离太多，减少参考图数量通常比继续堆参考图更容易定位问题。":
    "結果が大きくずれる場合、参考画像を増やし続けるより枚数を減らした方が原因を特定しやすくなります。",
  "生成设置": "生成設定",
  "说明画面描述、尺寸、候选数量和高级图片工具参数如何影响文/图生图任务。":
    "画像説明、サイズ、候補数、高度な画像ツールパラメータが画像生成チャットタスクにどう影響するかを説明します。",
  "字段说明": "項目説明",
  "设置": "設定",
  "商品关联": "商品関連付け",
  "商品详情进入时自动关联当前商品；全局入口进入时，可在生成设置里选择目标商品，之后才能把结果保存为商品参考图。":
    "商品詳細から入ると現在の商品に自動関連付けされます。全体入口から入る場合は、生成設定で対象商品を選ぶと、その後で結果を商品参考画像として保存できます。",
  "商品参考图": "商品参考画像",
  "显示目标商品已有参考图和主图参考；选中已完成候选后，可把候选加入参考图或设为商品主图参考。":
    "対象商品の既存参考画像とメイン画像参考を表示します。完了候補を選択すると、候補を参考画像に追加したり、商品メイン画像参考に設定したりできます。",
  "会话参考图": "セッション参考画像",
  "上传本会话可复用的参考图，并勾选参与本轮生成。单轮图片上下文数量仍受 6 张限制。":
    "このセッションで再利用できる参考画像をアップロードし、このラウンドの生成に参加させるものをチェックします。1ラウンドの画像コンテキスト数は引き続き6枚に制限されます。",
  "画面描述": "画像説明",
  "本轮真正提交给生成任务的用户要求。写清主体、保留项、变化项、背景、构图、光线和用途。":
    "このラウンドで生成タスクに実際に送信されるユーザー要件です。主体、保持する要素、変更点、背景、構図、光、用途を明確に書きます。",
  "尺寸": "サイズ",
  "选择常用 1K / 2K / 4K 预设，或输入自定义宽高。提交前会按后端最大单边限制校准。":
    "よく使う 1K / 2K / 4K プリセットを選ぶか、カスタム幅・高さを入力します。送信前にバックエンドの最大単辺制限に合わせて補正されます。",
  "候选数量": "候補数",
  "决定本轮创建多少张候选。多候选会在历史记录中显示多个占位，完成后分别替换为结果。":
    "このラウンドで作成する候補数を決めます。複数候補の場合、履歴に複数のプレースホルダーが表示され、完了後にそれぞれ結果へ置き換わります。",
  "生成设置 / 高级标签页": "生成設定 / 高度タブ",
  "生成设置包含商品、参考图、描述、尺寸和候选数量；高级包含供应商图片工具参数。":
    "生成設定には商品、参考画像、説明、サイズ、候補数が含まれ、高度タブにはサプライヤー画像ツールパラメータが含まれます。",
  "图片工具参数": "画像ツールパラメータ",
  "只显示配置页“可用 Tool 字段”中启用的字段，例如质量、格式、背景、输入保真度等。未启用字段不会提交。":
    "設定ページの「利用可能な Tool 項目」で有効化された項目だけを表示します。例：品質、形式、背景、入力忠実度など。無効な項目は送信されません。",
  "提交按钮": "送信ボタン",
  "桌面端在右侧设置底部，移动端在底部生成面板底部。按钮文案会按候选数量显示本轮要提交的数量。":
    "デスクトップでは右側設定の下部、モバイルでは下部生成パネルの下部にあります。ボタン文言は候補数に応じて、このラウンドで送信する数量を表示します。",
  "连续改图写法": "連続画像編集の書き方",
  "保持包的角度不变，背景换成更明亮的办公室。减少桌面杂物，只保留电脑和咖啡；包身纹理要清晰，阴影柔和。":
    "バッグの角度は変えず、背景をより明るいオフィスに変更。デスク上の小物を減らし、PC とコーヒーだけを残す。バッグ表面の質感は明瞭に、影は柔らかく。",
  "连续改图时，建议明确写“保持什么不变”和“只修改什么”。如果只写一个很宽泛的新描述，模型可能会把它当成重新生成，而不是局部调整。":
    "連続して画像を編集する場合は、「何を変えないか」と「何だけを変更するか」を明確に書くことをおすすめします。広すぎる新しい説明だけを書くと、モデルが局所調整ではなく再生成として扱う可能性があります。",
  "任务与结果": "タスクと結果",
  "说明文/图生图任务状态、重试、取消、下载、投至画廊和保存回商品的规则。":
    "画像生成チャットタスクの状態、再試行、キャンセル、ダウンロード、ギャラリー投入、商品への保存ルールを説明します。",
  "运行状态、重试和取消": "実行状態、再試行、キャンセル",
  "状态": "状態",
  "页面表现": "ページ表示",
  "排队中": "キュー待ち",
  "中间结果区和历史记录会显示占位，可能显示队列位置、前方任务数和全局活跃数量。":
    "中央結果領域と履歴にプレースホルダーが表示され、キュー位置、前方タスク数、全体のアクティブ数が表示される場合があります。",
  "生成中": "生成中",
  "占位会显示当前候选序号、候选总数、最近进度和供应商状态。":
    "プレースホルダーには現在の候補番号、候補総数、最新進捗、サプライヤー状態が表示されます。",
  "生成完成": "生成完了",
  "占位替换为真实候选图，页面提示“新候选已生成”。":
    "プレースホルダーが実際の候補画像に置き換わり、ページに「新しい候補が生成されました」と表示されます。",
  "失败": "失敗",
  "显示失败原因；如果任务可重试，会出现“重试生成”。": "失敗理由を表示します。タスクが再試行可能な場合は「生成を再試行」が表示されます。",
  "已取消": "キャンセル済み",
  "显示任务已取消，不再写入新的候选结果。": "タスクがキャンセル済みであることを表示し、新しい候補結果は書き込まれません。",
  "运行中的任务可点击“取消生成”。": "実行中のタスクは「生成をキャンセル」をクリックできます。",
  "失败且可重试的任务可点击“重试生成”。重试复用原任务的提示词、尺寸、参考图和高级参数。":
    "失敗して再試行可能なタスクは「生成を再試行」をクリックできます。再試行では元タスクのプロンプト、サイズ、参考画像、高度パラメータを再利用します。",
  "如果你已经修改了画面描述、尺寸或参考图，应提交新一轮生成，而不是重试旧失败任务。":
    "画像説明、サイズ、参考画像を変更済みの場合は、古い失敗タスクを再試行せず、新しいラウンドの生成を送信してください。",
  "页面运行中只轮询轻量状态，任务结束后再刷新完整会话详情。":
    "ページ実行中は軽量状態だけをポーリングし、タスク終了後に完全なセッション詳細を再取得します。",
  "保存结果": "結果を保存",
  "结果": "結果",
  "下载": "ダウンロード",
  "下载当前选中候选的原图。": "現在選択中の候補の元画像をダウンロードします。",
  "投至画廊": "ギャラリーへ送る",
  "把当前候选保存到全局画廊，保留来源会话、商品、提示词、尺寸、模型和下载入口。":
    "現在の候補を全体ギャラリーに保存し、ソースセッション、商品、プロンプト、サイズ、モデル、ダウンロード入口を保持します。",
  "加入参考图 / 保存为参考图": "参考画像に追加 / 参考画像として保存",
  "把当前候选写入目标商品的参考图素材，之后商品工作台和文/图生图都可以继续引用。":
    "現在の候補を対象商品の参考画像素材へ書き込み、その後は商品ワークベンチと画像生成チャットの両方で引き続き参照できます。",
  "设为商品主图参考": "商品メイン画像参考に設定",
  "把当前候选保存为商品主图参考素材，用于后续商品素材链路。":
    "現在の候補を商品メイン画像参考素材として保存し、後続の商品素材フローに使います。",
  "保存动作需要先选中候选": "保存操作には候補選択が必要です",
  "只有中间结果区显示已完成图片时，下载、投至画廊和保存到商品才有明确目标。选中生成中占位或没有结果时，这些动作不会提交。":
    "中央結果領域に完了画像が表示されている場合だけ、ダウンロード、ギャラリー投入、商品保存の明確な対象があります。生成中プレースホルダーを選択している場合や結果がない場合、これらの操作は送信されません。",
  "移动端布局": "モバイルレイアウト",
  "手机上显示的行为": "スマートフォン上の動作",
  "手机上的行为": "スマートフォン上の動作",
  "顶部栏": "上部バー",
  "左侧按钮打开会话抽屉，中间显示当前会话标题；铅笔进入重命名，右侧历史按钮打开窄历史抽屉。":
    "左ボタンでセッションドロワーを開き、中央に現在のセッション名を表示します。鉛筆で名前変更に入り、右側の履歴ボタンで細い履歴ドロワーを開きます。",
  "主视图": "メインビュー",
  "生成状态、当前结果、失败原因和供应商提示保留可见。点当前结果可打开预览。":
    "生成状態、現在結果、失敗理由、サプライヤーメッセージは見える状態で保持されます。現在結果をタップするとプレビューを開けます。",
  "右侧历史抽屉": "右側履歴ドロワー",
  "显示分支、候选和生成中占位。多候选提交后会先出现对应数量的占位；任务结束后刷新为真实候选或失败/取消状态。":
    "ブランチ、候補、生成中プレースホルダーを表示します。複数候補を送信すると、対応する数のプレースホルダーが先に表示され、タスク終了後に実際の候補または失敗/キャンセル状態へ更新されます。",
  "底部快捷条": "下部ショートカットバー",
  "生成入口一直可用。选中已完成图片后，快捷条增加下载和投至画廊。":
    "生成入口は常に利用できます。完了画像を選択すると、ショートカットバーにダウンロードとギャラリー投入が追加されます。",
  "底部生成面板": "下部生成パネル",
  "生成设置标签页管理商品关联、商品参考图、会话参考图、画面描述、尺寸和候选数量；高级标签页管理图片工具参数。面板底部按钮提交本轮生成。":
    "生成設定タブで商品関連付け、商品参考画像、セッション参考画像、画像説明、サイズ、候補数を管理し、高度タブで画像ツールパラメータを管理します。パネル下部のボタンでこのラウンドの生成を送信します。",
  "画廊用于收藏满意的文/图生图结果，方便集中浏览和下载。":
    "ギャラリーは満足した画像生成チャット結果を保存し、まとめて閲覧・ダウンロードするために使います。",
  "保存到画廊": "ギャラリーへ保存",
  "文/图生图结果可以保存到画廊。画廊条目保留来源会话、关联商品、提示词、尺寸、模型和下载入口。":
    "画像生成チャットの結果はギャラリーに保存できます。ギャラリー項目はソースセッション、関連商品、プロンプト、サイズ、モデル、ダウンロード入口を保持します。",
  "适合保存暂时不挂回商品、但以后可能复用的背景或构图。":
    "今すぐ商品に紐づけないが、後で再利用する可能性がある背景や構図の保存に適しています。",
  "适合保存需要集中给别人挑选的候选图。": "他の人にまとめて選んでもらう候補画像の保存に適しています。",
  "适合保存调参过程中效果不错但不是当前最终稿的图片。":
    "パラメータ調整中に良い結果だが現在の最終稿ではない画像の保存に適しています。",
  "配置概览": "設定概要",
  "配置页用于管理运行时业务配置。基础设施配置仍由环境变量控制，不在设置页覆盖。":
    "設定ページは実行時の業務設定を管理します。インフラ設定は引き続き環境変数で制御され、設定ページでは上書きしません。",
  "访问和保存规则": "アクセスと保存ルール",
  "配置页需要先登录；如果设置页要求二次解锁，还需要输入 `SETTINGS_ACCESS_TOKEN`。":
    "設定ページは先にログインが必要です。設定ページが二次ロック解除を要求する場合は、`SETTINGS_ACCESS_TOKEN` も入力します。",
  "配置项会显示来源。数据库覆盖值会标记为数据库来源；未覆盖时使用 env/default。":
    "各設定項目にはソースが表示されます。データベース上書き値はデータベースソースとして表示され、未上書きの場合は env/default を使います。",
  "只提交发生变化的字段。密钥字段留空不会覆盖已有值。":
    "変更された項目だけを送信します。シークレット項目を空のままにしても既存値は上書きされません。",
  "点击恢复默认会删除数据库覆盖值，让该字段回到 env/default。":
    "既定値へ戻すをクリックするとデータベース上書き値が削除され、その項目は env/default に戻ります。",
  "Env-only 配置": "Env-only 設定",
  "`DATABASE_URL`、`REDIS_URL`、`SESSION_SECRET`、`ADMIN_ACCESS_KEY` 等基础设施配置不支持设置页覆盖。":
    "`DATABASE_URL`、`REDIS_URL`、`SESSION_SECRET`、`ADMIN_ACCESS_KEY` などのインフラ設定は、設定ページでの上書きに対応していません。",
  "设置页二次解锁由 `SETTINGS_ACCESS_TOKEN` 保护。": "設定ページの二次ロック解除は `SETTINGS_ACCESS_TOKEN` で保護されます。",
  "关闭登录门禁不会关闭设置页二次解锁。": "ログイン保護を無効にしても、設定ページの二次ロック解除は無効になりません。",
  "模型供应商": "モデルプロバイダー",
  "说明供应商档案、文案/图片用途绑定、模型和图片生成基础参数。":
    "プロバイダープロファイル、コピー/画像用途バインディング、モデル、画像生成の基本パラメータを説明します。",
  "字段": "項目",
  "供应商档案": "プロバイダープロファイル",
  "保存供应商类型、连接信息、API Key 和能力。Google Gemini 使用官方 SDK endpoint，不配置 Base URL；密钥不会回显，编辑档案时留空 API Key 会保留旧值。":
    "プロバイダータイプ、接続情報、API Key、能力を保存します。Google Gemini は公式 SDK endpoint を使い、Base URL は設定しません。シークレットは返されず、プロファイル編集時に API Key を空欄にすると旧値を保持します。",
  "文案用途绑定": "コピー用途バインディング",
  "选择 `mock` 或真实 OpenAI Responses 兼容接口，并选择具备文案能力的供应商档案。":
    "`mock` または実際の OpenAI Responses 互換インターフェースを選択し、コピー能力を持つプロバイダープロファイルを選びます。",
  "商品理解模型": "商品理解モデル",
  "用于把商品名称、类目、价格、说明等整理成 CreativeBrief。":
    "商品名、カテゴリ、価格、説明などを CreativeBrief に整理するために使います。",
  "文案生成模型": "コピー生成モデル",
  "文案生成": "コピー生成",
  "用于生成 CopyPayloadV2 结构化文案，可包含自由正文、文案块、布局分区和视觉建议。":
    "CopyPayloadV2 の構造化コピーを生成するために使います。自由本文、コピーブロック、レイアウトセクション、視覚提案を含められます。",
  "OpenAI 兼容档案可以同时声明文案、Responses 图片和 Images API 图片能力；Google Gemini 档案只声明 Gemini 图片能力。":
    "OpenAI 互換プロファイルはコピー、Responses 画像、Images API 画像能力を同時に宣言できます。Google Gemini プロファイルは Gemini 画像能力だけを宣言します。",
  "图片用途绑定": "画像用途バインディング",
  "选择 `mock`、OpenAI Responses、OpenAI Images API 或 Google Gemini Image，并选择具备对应图片能力的供应商档案。":
    "`mock`、OpenAI Responses、OpenAI Images API、Google Gemini Image を選択し、対応する画像能力を持つプロバイダープロファイルを選びます。",
  "图片模型": "画像モデル",
  "图片生成": "画像生成",
  "发送给图片 provider 的默认图片模型。Responses、Images API 与 Gemini 支持范围不同。":
    "画像プロバイダーへ送信する既定の画像モデルです。Responses、Images API、Gemini で対応範囲が異なります。",
  "Responses 后台响应模式": "Responses バックグラウンド応答モード",
  "只属于 OpenAI Responses 图片绑定。开启后长任务先拿到 response_id 再轮询状态；如果网关明确不支持，会按同步请求重试。":
    "OpenAI Responses 画像バインディングだけに属します。有効にすると長時間タスクは先に response_id を取得してから状態をポーリングします。ゲートウェイが明確に非対応の場合は同期リクエストとして再試行します。",
  "Images API Quality / Style": "Images API Quality / Style",
  "只属于 OpenAI Images API 图片绑定。兼容网关不支持可选字段时会按基础参数重试。":
    "OpenAI Images API 画像バインディングだけに属します。互換ゲートウェイが任意項目に非対応の場合は、基本パラメータで再試行します。",
  "Gemini API 版本 / 输出 MIME": "Gemini API バージョン / 出力 MIME",
  "只属于 Google Gemini 图片绑定。API 版本默认 `v1beta`，输出 MIME 留空时使用供应商默认值。":
    "Google Gemini 画像バインディングだけに属します。API バージョンは既定で `v1beta`、出力 MIME を空欄にするとプロバイダー既定値を使います。",
  "生图最大单边": "画像生成の最大単辺",
  "工作台生图和文/图生图的最大宽/高像素。最大面积同步使用该值平方。":
    "ワークベンチ画像生成と画像生成チャットの最大幅/高さピクセルです。最大面積もこの値の二乗を使います。",
  "主图尺寸（兼容默认）": "メイン画像サイズ（互換既定値）",
  "高级兼容值。只有当 provider 输入未明确传入 image_size 且类型为主图时才使用。新工作流优先看节点里的尺寸选择器。":
    "高度な互換値です。プロバイダー入力で image_size が明示されず、種類がメイン画像の場合だけ使います。新しいワークフローではノード内のサイズ選択を優先します。",
  "促销海报尺寸（兼容默认）": "販促ポスターサイズ（互換既定値）",
  "高级兼容值。只有当 provider 输入未明确传入 image_size 且类型为促销海报时才使用。":
    "高度な互換値です。プロバイダー入力で image_size が明示されず、種類が販促ポスターの場合だけ使います。",
  "海报生成模式": "ポスター生成モード",
  "`模板渲染` 不消耗图片模型；`AI 生成` 会调用图片供应商。":
    "`テンプレートレンダリング` は画像モデルを消費しません。`AI 生成` は画像プロバイダーを呼び出します。",
  "海报字体路径": "ポスターフォントパス",
  "模板海报和 mock 图片中用于中文文字渲染的字体文件。":
    "テンプレートポスターと mock 画像で中国語文字を描画するためのフォントファイルです。",
  "说明 Responses 图片工具高级字段的含义，以及它们和前端可见控件、后端持久化的关系。":
    "Responses image_generation tool の高度項目の意味と、それらがフロントエンドの表示コントロールやバックエンド永続化とどう関係するかを説明します。",
  "tool_settings": "tool_settings",
  "图片工具参数是发送给 Responses `image_generation` tool 的高级字段。配置页的“可用 Tool 字段”决定前端哪些高级控件可见，也决定后端哪些字段可以持久化并发送给 provider。":
    "画像ツールパラメータは Responses `image_generation` tool に送信される高度項目です。設定ページの「利用可能な Tool 項目」は、フロントエンドに表示される高度コントロールと、バックエンドが永続化してプロバイダーへ送信できる項目を決めます。",
  "可用 Tool 字段": "利用可能な Tool 項目",
  "多选字段。未勾选的高级字段不会在前端显示，也不会发送给 provider。":
    "複数選択項目です。未選択の高度項目はフロントエンドに表示されず、プロバイダーにも送信されません。",
  "Tool 模型": "Tool モデル",
  "发送到 image_generation tool 内部的模型字段。留空不发送，需要 provider 支持。":
    "image_generation tool 内部へ送信するモデル項目です。空欄の場合は送信せず、プロバイダー側の対応が必要です。",
  "质量": "品質",
  "可选默认、Auto、Low、Medium、High。用于支持质量参数的 provider。":
    "既定、Auto、Low、Medium、High から選択できます。品質パラメータに対応するプロバイダーで使います。",
  "格式": "形式",
  "可选默认、PNG、JPEG、WebP。影响 provider 输出格式。":
    "既定、PNG、JPEG、WebP から選択できます。プロバイダー出力形式に影響します。",
  "压缩": "圧縮",
  "0-100；留空不发送。通常只对 JPEG/WebP 等格式有意义。":
    "0〜100。空欄の場合は送信しません。通常は JPEG/WebP などの形式でのみ意味があります。",
  "背景": "背景",
  "可选默认、Auto、Opaque、Transparent。仅在可用 Tool 字段勾选 background 后发送。":
    "既定、Auto、Opaque、Transparent から選択できます。利用可能な Tool 項目で background が選択されている場合だけ送信されます。",
  "审核": "モデレーション",
  "可选默认、Auto、Low。是否生效取决于 provider 支持。":
    "既定、Auto、Low から選択できます。効果はプロバイダー対応に依存します。",
  "Action": "Action",
  "可选默认、Auto、Generate、Edit。用于提示 provider 当前更像生成还是编辑。":
    "既定、Auto、Generate、Edit から選択できます。現在のタスクが生成寄りか編集寄りかをプロバイダーに示します。",
  "Input fidelity": "Input fidelity",
  "可选默认、Low、High。用于控制输入参考图保真度，需 provider 支持。":
    "既定、Low、High から選択できます。入力参考画像の忠実度を制御するために使い、プロバイダー対応が必要です。",
  "Partial": "Partial",
  "0-3；留空不发送。用于支持 partial images 的 provider。":
    "0〜3。空欄の場合は送信しません。partial images に対応するプロバイダーで使います。",
  "Provider n": "Provider n",
  "高级 provider 字段，不改变 ProductFlow 文/图生图“候选数量”的产品语义。":
    "高度なプロバイダー項目です。ProductFlow の画像生成チャットにおける「候補数」のプロダクト上の意味は変わりません。",
  "候选数量和 Provider n 不等价": "候補数と Provider n は同等ではありません",
  "文/图生图右侧的“候选数量”会创建 ProductFlow 自己的候选任务语义；`Provider n` 是透传给 provider 的高级字段，默认不应把它当成页面候选数量来用。":
    "画像生成チャット右側の「候補数」は ProductFlow 独自の候補タスク意味を作ります。`Provider n` はプロバイダーへ透過する高度項目であり、既定ではページの候補数として扱うべきではありません。",
  "提示词模板": "プロンプトテンプレート",
  "说明全局提示词模板负责哪些默认行为，以及哪些要求应该留在单次节点或文/图生图输入里。":
    "グローバルプロンプトテンプレートがどの既定動作を制御し、どの要件を単発ノードや画像生成チャット入力に残すべきかを説明します。",
  "商品理解系统提示词": "商品理解システムプロンプト",
  "用于商品资料理解，要求模型输出 CreativeBrief JSON。":
    "商品データ理解に使い、モデルに CreativeBrief JSON の出力を要求します。",
  "文案生成系统提示词": "コピー生成システムプロンプト",
  "用于主图/海报文案生成，要求模型输出 CopyPayloadV2 JSON；后端会兼容常见的自由文案、块状文案和布局说明变体。":
    "メイン画像/ポスターコピー生成に使い、モデルに CopyPayloadV2 JSON の出力を要求します。バックエンドは一般的な自由コピー、ブロックコピー、レイアウト説明のバリエーションに対応します。",
  "海报生图提示词模板": "ポスター画像生成プロンプトテンプレート",
  "用于工作台 AI 生图。常用占位符包括 `instruction`、`size`、`context_block`、`reference_policy`、`kind` 等。":
    "ワークベンチ AI 画像生成に使います。よく使うプレースホルダーには `instruction`、`size`、`context_block`、`reference_policy`、`kind` などがあります。",
  "图片改图提示词模板": "画像編集プロンプトテンプレート",
  "用于工作台参考图/生成图继续生图。适合带上游文案或参考图上下文的场景。":
    "ワークベンチ参考画像/生成画像から続けて画像生成するために使います。上流コピーや参考画像コンテキストを持つシーンに適しています。",
  "工作台视觉参考规则": "ワークベンチ視覚参考ルール",
  "填入工作台生图模板的 `reference_policy` 占位符，用于控制视觉参考优先级规则。":
    "ワークベンチ画像生成テンプレートの `reference_policy` プレースホルダーに入り、視覚参考の優先順位ルールを制御します。",
  "文/图生图提示词模板": "画像生成チャットプロンプトテンプレート",
  "用于文/图生图对话。可用占位符：`prompt`、`size`、`history_block`。":
    "画像生成チャットに使います。利用可能なプレースホルダー：`prompt`、`size`、`history_block`。",
  "单次要求不要写进全局模板": "単発要件はグローバルテンプレートに入れない",
  "如果只是这一次想要某种背景、构图或语气，应写在节点要求或文/图生图的画面描述里。提示词模板适合长期默认行为。":
    "今回だけ特定の背景、構図、トーンが必要な場合は、ノード要件または画像生成チャットの画像説明に書いてください。プロンプトテンプレートは長期的な既定動作に適しています。",
  "上传、队列与安全": "アップロード、キュー、安全",
  "说明上传限制、生成并发、任务恢复、provider 超时和安全开关这些运维类配置。":
    "アップロード制限、生成並行数、タスク復旧、プロバイダータイムアウト、安全スイッチなどの運用系設定を説明します。",
  "上传、队列和恢复": "アップロード、キュー、復旧",
  "单图最大字节数": "画像1枚あたりの最大バイト数",
  "限制单张上传图片大小。": "アップロード画像1枚のサイズを制限します。",
  "最多参考图数量": "最大参考画像数",
  "限制参考图数量，文/图生图单轮上下文还会受到 6 张图片上下文限制。":
    "参考画像数を制限します。画像生成チャットの1ラウンドコンテキストも6枚の画像コンテキスト制限を受けます。",
  "最大像素数": "最大ピクセル数",
  "限制上传图片的像素面积。": "アップロード画像のピクセル面積を制限します。",
  "允许图片 MIME": "許可する画像 MIME",
  "逗号分隔，例如 `image/png,image/jpeg,image/webp`。":
    "カンマ区切りです。例：`image/png,image/jpeg,image/webp`。",
  "全局生成并发上限": "全体生成並行数上限",
  "工作流和文/图生图共享的资源保护阈值。达到上限时页面会提示稍后重试。":
    "ワークフローと画像生成チャットで共有するリソース保護しきい値です。上限に達すると、ページは後で再試行するよう表示します。",
  "文/图生图进度闲置恢复阈值": "画像生成チャット進捗アイドル復旧しきい値",
  "worker 启动恢复时，running 文/图生图任务会按最近 progress heartbeat 判断是否闲置。":
    "worker 起動時の復旧では、running の画像生成チャットタスクが直近の progress heartbeat によりアイドルかどうか判定されます。",
  "工作流生图 Provider 超时": "ワークフロー画像生成 Provider タイムアウト",
  "工作流 AI 生图节点单次 provider 调用的项目级超时上界。超时后任务安全失败并释放队列容量。":
    "ワークフロー AI 画像生成ノードにおける1回の provider 呼び出しのプロジェクトレベルのタイムアウト上限です。タイムアウト後、タスクは安全に失敗し、キュー容量を解放します。",
  "安全与运维": "安全と運用",
  "密钥字段不会在 API 响应和页面中回显。留空保存不会覆盖已有密钥；只有输入新值才会写入数据库覆盖。":
    "シークレット項目は API レスポンスやページに表示されません。空欄で保存しても既存シークレットは上書きされず、新しい値を入力した場合だけデータベース上書きが書き込まれます。",
  "要求登录访问密钥": "ログインアクセスキーを要求",
  "默认开启。普通工作台和私有 API 需要 `ADMIN_ACCESS_KEY` 登录；关闭后仍需 `SETTINGS_ACCESS_TOKEN` 才能查看和修改系统配置。":
    "既定で有効です。通常のワークベンチとプライベート API は `ADMIN_ACCESS_KEY` ログインが必要です。無効化しても、システム設定の閲覧・変更には `SETTINGS_ACCESS_TOKEN` が必要です。",
  "启用业务删除": "業務削除を有効化",
  "默认关闭。用于体验站禁止整条商品和文/图生图会话被删除，保留溯源证据。工作流节点/连线编辑和参考图删除不受该开关影响。":
    "既定で無効です。デモ環境で商品全体や画像生成チャットセッションの削除を禁止し、追跡証拠を残すために使います。ワークフローノード/接続線の編集や参考画像削除はこのスイッチの影響を受けません。",
  "故障排查": "トラブルシューティング",
  "先看页面上的失败原因，再决定重试、取消、修改提示词、调整参数或检查供应商配置。":
    "まずページ上の失敗理由を確認し、再試行、キャンセル、プロンプト修正、パラメータ調整、プロバイダー設定確認のどれを行うか判断します。",
  "失败分类": "失敗分類",
  "提示": "表示",
  "处理方式": "対応方法",
  "配额或限流": "クォータまたはレート制限",
  "稍后重试，或降低并发。": "後で再試行するか、並行数を下げます。",
  "内容策略": "コンテンツポリシー",
  "调整提示词或参考图。": "プロンプトまたは参考画像を調整します。",
  "网络中断": "ネットワーク中断",
  "检查网络、代理和供应商可用性。": "ネットワーク、プロキシ、プロバイダー可用性を確認します。",
  "请求超时": "リクエストタイムアウト",
  "稍后重试；重复出现时检查供应商状态和超时配置。":
    "後で再試行します。繰り返し発生する場合は、プロバイダー状態とタイムアウト設定を確認します。",
  "参数不支持": "未対応パラメータ",
  "检查尺寸、模型和高级参数。": "サイズ、モデル、高度パラメータを確認します。",
  "重试还是重新运行": "再試行か再実行か",
  "重试适合临时失败，通常复用本次任务的提示词、尺寸、参考图和高级参数。如果你已经修改商品资料、文案、参考图或图片要求，应发起新的运行。":
    "再試行は一時的な失敗に適しており、通常は今回のタスクのプロンプト、サイズ、参考画像、高度パラメータを再利用します。商品データ、コピー、参考画像、画像要件を変更済みの場合は、新しい実行を開始してください。",
  "任务长时间运行中": "タスクが長時間実行中のまま",
  "运行中页面只轮询轻量 status，任务结束后才刷新完整详情。":
    "実行中ページは軽量 status だけをポーリングし、タスク終了後に完全な詳細を更新します。",
  "可取消的运行会显示取消入口。": "キャンセル可能な実行にはキャンセル入口が表示されます。",
  "API 和 worker 启动时会恢复未完成任务。": "API と worker の起動時に未完了タスクが復旧されます。",
  "如果刷新后仍没有变化，检查后端、worker、Redis 和供应商日志。":
    "更新後も変化がない場合は、バックエンド、worker、Redis、プロバイダーログを確認します。",
};

function translateHelpTextToJapanese(text: string): string {
  return HELP_DOC_JA_TRANSLATIONS[text] ?? text;
}

function translateHelpBlockToJapanese(block: SectionBlock): SectionBlock {
  if (block.type === "paragraph" || block.type === "code") {
    return { ...block, text: translateHelpTextToJapanese(block.text) };
  }
  if (block.type === "list" || block.type === "steps") {
    return { ...block, items: block.items.map(translateHelpTextToJapanese) };
  }
  if (block.type === "table") {
    return {
      ...block,
      headers: [translateHelpTextToJapanese(block.headers[0]), translateHelpTextToJapanese(block.headers[1])],
      rows: block.rows.map(
        ([left, right]): [string, string] => [translateHelpTextToJapanese(left), translateHelpTextToJapanese(right)],
      ),
    };
  }
  return {
    ...block,
    title: translateHelpTextToJapanese(block.title),
    text: translateHelpTextToJapanese(block.text),
  };
}

function translateHelpPageToJapanese(page: DocPage): DocPage {
  return {
    ...page,
    title: translateHelpTextToJapanese(page.title),
    description: translateHelpTextToJapanese(page.description),
    category: translateHelpTextToJapanese(page.category),
    sections: page.sections.map((section) => ({
      ...section,
      title: translateHelpTextToJapanese(section.title),
      blocks: section.blocks.map(translateHelpBlockToJapanese),
    })),
  };
}

const DOC_PAGES_JA: DocPage[] = DOC_PAGES.map(translateHelpPageToJapanese);
const NAV_GROUPS_JA: NavGroup[] = NAV_GROUPS.map((group) => ({
  ...group,
  title: translateHelpTextToJapanese(group.title),
}));

function collectHelpBlockTexts(block: SectionBlock): string[] {
  if (block.type === "paragraph" || block.type === "code") {
    return [block.text];
  }
  if (block.type === "list" || block.type === "steps") {
    return block.items;
  }
  if (block.type === "table") {
    return [...block.headers, ...block.rows.flatMap((row) => row)];
  }
  return [block.title, block.text];
}

function collectHelpPageTexts(page: DocPage): string[] {
  return [
    page.title,
    page.description,
    page.category,
    ...page.sections.flatMap((section) => [section.title, ...section.blocks.flatMap(collectHelpBlockTexts)]),
  ];
}

export function getMissingHelpDocTranslations(): string[] {
  const sourceTexts = [...DOC_PAGES.flatMap(collectHelpPageTexts), ...NAV_GROUPS.map((group) => group.title)];
  return Array.from(new Set(sourceTexts)).filter(
    (text) => !Object.prototype.hasOwnProperty.call(HELP_DOC_JA_TRANSLATIONS, text),
  );
}

export function getHelpDocsForLocale(locale: Locale): DocPage[] {
  if (locale === "zh-CN") {
    return DOC_PAGES;
  }
  if (locale === "ja-JP") {
    return DOC_PAGES_JA;
  }
  return DOC_PAGES_EN;
}

export function getHelpNavGroupsForLocale(locale: Locale): NavGroup[] {
  if (locale === "zh-CN") {
    return NAV_GROUPS;
  }
  if (locale === "ja-JP") {
    return NAV_GROUPS_JA;
  }
  return NAV_GROUPS_EN;
}

function findPage(slug: string | null, pages: DocPage[]): DocPage {
  return pages.find((page) => page.slug === slug) ?? pages[0];
}

function pageIndex(page: DocPage, pages: DocPage[]): number {
  return pages.findIndex((item) => item.slug === page.slug);
}

function blockSearchText(block: SectionBlock): string {
  if (block.type === "paragraph" || block.type === "code") {
    return block.text;
  }
  if (block.type === "list" || block.type === "steps") {
    return block.items.join(" ");
  }
  if (block.type === "table") {
    return [block.headers.join(" "), ...block.rows.map((row) => row.join(" "))].join(" ");
  }
  return `${block.title} ${block.text}`;
}

function collectPageSearchText(page: DocPage): string {
  return [
    page.title,
    page.description,
    page.category,
    ...page.sections.flatMap((section) => [section.title, ...section.blocks.map(blockSearchText)]),
  ].join(" ");
}

function findMatchedSection(page: DocPage, query: string): DocSection | null {
  return (
    page.sections.find((section) => {
      const sectionText = [section.title, ...section.blocks.map(blockSearchText)].join(" ").toLowerCase();
      return sectionText.includes(query);
    }) ?? null
  );
}

function getSearchPreview(page: DocPage, query: string): string {
  const source = collectPageSearchText(page).replace(/\s+/g, " ").trim();
  const index = source.toLowerCase().indexOf(query);
  if (index === -1) {
    return page.description;
  }
  const start = Math.max(0, index - 24);
  const end = Math.min(source.length, index + query.length + 44);
  return `${start > 0 ? "..." : ""}${source.slice(start, end)}${end < source.length ? "..." : ""}`;
}

function searchDocPages(queryText: string, pages: DocPage[]): SearchResult[] {
  const query = queryText.trim().toLowerCase();
  if (!query) {
    return [];
  }
  return pages.flatMap((page) => {
    const pageText = collectPageSearchText(page).toLowerCase();
    if (!pageText.includes(query)) {
      return [];
    }
    const matchedSection = findMatchedSection(page, query);
    const titleMatch = page.title.toLowerCase().includes(query);
    const categoryMatch = page.category.toLowerCase().includes(query);
    const descriptionMatch = page.description.toLowerCase().includes(query);
    const score = (titleMatch ? 4 : 0) + (categoryMatch ? 2 : 0) + (descriptionMatch ? 1 : 0);
    return [
      {
        page,
        matchedSectionTitle: matchedSection?.title ?? null,
        preview: getSearchPreview(page, query),
        score,
      },
    ];
  })
    .sort((left, right) => right.score - left.score || pageIndex(left.page, pages) - pageIndex(right.page, pages))
    .slice(0, 8);
}

function renderBlock(block: SectionBlock) {
  if (block.type === "paragraph") {
    return <p className="text-[15px] leading-7 text-slate-700 dark:text-slate-300">{block.text}</p>;
  }
  if (block.type === "list") {
    return (
      <ul className="list-disc space-y-2 pl-5 text-[15px] leading-7 text-slate-700 dark:text-slate-300">
        {block.items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
    );
  }
  if (block.type === "steps") {
    return (
      <ol className="space-y-3">
        {block.items.map((item, index) => (
          <li key={item} className="grid grid-cols-[2rem_minmax(0,1fr)] gap-3 text-[15px] leading-7 text-slate-700 dark:text-slate-300">
            <span className="mt-0.5 flex h-7 w-7 items-center justify-center rounded-full border border-slate-300 bg-white text-xs font-semibold text-slate-600 dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100">
              {index + 1}
            </span>
            <span>{item}</span>
          </li>
        ))}
      </ol>
    );
  }
  if (block.type === "table") {
    return (
      <div className="overflow-hidden rounded-lg border border-slate-200 dark:border-slate-700/80">
        <table className="w-full border-collapse text-left text-sm">
          <thead className="bg-slate-50 text-slate-600 dark:bg-[#151f33] dark:text-slate-300">
            <tr>
              <th className="w-[32%] border-b border-slate-200 px-4 py-3 font-semibold dark:border-slate-700/80">{block.headers[0]}</th>
              <th className="border-b border-slate-200 px-4 py-3 font-semibold dark:border-slate-700/80">{block.headers[1]}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800 dark:bg-[#0f1726]">
            {block.rows.map(([left, right]) => (
              <tr key={`${left}-${right}`}>
                <td className="px-4 py-3 font-medium text-slate-950 dark:text-slate-100">{left}</td>
                <td className="px-4 py-3 leading-6 text-slate-700 dark:text-slate-300">{right}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }
  if (block.type === "code") {
    return (
      <pre className="overflow-x-auto rounded-lg border border-slate-200 bg-slate-950 px-4 py-3 text-sm leading-6 text-slate-100 dark:border-slate-700/80 dark:bg-[#060a12]">
        <code>{block.text}</code>
      </pre>
    );
  }
  return (
    <div className="rounded-lg border border-indigo-100 bg-indigo-50/70 px-4 py-3 dark:border-violet-400/35 dark:bg-violet-500/14">
      <div className="text-sm font-semibold text-indigo-900 dark:text-violet-100">{block.title}</div>
      <p className="mt-1 text-sm leading-6 text-indigo-900/80 dark:text-slate-300">{block.text}</p>
    </div>
  );
}

export function HelpPage() {
  const { locale, t } = useI18n();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [searchQuery, setSearchQuery] = useState("");
  const docPages = getHelpDocsForLocale(locale);
  const navGroups = getHelpNavGroupsForLocale(locale);
  const page = findPage(searchParams.get("page"), docPages);
  const currentIndex = pageIndex(page, docPages);
  const previousPage = currentIndex > 0 ? docPages[currentIndex - 1] : null;
  const nextPage = currentIndex < docPages.length - 1 ? docPages[currentIndex + 1] : null;
  const PageIcon = page.icon;
  const pagesBySlug = useMemo(() => new Map(docPages.map((item) => [item.slug, item])), [docPages]);
  const searchResults = useMemo(() => searchDocPages(searchQuery, docPages), [docPages, searchQuery]);
  const normalizedSearchQuery = searchQuery.trim();

  const openPage = (slug: string) => {
    setSearchParams({ page: slug });
    setSearchQuery("");
    window.scrollTo({ top: 0, behavior: "smooth" });
  };

  return (
    <div className="flex min-h-screen flex-col bg-white dark:bg-[#060a12] dark:text-slate-100">
      <TopNav breadcrumbs={t("help.breadcrumb")} onHome={() => navigate("/products")} />

      <main className="mx-auto grid w-full max-w-[1440px] flex-1 grid-cols-1 lg:grid-cols-[280px_minmax(0,1fr)_220px]">
        <aside className="border-b border-slate-200 bg-slate-50/70 dark:border-slate-800 dark:bg-[#0f1726] lg:sticky lg:top-0 lg:h-screen lg:border-b-0 lg:border-r">
          <div className="border-b border-slate-200 px-5 py-5 dark:border-slate-800">
            <button
              type="button"
              onClick={() => openPage("overview")}
              className="flex items-center gap-2 text-left text-base font-semibold text-slate-950 dark:text-white"
            >
              <BookOpen size={18} className="text-indigo-600 dark:text-violet-300" />
              {t("help.title")}
            </button>
            <div className="relative mt-4">
              <label htmlFor="help-search" className="sr-only">
                {t("help.search")}
              </label>
              <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 dark:text-slate-500" />
              <input
                id="help-search"
                type="search"
                value={searchQuery}
                onChange={(event) => setSearchQuery(event.target.value)}
                placeholder={t("help.search")}
                className="h-9 w-full rounded-lg border border-slate-200 bg-white px-9 text-sm text-slate-900 outline-none transition-colors placeholder:text-slate-400 focus:border-indigo-300 focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:ring-violet-400/20"
              />
              {normalizedSearchQuery ? (
                <div className="absolute left-0 right-0 top-11 z-20 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-lg dark:border-slate-700/80 dark:bg-[#151f33] dark:shadow-black/30">
                  {searchResults.length > 0 ? (
                    <div className="max-h-[360px] overflow-y-auto py-1">
                      {searchResults.map((result) => (
                        <button
                          key={result.page.slug}
                          type="button"
                          onClick={() => openPage(result.page.slug)}
                          className="block w-full px-3 py-2.5 text-left transition-colors hover:bg-slate-50 dark:hover:bg-violet-500/12"
                        >
                          <div className="flex items-center gap-2">
                            <span className="rounded border border-slate-200 px-1.5 py-0.5 text-[11px] font-medium text-slate-500 dark:border-slate-700 dark:text-slate-400">
                              {result.page.category}
                            </span>
                            <span className="min-w-0 truncate text-sm font-semibold text-slate-950 dark:text-white">
                              {result.page.title}
                            </span>
                          </div>
                          {result.matchedSectionTitle ? (
                            <div className="mt-1 text-xs font-medium text-indigo-700 dark:text-violet-200">{result.matchedSectionTitle}</div>
                          ) : null}
                          <p className="mt-1 line-clamp-2 text-xs leading-5 text-slate-600 dark:text-slate-400">{result.preview}</p>
                        </button>
                      ))}
                    </div>
                  ) : (
                    <div className="px-3 py-3 text-sm text-slate-500 dark:text-slate-400">{t("help.noSearchResults")}</div>
                  )}
                </div>
              ) : null}
            </div>
          </div>

          <nav className="hidden space-y-6 px-3 py-5 lg:block" aria-label={t("help.nav")}>
            {navGroups.map((group) => (
              <div key={group.title}>
                <div className="px-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">{group.title}</div>
                <div className="mt-2 space-y-1">
                  {group.pages.map((slug) => {
                    const item = pagesBySlug.get(slug);
                    if (!item) {
                      return null;
                    }
                    const Icon = item.icon;
                    const active = item.slug === page.slug;
                    return (
                      <button
                        key={item.slug}
                        type="button"
                        onClick={() => openPage(item.slug)}
                        aria-current={active ? "page" : undefined}
                        className={`flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors ${
                          active
                            ? "bg-white font-semibold text-indigo-700 shadow-sm ring-1 ring-slate-200 dark:bg-violet-500/18 dark:text-violet-100 dark:ring-violet-400/35"
                            : "text-slate-600 hover:bg-white hover:text-slate-950 dark:text-slate-300 dark:hover:bg-violet-500/12 dark:hover:text-white"
                        }`}
                      >
                        <Icon size={15} className={active ? "text-indigo-600 dark:text-violet-200" : "text-slate-400 dark:text-slate-500"} />
                        <span className="min-w-0 truncate">{item.title}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </nav>

          <div className="p-4 lg:hidden">
            <label htmlFor="doc-page" className="mb-2 block text-xs font-semibold text-slate-500 dark:text-slate-400">
              {t("help.pageSelect")}
            </label>
            <SelectField
              id="doc-page"
              value={page.slug}
              groups={navGroups.map((group) => ({
                label: group.title,
                options: group.pages
                  .map((slug) => {
                    const item = pagesBySlug.get(slug);
                    return item ? { value: item.slug, label: item.title } : null;
                  })
                  .filter((item): item is { value: string; label: string } => Boolean(item)),
              }))}
              onChange={openPage}
              radius="lg"
            />
          </div>
        </aside>

        <article className="min-w-0 bg-white px-5 py-8 dark:bg-[#0b1220] sm:px-8 lg:px-12 lg:py-12">
          <header className="max-w-3xl">
            <div className="mb-4 flex items-center gap-2 text-sm font-medium text-slate-500 dark:text-slate-400">
              <span>{page.category}</span>
              <ChevronRight size={14} />
              <span>{page.title}</span>
            </div>
            <div className="mb-5 inline-flex h-10 w-10 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-indigo-600 dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100">
              <PageIcon size={20} />
            </div>
            <h1 className="text-3xl font-semibold tracking-tight text-slate-950 dark:text-white sm:text-4xl">{page.title}</h1>
            <p className="mt-4 text-base leading-7 text-slate-600 dark:text-slate-300">{page.description}</p>
          </header>

          <div className="mt-10 max-w-3xl space-y-10">
            {page.sections.map((section) => (
              <section key={section.id} id={section.id} className="scroll-mt-6">
                <h2 className="text-xl font-semibold tracking-tight text-slate-950 dark:text-white">{section.title}</h2>
                <div className="mt-4 space-y-4">{section.blocks.map((block, index) => <div key={index}>{renderBlock(block)}</div>)}</div>
              </section>
            ))}
          </div>

          <footer className="mt-12 grid max-w-3xl gap-3 border-t border-slate-200 pt-6 dark:border-slate-800 sm:grid-cols-2">
            {previousPage ? (
              <button
                type="button"
                onClick={() => openPage(previousPage.slug)}
                className="rounded-lg border border-slate-200 px-4 py-3 text-left transition-colors hover:bg-slate-50 dark:border-slate-700/80 dark:bg-[#0f1726] dark:hover:bg-violet-500/12"
              >
                <div className="text-xs font-medium text-slate-500 dark:text-slate-400">{t("help.previous")}</div>
                <div className="mt-1 text-sm font-semibold text-slate-950 dark:text-white">{previousPage.title}</div>
              </button>
            ) : (
              <div />
            )}
            {nextPage ? (
              <button
                type="button"
                onClick={() => openPage(nextPage.slug)}
                className="rounded-lg border border-slate-200 px-4 py-3 text-left transition-colors hover:bg-slate-50 dark:border-slate-700/80 dark:bg-[#0f1726] dark:hover:bg-violet-500/12 sm:text-right"
              >
                <div className="text-xs font-medium text-slate-500 dark:text-slate-400">{t("help.next")}</div>
                <div className="mt-1 inline-flex items-center text-sm font-semibold text-indigo-700 dark:text-violet-200">
                  {nextPage.title}
                  <ArrowRight size={14} className="ml-1" />
                </div>
              </button>
            ) : null}
          </footer>
        </article>

        <aside className="hidden border-l border-slate-200 bg-slate-50/70 px-5 py-12 dark:border-slate-800 dark:bg-[#0f1726] lg:block">
          <div className="sticky top-8">
            <div className="text-sm font-semibold text-slate-950 dark:text-white">{t("help.onThisPage")}</div>
            <nav className="mt-3 space-y-2" aria-label={t("help.onThisPage")}>
              {page.sections.map((section) => (
                <a
                  key={section.id}
                  href={`#${section.id}`}
                  className="block border-l border-slate-200 pl-3 text-sm leading-5 text-slate-500 transition-colors hover:border-indigo-400 hover:text-slate-950 dark:border-slate-700 dark:text-slate-400 dark:hover:border-violet-400 dark:hover:text-white"
                >
                  {section.title}
                </a>
              ))}
            </nav>
            <div className="mt-8 rounded-lg border border-slate-200 bg-slate-50 p-3 dark:border-slate-700/80 dark:bg-[#151f33]">
              <div className="flex items-center gap-2 text-sm font-semibold text-slate-950 dark:text-white">
                <CircleHelp size={15} className="text-indigo-600 dark:text-violet-300" />
                {t("help.needAction")}
              </div>
              <div className="mt-3 grid gap-2">
                <button
                  type="button"
                  onClick={() => navigate("/products")}
                  className="rounded-md bg-slate-950 px-3 py-2 text-sm font-semibold text-white hover:bg-slate-800 dark:bg-violet-500 dark:hover:bg-violet-400"
                >
                  {t("help.openProducts")}
                </button>
                <button
                  type="button"
                  onClick={() => navigate("/image-chat")}
                  className="rounded-md border border-slate-200 bg-white px-3 py-2 text-sm font-semibold text-slate-700 hover:text-slate-950 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:bg-violet-500/12 dark:hover:text-white"
                >
                  {t("help.openImageChat")}
                </button>
              </div>
            </div>
          </div>
        </aside>
      </main>
    </div>
  );
}
