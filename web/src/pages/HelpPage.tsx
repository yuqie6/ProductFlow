import {
  ArrowRight,
  BookOpen,
  Bot,
  ChevronRight,
  CircleHelp,
  FolderTree,
  GalleryHorizontalEnd,
  GitBranch,
  Image,
  Images,
  PanelRight,
  Search,
  Settings,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { Select as SelectField } from "../components/ui/select";
import { TopNav } from "../components/TopNav";
import type { Locale } from "../lib/i18n";
import { useI18n } from "../lib/preferences";

type SectionBlock =
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "steps"; items: string[] }
  | { type: "callout"; title: string; text: string };

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

export interface HelpNavGroup {
  title: string;
  pages: string[];
}

interface SearchResult {
  page: DocPage;
  matchedSectionTitle: string | null;
  preview: string;
}

/** 权威来源：docs/USER_GUIDE.md。改帮助内容时同步更新用户指南。 */
const HELP_DOCS = {
  "zh-CN": [
    {
      slug: "overview",
      title: "ProductFlow 文档概览",
      description: "当前版本围绕 Agent 商品创建、工作流画布、商品图库、全局素材库和 Global Agent Dock 组织完整链路。",
      category: "快速开始",
      icon: BookOpen,
      sections: [
        {
          id: "current-baseline",
          title: "当前工作方式",
          blocks: [
            {
              type: "steps",
              items: [
                "从商品列表进入新建页，填写商品名称即可点「开始对话」。仅名称时画布只有商品资料节点，对话侧栏会打开。图种、参考图和商品说明齐了，「开始对话」会带上完整画布并打开对话；「只建画布」落下同一张图，不打开对话。只选了图种或只传了参考图时需要补齐，不会悄悄改成仅名称。",
                "在工作台对话框上传 1 至 6 张真实商品参考图，并用文字说明要做哪些图。Agent 会写入商品输入并追问缺口。",
                "图能跑之后，可在对话侧栏显式开始 Goal。一次跑图结束不等于 Goal 完成，由你点完成或清除。开聊不会自动创建 Goal。",
                "「开始对话」进入工作台时打开对话；「只建画布」先看详情。仅名称时画布只有商品资料节点。",
                "运行整张图时，会按 DAG 处理所有具备必需输入的节点：模板种子和需要更新的生图会生成，已写文稿与未变化的图片会记录为已冻结或已复用。没有必连输入的节点不会入队；没有可入队节点时会提示校验问题。没有互相依赖的节点会同时运行；一张失败不会停掉无依赖的同层镜头。运行内容节点不会直接出图。",
                "在画布中编辑提示词、生成规格和参考绑定；上传图与生成图进入商品图库，跨商品长期素材进入 `/media-library`。",
              ],
            },
          ],
        },
        {
          id: "core-objects",
          title: "核心对象",
          blocks: [
            {
              type: "list",
              items: [
                "商品：承载名称、类目、价格和真实商品图片。",
                "工作流：由商品资料、参考图、提示词生成和图片生成节点组成。",
                "视觉体系：保存全套图片共享的色彩、字体、摄影和品质规范。",
                "商品图库：统一管理上传、工作流生成、生图会话写入和交付图片。",
                "全局素材库：跨商品长期保存的图片，可关联到多个工作流，不复制媒体文件。",
                "Agent Session / Task：长期交流容器和一个明确业务目标。",
                "工作流配方：保存用户认可的完整工作流或局部片段，供后续复用。",
              ],
            },
          ],
        },
      ],
    },
    {
      slug: "agent-create",
      title: "Agent 创建商品",
      description: "在对话里补参考图和图种，Agent 写 intake 并用画布改现图。",
      category: "快速开始",
      icon: Bot,
      sections: [
        {
          id: "image-plan",
          title: "图片类型与数量",
          blocks: [
            {
              type: "paragraph",
              text: "图片类型按摄影镜头、信息图、证据图分组。摄影和信息图每种默认 2 张，表示同一镜头的变体，共用一条提示词。资质和工厂是证据图，只占待绑定素材，不会生成。",
            },
            {
              type: "callout",
              title: "参考图在对话里提交",
              text: "把 1 至 6 张能看清商品外形的图发到对话框，并说明图片类型。Agent 不会凭空补造用户没有提供的 Logo、认证、包装或工厂素材。创建页表单齐备后，「开始对话」和「只建画布」都会带上完整画布。",
            },
          ],
        },
        {
          id: "clarification",
          title: "澄清与确认",
          blocks: [
            {
              type: "list",
              items: [
                "在对话框上传参考图并说明图片类型；缺什么由 Agent 在对话里问。",
                "回答写在原轮输入框里，点选项、打字或跳过即可；时间线不会多出第二条用户消息。",
                "商品事实：名称、类目、价格、规格和需要强调的卖点。",
                "生成意图：图片风格、文字语种、文案密度、比例、品质和参考保真度。",
                "缺失信息：只补问会影响工作流或生成结果的问题。",
                "多节点改图在画布上预览确认；关掉对话仍可继续改图。",
              ],
            },
          ],
        },
        {
          id: "direct-create",
          title: "只建画布",
          blocks: [
            {
              type: "paragraph",
              text: "新建页填写商品说明、画面文案、语种，并为每种图片类型选择画幅后，可以点「只建画布」跳过对话、生成可运行的工作流图。商品说明写入创作要求；文案要求和语种写入每张生图节点，画幅按图种写入对应生图节点。默认要文案、简体中文；画幅按图种给默认值，例如首屏 3:4、细节 1:1。上传参考图并选择图片类型后，系统会立刻创建商品资料、身份参考图、视觉规范、创作要求，以及每个摄影/信息图镜头分组。证据类型只放待绑定素材。身份参考接到视觉、创作和会生图的镜头。进入工作台后先看详情，需要时再打开对话；可用「添加场景」再加镜头。Agent 可以解释画布、检查配置和请求运行，不能再提交一份 Draft 覆盖现图。进入工作台后先运行视觉规范、创作要求和提示词，让模型把内容写进检查器；确认后再运行生图节点、镜头或整张图。",
            },
          ],
        },
      ],
    },
    {
      slug: "global-agent",
      title: "全局 Agent Dock",
      description: "登录后的右侧 Dock 管理 Session 和 Task，不取代商品工作台。",
      category: "快速开始",
      icon: PanelRight,
      sections: [
        {
          id: "dock-actions",
          title: "可以做什么",
          blocks: [
            {
              type: "list",
              items: [
                "列出、搜索、新建和归档 Agent Session。",
                "查看 Task 摘要，取消任务，暂停或恢复可暂停的任务。",
                "打开对应商品工作区。",
                "确认全局素材整理 Draft。",
              ],
            },
          ],
        },
        {
          id: "dock-boundary",
          title: "与工作台的边界",
          blocks: [
            {
              type: "callout",
              title: "Dock 不运行工作流",
              text: "编辑节点、运行、取消和重试仍在商品工作台完成。Dock 不提交 WorkflowRun，也不把当前页面当成 Task 目标。",
            },
          ],
        },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "工作流画布",
      description: "画布保留自由添加、连接、拖动、局部组织和节点详情编辑能力。",
      category: "工作流",
      icon: GitBranch,
      sections: [
        {
          id: "nodes",
          title: "节点职责",
          blocks: [
            {
              type: "list",
              items: [
                "商品资料：保存确认过的商品事实。",
                "图片素材：绑定商品图片库中的一张明确图片。",
                "创作要求：运行时根据商品资料和参考图生成目标、文案和限制，结果写进检查器后可再编辑。",
                "视觉规范：运行时根据商品资料和参考图生成风格、背景和限制，结果写进检查器后可再编辑。",
                "提示词生成：运行时根据商品资料、参考图、视觉规范和创作要求生成提示词，结果写进检查器后可再编辑。运行该节点不会生图。",
                "图片生成：保存比例、分辨率、质量、背景、文字策略、参考保真度和执行状态。文字策略会约束提示词生成和出图。直接创建默认要画面文案并带语种；画布上新加的生图节点默认不要文案。运行该节点只出图，读取当前提示词文稿，不要求上游产物。无参考边也可以跑。已写的内容节点不会被整图覆盖。直接创建会把上传图接到视觉规范、创作要求、提示词和生图节点。",
              ],
            },
          ],
        },
        {
          id: "organization",
          title: "连接与局部组织",
          blocks: [
            {
              type: "callout",
              title: "画布不依赖对话",
              text: "关闭右侧 Agent 对话后，添加、连线、详情、运行、撤销和配方与从未打开对话相同。对话失败或状态不明不会锁住画布。Agent 刚改过的节点可以立刻再改、立刻撤销。",
            },
            {
              type: "paragraph",
              text: "节点使用按角色分的输入端口和输出连接点。拖线时合法目标变绿；不兼容端口无法吸附。可拖动已有边的端点换端口。缺必连输入时端口和卡片标红，该节点与该镜头的运行禁用；整图运行在仍有可入队处理节点时可用。还没有工作流时可用从空白建图写入空图，再添加六类节点。节点较多时可用分组收拢局部流程；双击分组进入后只看组内节点，面包屑返回全图。分组只改善布局，不改变执行顺序。",
            },
            {
              type: "list",
              items: [
                "添加面板可添加场景：一次落下分组、提示词和一张生图。选中分组后运行此场景对该组生图提交一次运行，组内无依赖的图并行。",
                "添加单个节点会落在当前视口中心并被选中。",
                "复制粘贴后选中新节点；删除节点前会确认。",
                "Ctrl/Cmd+Z 撤销最近一次图编辑，Shift 组合键重做。",
                "卡片按类型着色，运行失败写在卡片和详情上；缺边不作为失败原因。工具条可运行、运行到此、复制、聚焦、存为配方、删除。有当前出图的生图节点可固定为图片素材。多选时工具条还可编组。",
                "最大化会收起顶部导航。",
                "分组可进入；组内和全图各自记住视口。",
                "图保存进行中时，添加、详情、绑定和配方会暂时不可用。",
                "把素材拖到画布空白处会新建已绑定、未连线的图片节点；拖到参考端口会创建或复用后再连参考边。",
                "悬停整图、节点、运行到此或镜头运行按钮，会按该次提交的范围给将生成、复用、冻结或阻塞的节点着色；点击立即提交。已有运行进行中时新请求进入排队。",
                "窄屏检查器是底抽屉，打开后画布仍露出节点；选中连线后删除保持可见。",
                "详情和运行第一屏用来源节点标题和角色说明输入。",
                "可把全图、当前分组或选中节点存成预设。应用前标明新建或合并，并列出将出现的节点和连线；预览失败时不能确认。完整预设遇到已有工作流会提示冲突；片段无法合并时也会明确提示。",
              ],
            },
          ],
        },
      ],
    },
    {
      slug: "visual-system",
      title: "视觉体系与提示词",
      description: "全局视觉规范和每张图片的具体内容计划分别保存，并在生成时组合。",
      category: "工作流",
      icon: Sparkles,
      sections: [
        {
          id: "shared-system",
          title: "统一视觉体系",
          blocks: [
            {
              type: "list",
              items: [
                "视觉风格、色彩系统、字体层级和装饰语言。",
                "摄影光线、景深、镜头意图和构图留白。",
                "产品形态锁定、允许变化范围、分辨率和真实感要求。",
              ],
            },
          ],
        },
        {
          id: "per-image-prompt",
          title: "单类与单张图片计划",
          blocks: [
            {
              type: "paragraph",
              text: "一类图片可以共享目标、构图和文字要求；需要不同角度或内容时，再为具体图片保存独立提示词。参考图通过独立参考节点绑定，生成结果由对应图片节点承载。",
            },
          ],
        },
      ],
    },
    {
      slug: "product-library",
      title: "商品图库",
      description: "图库采用资源管理器式目录，保存商品范围内的全部图片。",
      category: "素材",
      icon: FolderTree,
      sections: [
        {
          id: "directories",
          title: "目录与分类",
          blocks: [
            {
              type: "list",
              items: [
                "按全部图片、最近生成、用户上传、生成结果、图片类型和来源浏览。",
                "创建、重命名和删除用户文件夹，批量移动当前已加载图片。",
                "使用网格或列表查看，按名称与时间排序，并下载单图或选中图片压缩包。",
                "Agent 可以读取文件名与目录并执行整理，再按需查看具体图片作为参考。",
              ],
            },
          ],
        },
        {
          id: "cover-and-reference",
          title: "封面与参考绑定",
          blocks: [
            {
              type: "paragraph",
              text: "商品封面由系统自动选择，不承担工作流语义。工作流引用图片时，必须把具体图库资产绑定到独立参考图节点。",
            },
          ],
        },
      ],
    },
    {
      slug: "media-library",
      title: "全局素材库",
      description: "`/media-library` 是跨商品长期保存的全局入口。",
      category: "素材",
      icon: Images,
      sections: [
        {
          id: "browse",
          title: "浏览与组织",
          blocks: [
            {
              type: "list",
              items: [
                "搜索、筛选并分页浏览跨商品长期保存的素材。",
                "创建文件夹和标签，批量移动、打标签、归档或恢复。",
                "直接上传图片，或把连续生图候选保存进来。",
              ],
            },
          ],
        },
        {
          id: "associate",
          title: "工作流关联",
          blocks: [
            {
              type: "paragraph",
              text: "全局素材可以关联到某个工作流子图库，同一媒体可被多个工作流使用，不复制文件。Agent 只能提出整理 Draft，确认前不会改名称、文件夹、标签或归档状态。",
            },
          ],
        },
      ],
    },
    {
      slug: "image-chat",
      title: "生图会话",
      description: "独立试图、分支与候选选择，再把认可结果保存到全局素材库或商品图库。",
      category: "素材",
      icon: Image,
      sections: [
        {
          id: "generation",
          title: "候选与分支",
          blocks: [
            {
              type: "list",
              items: [
                "选择尺寸、候选数量和供应商支持的高级图片参数。",
                "可从已完成图片继续分支，并附带最多 6 张会话参考图。",
                "每个候选都保留所属会话、提示词、尺寸、模型和生成关系。",
              ],
            },
          ],
        },
        {
          id: "save-results",
          title: "保存结果",
          blocks: [
            {
              type: "list",
              items: [
                "保存到全局素材库后，可在 `/media-library` 中组织、归档或关联到工作流。",
                "选择目标商品并保存后，当前候选作为一张 canonical 图片进入该商品图库，之后可分类、命名或绑定到参考图节点。",
              ],
            },
          ],
        },
      ],
    },
    {
      slug: "settings",
      title: "模型与运行设置",
      description: "当前供应商配置只包含提示词、工作流 Agent 和图片三种用途。",
      category: "配置",
      icon: Settings,
      sections: [
        {
          id: "providers",
          title: "供应商用途",
          blocks: [
            {
              type: "list",
              items: [
                "提示词：为视觉规范、创作要求和提示词节点提供模型。",
                "工作流 Agent：负责澄清需求、整理图库和创建工作流。",
                "图片：负责工作流与生图会话的图片生成。",
              ],
            },
          ],
        },
        {
          id: "runtime",
          title: "运行配置",
          blocks: [
            {
              type: "paragraph",
              text: "设置页还管理图片工具允许字段、生成尺寸上限、上传限制、队列、安全开关和当前格式的配置导入导出。修改后按页面反馈确认保存结果。",
            },
          ],
        },
      ],
    },
  ],
  "en-US": [
    {
      slug: "overview",
      title: "ProductFlow Docs Overview",
      description: "The current product flow is built around Agent-led product creation, the workflow canvas, the product library, the global media library, and the Global Agent Dock.",
      category: "Getting started",
      icon: BookOpen,
      sections: [
        { id: "current-baseline", title: "Current workflow", blocks: [{ type: "steps", items: ["Open product creation. A name is enough to click Start conversation: the canvas has a product-facts node and the chat panel opens. When the brief, image types, and references are complete, Start conversation lands the full canvas and opens chat; Canvas only lands the same graph without opening chat. A partial image plan must be finished; it is not silently reduced to name-only.", "Upload one to six real product reference images in the workbench composer and say which images you need. The Agent writes that intake and asks about gaps.", "After the graph can run, start a Goal in the conversation sidebar. A finished graph run is not Goal complete; you mark complete or clear it. Opening chat does not create a Goal.", "Start conversation opens the Agent panel. Canvas only opens the workbench on details. Name-only graphs start with a product-facts node.", "A full-graph run considers every processing node with required inputs. Seed visual, brief, and prompt documents and stale images generate; authored documents and unchanged images are recorded as skipped. Independent nodes generate at the same time; one failed image does not stop sibling shots. Running a content node does not render images.", "Edit prompts, generation specifications, and reference bindings. Uploads and generated results belong to the product library; long-lived cross-product media lives in `/media-library`."] }] },
        { id: "core-objects", title: "Core objects", blocks: [{ type: "list", items: ["Product: facts and real product imagery.", "Workflow: product context, reference image, prompt generation, and image generation nodes.", "Visual system: shared color, typography, photography, and quality rules.", "Product library: uploads, workflow output, image-chat attachments, and delivery images.", "Global media library: long-lived cross-product images that can be associated with multiple workflows without copying bytes.", "Agent Session / Task: a long-lived conversation container and one business goal.", "Workflow recipe: a user-saved workflow or fragment for later reuse."] }] },
      ],
    },
    {
      slug: "agent-create",
      title: "Create with the Agent",
      description: "Send references and image types in chat. The Agent writes intake and edits the live canvas.",
      category: "Getting started",
      icon: Bot,
      sections: [
        { id: "image-plan", title: "Image types and quantities", blocks: [{ type: "paragraph", text: "Image types are grouped as photography, infographic, and evidence. Photography and infographic types default to two images in the same shot, sharing one prompt. Certification and factory are evidence placeholders and are not generated." }, { type: "callout", title: "Send references in the conversation", text: "Upload one to six photos that show the real product in the composer, and name the image types. The Agent does not invent a logo, certification, packaging, or factory material that the user did not provide. A complete create form lands the full canvas from either Start conversation or Canvas only." }] },
        { id: "clarification", title: "Clarification and confirmation", blocks: [{ type: "list", items: ["Upload references in chat and name the image types; the Agent asks only for missing facts that change the result.", "Answers stay in the same-turn composer: pick an option, type, or skip. The timeline does not add a second user bubble.", "Product facts: name, category, price, specifications, and selling points.", "Generation intent: style, image-text language, copy density, ratio, quality, and reference fidelity.", "Multi-node edits preview on the canvas; closing the conversation does not lock the graph."] }] },
        { id: "direct-create", title: "Canvas only", blocks: [{ type: "paragraph", text: "On the create page, enter a product brief plus on-image copy and language, then set an aspect ratio for each image type. Canvas only skips chat and writes a runnable workflow immediately. The brief is written into the creative-brief node; copy requirement and language are written onto every image node; aspect ratio is written per type. Defaults are required copy and Simplified Chinese, with type-specific frames such as 3:4 for hero and 1:1 for detail. After you upload references and choose image types, the graph includes product facts, identity photos, visual system, creative brief, and one group per photography or infographic shot. Evidence types are unbound placeholders. Identity photos connect to visual, brief, and generating shots. The workbench opens on details; open the Agent panel when you need it, or add another shot from the add panel. The Agent can explain the graph, check configuration, and request a run; it cannot submit a Draft that replaces the live graph. Run visual, brief, and prompt nodes first so the model writes into the inspector; then run image nodes, a shot, or the whole graph." }] },
      ],
    },
    {
      slug: "global-agent",
      title: "Global Agent Dock",
      description: "After login, the right-hand Dock manages Sessions and Tasks. It does not replace the product workbench.",
      category: "Getting started",
      icon: PanelRight,
      sections: [
        { id: "dock-actions", title: "What it can do", blocks: [{ type: "list", items: ["List, search, create, and archive Agent Sessions.", "Inspect Task summaries, cancel a Task, and pause or resume a pausable Task.", "Open the matching product workspace.", "Confirm a global library-organization Draft."] }] },
        { id: "dock-boundary", title: "Boundary with the workbench", blocks: [{ type: "callout", title: "The Dock does not run workflows", text: "Edit, run, cancel, and retry stay on the product workbench. The Dock does not submit a WorkflowRun or treat the current page as the Task goal." }] },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "Workflow canvas",
      description: "The canvas supports free node creation, connections, movement, local grouping, and detailed node editing.",
      category: "Workflow",
      icon: GitBranch,
      sections: [
        { id: "nodes", title: "Node responsibilities", blocks: [{ type: "list", items: ["Product facts hold confirmed product facts.", "An image asset node binds exactly one product-library image.", "Creative brief runs against product facts and photos and writes goals, copy, and constraints into the inspector.", "Visual system runs against product facts and photos and writes style and background into the inspector.", "Prompt generation writes a prompt into the inspector from product facts, photos, visual system, and brief. Running this node does not render images.", "Image generation stores ratio, quality, text policy, background intent, reference fidelity, and execution state. Direct create defaults to required on-image copy with a language; a new image node added on the canvas defaults to no copy. Running this node only renders from the live prompt document; a prompt artifact is not required. A reference edge is optional. Authored content nodes are not overwritten on a full-graph run. Direct create wires uploads to visual, brief, prompt, and image nodes."] }] },
        { id: "organization", title: "Connections and local organization", blocks: [{ type: "paragraph", text: "Processing nodes expose one input port per Catalog role. Compatible drop targets turn green; incompatible ports do not snap. Existing edges can be reconnected by dragging an endpoint. Missing required inputs turn the port and card red and disable that node or shot Run. Whole-graph Run stays available while at least one processing node can enqueue. Groups can tidy a local flow without changing execution order. Double-click a group to see only its members; the breadcrumb returns to the full graph." }, { type: "list", items: ["The add panel can drop a shot: one group, a prompt, and an image node. Running the shot submits one run for the group's images; independent images run in parallel.", "New nodes land near the current viewport center and stay selected.", "Paste selects the clones; deleting nodes asks for confirmation.", "Undo the last edit; Redo restores it if nothing new was saved.", "Cards keep type color and show failures on the card and in Details; the toolbar can run, run up to here, duplicate, focus, save as recipe, and delete. An image generation node with a current output can pin that result as an image asset.", "Maximize hides the top navigation.", "Groups can be entered; in-group and full-graph viewports are remembered separately.", "While a graph save is in progress, add, inspect, bind, and recipes lock.", "Dropping an asset onto blank canvas creates a bound unused image node; dropping onto a reference port creates or reuses a node, then connects a reference edge.", "Hovering whole-graph, node, run-to-node, or shot Run colors nodes that will generate, reuse, freeze, or block for that submit scope; a click submits immediately. A new request while a run is active joins the queue.", "Save the full graph, current group, or selected nodes as a preset. Applying it on another product previews the nodes and edges that will be created. A full preset conflicts if the product already has a workflow; a fragment that cannot merge also returns an explicit conflict."] }] },
      ],
    },
    {
      slug: "visual-system",
      title: "Visual system and prompts",
      description: "Shared visual rules and per-image content plans are stored separately and combined at generation time.",
      category: "Workflow",
      icon: Sparkles,
      sections: [
        { id: "shared-system", title: "Shared visual system", blocks: [{ type: "list", items: ["Visual style, colors, type hierarchy, and decorative language.", "Lighting, depth of field, lens intent, composition, and whitespace.", "Product-form lock, allowed variation, resolution, and realism requirements."] }] },
        { id: "per-image-prompt", title: "Per-type and per-image plans", blocks: [{ type: "paragraph", text: "An image type may share its goal, composition, and copy requirements. Images that need distinct angles or content receive their own prompts. References are bound through dedicated nodes, and each image node holds its own output." }] },
      ],
    },
    {
      slug: "product-library",
      title: "Product library",
      description: "An Explorer-style directory manages every image owned by a product.",
      category: "Assets",
      icon: FolderTree,
      sections: [
        { id: "directories", title: "Directories and classification", blocks: [{ type: "list", items: ["Browse all images, recent generations, uploads, generated results, image types, and origins.", "Create, rename, and remove user folders, and move currently loaded images in batches.", "Use grid or list view, sort by name or time, and download one image or a ZIP of selected images.", "The Agent can inspect names and directories, organize the library, and open only the images needed as references."] }] },
        { id: "cover-and-reference", title: "Cover and reference binding", blocks: [{ type: "paragraph", text: "The product cover is selected automatically and has no workflow semantics. A workflow reference always binds a concrete library asset to its own reference image node." }] },
      ],
    },
    {
      slug: "media-library",
      title: "Global media library",
      description: "`/media-library` is the long-lived cross-product entry.",
      category: "Assets",
      icon: Images,
      sections: [
        { id: "browse", title: "Browse and organize", blocks: [{ type: "list", items: ["Search, filter, and page through long-lived cross-product assets.", "Create folders and tags; batch-move, tag, archive, or restore.", "Upload images, or save an iterative-image candidate into the library."] }] },
        { id: "associate", title: "Workflow association", blocks: [{ type: "paragraph", text: "A global asset can be associated with a workflow sub-library. The same media can be used by multiple workflows without copying files. The Agent may only propose an organization Draft; names, folders, tags, and archive state do not change before confirmation." }] },
      ],
    },
    {
      slug: "image-chat",
      title: "Image chat",
      description: "Explore branches and candidates independently, then save an accepted result to the global media library or a product library.",
      category: "Assets",
      icon: Image,
      sections: [
        { id: "generation", title: "Candidates and branches", blocks: [{ type: "list", items: ["Choose image size, candidate count, and advanced fields supported by the active provider.", "Branch from a completed image and attach up to six session references.", "Every candidate retains its session, prompt, size, model, and generation relationships."] }] },
        { id: "save-results", title: "Save results", blocks: [{ type: "list", items: ["Save to the global media library, then organize, archive, or associate the asset from `/media-library`.", "Save to a selected product as one canonical product-library image, then classify, rename, or bind it to a reference node."] }] },
      ],
    },
    {
      slug: "settings",
      title: "Models and runtime settings",
      description: "Provider configuration has three current purposes: prompt, workflow Agent, and image.",
      category: "Configuration",
      icon: Settings,
      sections: [
        { id: "providers", title: "Provider purposes", blocks: [{ type: "list", items: ["Prompt supplies the model used by visual system, creative brief, and prompt generation nodes.", "Workflow Agent clarifies requirements, organizes the library, and creates workflows.", "Image powers workflow and image-chat generation."] }] },
        { id: "runtime", title: "Runtime configuration", blocks: [{ type: "paragraph", text: "Settings also manages allowed image-tool fields, maximum generation dimensions, upload limits, queues, security controls, and import or export of the current configuration format. Confirm changes through the page-level save feedback." }] },
      ],
    },
  ],
  "ja-JP": [
    {
      slug: "overview",
      title: "ProductFlow ドキュメント概要",
      description: "現在の ProductFlow は Agent による商品作成、ワークフローキャンバス、商品ライブラリ、グローバル素材ライブラリ、Global Agent Dock で構成されています。",
      category: "はじめに",
      icon: BookOpen,
      sections: [
        { id: "current-baseline", title: "現在の作業フロー", blocks: [{ type: "steps", items: ["商品作成で商品名を入力し「会話を開始」できます。名称のみのキャンバスは商品資料ノードで、会話パネルが開きます。説明・画像タイプ・参考画像が揃うと、「会話を開始」は完全なキャンバスと会話を開き、「キャンバスのみ」は同じグラフで会話を開きません。画像プランが途中のときは揃える必要があり、名称のみへ黙って落とされません。", "ワークベンチの会話に実商品の参考画像を 1 から 6 枚送り、必要な画像を伝えます。Agent が intake を書き、不足を質問します。", "図が実行できる状態になったら、会話サイドバーで Goal を明示開始できます。一度の実行終了は Goal 完了ではありません。完了または解除はユーザーが行います。会話を開いただけでは Goal は作られません。", "「会話を開始」は会話パネルを開きます。「キャンバスのみ」は詳細から入ります。名称のみはまず商品資料ノードです。", "図全体の実行はテンプレート種の視覚・要件・プロンプトだけを補い、更新が必要な出図をやり直します。手書きの原稿は上書きしません。つながっていないノードは同時に出図し、1 枚の失敗で同層の他のカットは止まりません。内容ノードの実行では画像を出しません。", "プロンプト、生成仕様、参考画像の紐付けを編集します。アップロードと生成結果は商品ライブラリへ、長期の横断素材は `/media-library` へ保存されます。"] }] },
        { id: "core-objects", title: "主要オブジェクト", blocks: [{ type: "list", items: ["商品：商品情報と実商品の画像。", "ワークフロー：商品情報、参考画像、プロンプト生成、画像生成ノード。", "ビジュアルシステム：色、書体、撮影、品質の共通ルール。", "商品ライブラリ：アップロード、ワークフロー生成、画像チャット、納品画像。", "グローバル素材ライブラリ：複数ワークフローで共有できる長期画像。メディア bytes は複製しません。", "Agent Session / Task：長期の対話容器と 1 つの業務目標。", "ワークフローレシピ：ユーザーが保存したワークフローまたは部分フロー。"] }] },
      ],
    },
    {
      slug: "agent-create",
      title: "Agent で商品を作成",
      description: "会話で参考画像と画像タイプを送ります。Agent が intake を書き、キャンバスを直します。",
      category: "はじめに",
      icon: Bot,
      sections: [
        { id: "image-plan", title: "画像タイプと枚数", blocks: [{ type: "paragraph", text: "選択した各タイプは初期値 2 枚で、個別に変更できます。複数のメイン画像は候補選択用の場合が多く、複数アングル画像には異なる視点と内容目標が必要です。" }, { type: "callout", title: "参考画像は会話で送ります", text: "実商品が分かる写真を 1 から 6 枚会話に送り、画像タイプを伝えてください。ユーザーが提供していないロゴ、認証、包装、工場素材を Agent が作り足すことはありません。作成フォームが揃うと、「会話を開始」と「キャンバスのみ」のどちらでも完全なキャンバスになります。" }] },
        { id: "clarification", title: "確認事項", blocks: [{ type: "list", items: ["会話で参考画像を送り画像タイプを伝えると、結果が変わる不足分だけ質問します。", "同じターンの入力欄で選択肢・入力・スキップできます。タイムラインに 2 つ目のユーザー吹き出しは出ません。", "商品情報：名称、カテゴリ、価格、仕様、訴求点。", "生成意図：スタイル、画像内テキスト言語、文字量、比率、品質、参考忠実度。", "複数ノードの変更はキャンバスでプレビュー確認します。会話を閉じても図は操作できます。"] }] },
        { id: "direct-create", title: "キャンバスのみ", blocks: [{ type: "paragraph", text: "商品作成では商品説明、画像内コピー、言語を記入し、画像タイプごとに比率を選んだうえで「キャンバスのみ」を押すと、会話を開かずに実行可能なワークフローをすぐ作れます。商品説明は創作要件へ、コピー方針と言語は各画像ノードへ、比率はタイプごとに書き込まれます。初期値はコピー必須、簡体字中国語で、比率はタイプ別（ファーストビュー 3:4、ディテール 1:1 など）です。参考画像をアップロードして画像タイプを選ぶと、商品情報、参考画像、ビジュアルシステム、創作要件、プロンプト、画像生成ノードが作成され、参考画像はこれらのノードに接続されます。ワークベンチは詳細から開きます。必要なときに会話を開き、作成ページの画像要件を再提出する必要はありません。不足があれば Agent が会話で尋ねます。ノードと接続の変更はキャンバス上で行います。Agent はキャンバスの説明、設定確認、実行リクエストができますが、既存グラフを上書きする Draft は再提出できません。先にビジュアル、創作要件、プロンプトを実行して検査器へ書き、確認後に画像ノードまたは図全体を実行します。" }] },
      ],
    },
    {
      slug: "global-agent",
      title: "グローバル Agent Dock",
      description: "ログイン後の右側 Dock で Session と Task を管理します。商品ワークベンチの代替ではありません。",
      category: "はじめに",
      icon: PanelRight,
      sections: [
        { id: "dock-actions", title: "できること", blocks: [{ type: "list", items: ["Agent Session の一覧、検索、作成、アーカイブ。", "Task 要約の確認、キャンセル、一時停止または再開。", "対応する商品ワークスペースを開く。", "グローバル素材整理 Draft を確認する。"] }] },
        { id: "dock-boundary", title: "ワークベンチとの境界", blocks: [{ type: "callout", title: "Dock はワークフローを実行しません", text: "編集、実行、キャンセル、再試行は商品ワークベンチで行います。Dock は WorkflowRun を送信せず、現在のページを Task 目標にもしません。" }] },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "ワークフローキャンバス",
      description: "ノード追加、接続、移動、局所整理、ノード詳細編集を行えます。",
      category: "ワークフロー",
      icon: GitBranch,
      sections: [
        { id: "nodes", title: "ノードの役割", blocks: [{ type: "list", items: ["商品情報ノードは確認済みの商品事実を提供します。", "参考画像ノードは商品ライブラリの具体的な 1 画像を保持します。", "創作要件ノードは実行時に商品情報と参考画像から目標と制限を書き、検査器で再編集できます。", "ビジュアルシステムノードは実行時にスタイルと背景を書き、検査器で再編集できます。", "プロンプト生成ノードは実行時にプロンプトを検査器へ書きます。この実行では画像を出しません。", "画像生成ノードは比率、品質、文字方針、背景、参考忠実度、実行状態を保持します。直接作成の初期値はコピー必須と言語指定です。キャンバスで追加した画像ノードの初期値はコピーなしです。このノードの実行は現在のプロンプト文稿を読んで出図するだけで、成果物は不要です。参考辺は任意です。書き済みの内容ノードは全図実行で上書きしません。直接作成ではアップロード画像をビジュアル、創作要件、プロンプト、画像ノードに接続します。"] }] },
        { id: "organization", title: "接続と局所整理", blocks: [{ type: "paragraph", text: "処理ノードは Catalog の役割ごとに入力ポートを持ちます。接続できる対象は緑、互換しないポートは吸着しません。既存の辺は端点をドラッグして付け直せます。必須入力が欠けているとポートが赤になり、実行ボタンは無効です。グループは配置を整理するだけで、実行順は変えません。ダブルクリックでグループ内だけを表示し、パンくずで全図に戻ります。" }, { type: "list", items: ["新しいノードは現在の表示中央付近に置かれ、選択されたままです。", "貼り付け後は複製が選択されます。ノード削除前に確認します。", "直前の編集を元に戻せます。その後に新しい編集がなければやり直せます。", "カードは種類色を保ち、失敗をカードと詳細に表示します。ツールバーで実行、ここまで実行、複製、フォーカス、レシピ保存、削除ができます。現在の出図がある画像生成ノードは画像素材として固定できます。", "最大化すると上部ナビをしまいます。", "グループに入れます。グループ内と全図の表示位置は別々に覚えます。", "グラフ保存中は追加、詳細、紐付け、レシピが一時ロックされます。", "素材をキャンバス空白へ落とすと、紐付き未接続の画像ノードができます。参考ポートへ落とすと作成または再利用してから参考辺がつながります。", "実行ボタンにマウスを置くと、生成・再利用・凍結・阻塞のノードが色分けされます。クリックですぐ送信します。実行中に新しい要求はキューに入ります。", "全図、現在のグループ、または選択ノードをプリセット保存できます。別の商品へ適用する前に作成されるノードと接続を確認できます。完全プリセットは既存ワークフローがあると衝突します。断片が結合できないときも明示的な衝突になります。"] }] },
      ],
    },
    {
      slug: "visual-system",
      title: "ビジュアルシステムとプロンプト",
      description: "共通ビジュアルルールと画像ごとの内容計画を分けて保存し、生成時に組み合わせます。",
      category: "ワークフロー",
      icon: Sparkles,
      sections: [
        { id: "shared-system", title: "共通ビジュアルシステム", blocks: [{ type: "list", items: ["スタイル、色、書体階層、装飾言語。", "照明、被写界深度、レンズ意図、構図、余白。", "商品形状の固定、許容変更、解像度、写実性。"] }] },
        { id: "per-image-prompt", title: "タイプ別・画像別計画", blocks: [{ type: "paragraph", text: "同じ画像タイプでは目標、構図、文字要件を共有できます。異なる角度や内容が必要な画像には個別プロンプトを保存します。参考画像は専用ノードで紐付け、各画像ノードが出力を保持します。" }] },
      ],
    },
    {
      slug: "product-library",
      title: "商品ライブラリ",
      description: "エクスプローラー形式のディレクトリで商品に属する全画像を管理します。",
      category: "素材",
      icon: FolderTree,
      sections: [
        { id: "directories", title: "ディレクトリと分類", blocks: [{ type: "list", items: ["全画像、最近の生成、アップロード、生成結果、画像タイプ、出所で閲覧できます。", "ユーザーフォルダを作成、名前変更、削除し、読み込み済み画像を一括移動できます。", "グリッドまたは一覧表示、名前と時間による並べ替え、単体または ZIP ダウンロードに対応します。", "Agent はファイル名とディレクトリを確認して整理し、必要な画像だけを参考として開けます。"] }] },
        { id: "cover-and-reference", title: "カバーと参考画像", blocks: [{ type: "paragraph", text: "商品カバーは自動選択され、ワークフロー上の意味を持ちません。ワークフローでは具体的なライブラリ画像を専用の参考画像ノードへ紐付けます。" }] },
      ],
    },
    {
      slug: "media-library",
      title: "グローバル素材ライブラリ",
      description: "`/media-library` は商品を横断する長期保存入口です。",
      category: "素材",
      icon: Images,
      sections: [
        { id: "browse", title: "閲覧と整理", blocks: [{ type: "list", items: ["検索、絞り込み、ページングで長期素材を閲覧します。", "フォルダとタグを作成し、一括移動、タグ付け、アーカイブ、復元ができます。", "画像を直接アップロードするか、連続生成の候補を保存します。"] }] },
        { id: "associate", title: "ワークフロー関連", blocks: [{ type: "paragraph", text: "グローバル素材はワークフローのサブライブラリへ関連付けできます。同じメディアを複数ワークフローで使え、ファイルは複製しません。Agent は整理 Draft だけを提案し、確認前に名前、フォルダ、タグ、アーカイブ状態は変わりません。" }] },
      ],
    },
    {
      slug: "image-chat",
      title: "画像チャット",
      description: "分岐と候補を試し、採用結果をグローバル素材ライブラリまたは商品ライブラリへ保存します。",
      category: "素材",
      icon: Image,
      sections: [
        { id: "generation", title: "候補と分岐", blocks: [{ type: "list", items: ["画像サイズ、候補数、利用中プロバイダが対応する詳細項目を選択します。", "完了画像から分岐し、セッション参考画像を最大 6 枚添付できます。", "各候補にはセッション、プロンプト、サイズ、モデル、生成関係が保存されます。"] }] },
        { id: "save-results", title: "結果を保存", blocks: [{ type: "list", items: ["グローバル素材ライブラリへ保存すると、`/media-library` で整理、アーカイブ、関連付けができます。", "対象商品へ保存すると canonical 画像として商品ライブラリに入り、分類、名前変更、参考ノードへの紐付けができます。"] }] },
      ],
    },
    {
      slug: "settings",
      title: "モデルと実行設定",
      description: "現在のプロバイダ用途はプロンプト、ワークフロー Agent、画像の 3 種類です。",
      category: "設定",
      icon: Settings,
      sections: [
        { id: "providers", title: "プロバイダ用途", blocks: [{ type: "list", items: ["プロンプト：ビジュアルシステム、創作要件、プロンプト生成ノードのモデル。", "ワークフロー Agent：要件確認、ライブラリ整理、ワークフロー作成。", "画像：ワークフローと画像チャットの画像生成。"] }] },
        { id: "runtime", title: "実行設定", blocks: [{ type: "paragraph", text: "画像ツールの許可項目、最大生成サイズ、アップロード制限、キュー、セキュリティ、現在形式の設定インポートとエクスポートも管理します。保存後はページの結果表示を確認してください。" }] },
      ],
    },
  ],
  "vi-VN": [
    {
      slug: "overview",
      title: "Tổng quan tài liệu ProductFlow",
      description: "Luồng hiện tại gồm tạo sản phẩm bằng Agent, canvas quy trình, thư viện sản phẩm, thư viện tài nguyên toàn cục và Global Agent Dock.",
      category: "Bắt đầu",
      icon: BookOpen,
      sections: [
        { id: "current-baseline", title: "Cách làm việc hiện tại", blocks: [{ type: "steps", items: ["Mở trang tạo sản phẩm. Chỉ cần tên là bấm «Bắt đầu hội thoại»: canvas có node thông tin sản phẩm và panel hội thoại mở. Khi mô tả, loại ảnh và ảnh tham chiếu đủ, «Bắt đầu hội thoại» mang canvas đầy đủ và mở chat; «Chỉ tạo canvas» mang cùng đồ thị, không mở chat. Kế hoạch ảnh chưa đủ thì phải bổ sung, không bị âm thầm rút thành chỉ còn tên.", "Tải từ một đến sáu ảnh tham chiếu của sản phẩm thật trong hộp thoại, và nói cần những ảnh nào. Agent ghi intake và hỏi phần còn thiếu.", "Khi đồ thị đã chạy được, bắt đầu Goal ở thanh hội thoại. Một lần chạy xong chưa phải Goal hoàn thành; bạn bấm hoàn thành hoặc xóa. Mở hội thoại không tự tạo Goal.", "«Bắt đầu hội thoại» mở panel Agent. «Chỉ tạo canvas» vào workbench ở tab chi tiết. Chỉ có tên thì trước hết có node thông tin sản phẩm.", "Chạy cả đồ thị chỉ bổ sung brief, visual và prompt còn là hạt giống, rồi vẽ lại ảnh đã cũ. Bản thảo đã viết không bị ghi đè. Các node không nối với nhau chạy cùng lúc; một ảnh thất bại không dừng các shot cùng lớp. Chạy node nội dung không xuất hình.", "Chỉnh prompt, thông số tạo và liên kết ảnh tham chiếu. Ảnh tải lên và kết quả tạo thuộc thư viện sản phẩm; tài nguyên dài hạn xuyên sản phẩm nằm ở `/media-library`."] }] },
        { id: "core-objects", title: "Đối tượng cốt lõi", blocks: [{ type: "list", items: ["Sản phẩm: dữ kiện và ảnh sản phẩm thật.", "Quy trình: node thông tin sản phẩm, ảnh tham chiếu, tạo prompt và tạo ảnh.", "Hệ thống hình ảnh: quy tắc chung về màu, chữ, nhiếp ảnh và chất lượng.", "Thư viện sản phẩm: ảnh tải lên, kết quả quy trình, ảnh từ phiên tạo và ảnh bàn giao.", "Thư viện tài nguyên toàn cục: ảnh dài hạn xuyên sản phẩm, có thể gắn vào nhiều quy trình mà không sao chép file.", "Agent Session / Task: nơi hội thoại dài hạn và một mục tiêu nghiệp vụ.", "Công thức quy trình: quy trình hoặc đoạn quy trình do người dùng lưu để tái sử dụng."] }] },
      ],
    },
    {
      slug: "agent-create",
      title: "Tạo sản phẩm bằng Agent",
      description: "Gửi ảnh tham chiếu và loại ảnh trong hội thoại. Agent ghi intake và sửa canvas.",
      category: "Bắt đầu",
      icon: Bot,
      sections: [
        { id: "image-plan", title: "Loại ảnh và số lượng", blocks: [{ type: "paragraph", text: "Mỗi loại đã chọn mặc định có hai ảnh và có thể chỉnh riêng. Nhiều ảnh chính thường là các ứng viên để chọn, còn ảnh đa góc cần có góc nhìn và mục tiêu nội dung khác nhau." }, { type: "callout", title: "Gửi ảnh tham chiếu trong hội thoại", text: "Tải một đến sáu ảnh cho thấy sản phẩm thật vào hộp thoại và nói loại ảnh. Agent không tự tạo logo, chứng nhận, bao bì hoặc tư liệu nhà máy mà người dùng chưa cung cấp. Biểu mẫu đủ thì «Bắt đầu hội thoại» và «Chỉ tạo canvas» đều mang canvas đầy đủ." }] },
        { id: "clarification", title: "Làm rõ và xác nhận", blocks: [{ type: "list", items: ["Tải ảnh tham chiếu trong hội thoại và nói loại ảnh; Agent chỉ hỏi phần thiếu làm thay đổi kết quả.", "Trả lời ngay ô nhập của lượt hiện tại: chọn, gõ, hoặc bỏ qua. Dòng thời gian không thêm bong bóng người dùng thứ hai.", "Dữ kiện sản phẩm: tên, danh mục, giá, thông số và điểm bán hàng.", "Ý định tạo: phong cách, ngôn ngữ chữ trong ảnh, mật độ nội dung, tỷ lệ, chất lượng và độ trung thành tham chiếu.", "Sửa nhiều node thì xem trước trên canvas; đóng hội thoại vẫn sửa được đồ thị."] }] },
        { id: "direct-create", title: "Chỉ tạo canvas", blocks: [{ type: "paragraph", text: "Trên trang tạo, nhập mô tả sản phẩm cùng nội dung trên ảnh và ngôn ngữ, rồi đặt tỷ lệ cho từng loại ảnh. «Chỉ tạo canvas» bỏ qua hội thoại và ghi ngay một quy trình chạy được. Mô tả được ghi vào node brief; yêu cầu nội dung và ngôn ngữ được ghi vào mọi node ảnh; tỷ lệ được ghi theo từng loại. Mặc định là bắt buộc có nội dung và tiếng Trung giản thể, với khung theo loại như 3:4 cho ảnh bìa và 1:1 cho ảnh chi tiết. Sau khi tải ảnh tham chiếu và chọn loại ảnh, đồ thị gồm thông tin sản phẩm, ảnh tham chiếu, hệ thống hình ảnh, brief sáng tạo, node prompt và node ảnh, với ảnh nối vào các node đó. Workbench mở ở tab chi tiết; cần thì mở hội thoại. Ảnh đã gắn vào sản phẩm, không cần nộp lại yêu cầu ảnh ở trang tạo. Thiếu gì Agent hỏi trong hội thoại. Sửa node và dây trên canvas. Agent có thể giải thích đồ thị, kiểm tra cấu hình và xin chạy; không được nộp Draft khác để ghi đè đồ thị hiện có. Chạy hệ thống hình ảnh, brief và prompt trước để ghi vào inspector; rồi chạy node ảnh hoặc toàn đồ." }] },
      ],
    },
    {
      slug: "global-agent",
      title: "Global Agent Dock",
      description: "Sau khi đăng nhập, Dock bên phải quản lý Session và Task, không thay workbench sản phẩm.",
      category: "Bắt đầu",
      icon: PanelRight,
      sections: [
        { id: "dock-actions", title: "Có thể làm gì", blocks: [{ type: "list", items: ["Liệt kê, tìm, tạo và lưu trữ Agent Session.", "Xem tóm tắt Task, hủy Task, tạm dừng hoặc tiếp tục Task có thể tạm dừng.", "Mở workbench sản phẩm tương ứng.", "Xác nhận Draft sắp xếp thư viện tài nguyên toàn cục."] }] },
        { id: "dock-boundary", title: "Ranh giới với workbench", blocks: [{ type: "callout", title: "Dock không chạy quy trình", text: "Chỉnh, chạy, hủy và thử lại vẫn ở workbench sản phẩm. Dock không gửi WorkflowRun và không lấy trang hiện tại làm mục tiêu Task." }] },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "Canvas quy trình",
      description: "Canvas hỗ trợ thêm node, nối dây, di chuyển, tổ chức cục bộ và chỉnh chi tiết node.",
      category: "Quy trình",
      icon: GitBranch,
      sections: [
        { id: "nodes", title: "Vai trò của node", blocks: [{ type: "list", items: ["Node thông tin sản phẩm cung cấp dữ kiện đã xác nhận.", "Mỗi node ảnh tham chiếu giữ đúng một ảnh trong thư viện sản phẩm.", "Node brief sáng tạo khi chạy ghi mục tiêu và giới hạn vào inspector.", "Node hệ thống hình ảnh khi chạy ghi phong cách và nền vào inspector.", "Node tạo prompt khi chạy ghi prompt vào inspector và không xuất hình.", "Node tạo ảnh lưu tỷ lệ, chất lượng, chính sách chữ, nền, độ trung thành tham chiếu và trạng thái chạy. Tạo trực tiếp mặc định bắt buộc có nội dung và có ngôn ngữ; node ảnh thêm trên canvas mặc định không nội dung. Chạy node này chỉ xuất hình từ văn bản prompt hiện tại, không cần artifact. Cạnh tham chiếu là tùy chọn. Node nội dung đã viết không bị chạy cả đồ thị ghi đè. Tạo trực tiếp nối ảnh tải lên vào hệ thống hình ảnh, brief, prompt và node ảnh."] }] },
        { id: "organization", title: "Kết nối và tổ chức cục bộ", blocks: [{ type: "paragraph", text: "Node xử lý có một cổng vào cho mỗi vai trò Catalog. Mục tiêu hợp lệ chuyển xanh; cổng không tương thích không hút. Có thể kéo đầu cạnh hiện có sang cổng khác. Thiếu đầu vào bắt buộc thì cổng đỏ và nút chạy bị khóa. Nhóm chỉ gọn bố cục, không đổi thứ tự chạy. Nhấp đúp nhóm để chỉ xem node trong nhóm; đường dẫn quay lại toàn đồ." }, { type: "list", items: ["Node mới rơi gần tâm viewport hiện tại và được chọn.", "Dán sẽ chọn bản sao; xóa node có xác nhận.", "Hoàn tác lần chỉnh sửa gần nhất; làm lại được nếu chưa có chỉnh sửa mới.", "Thẻ giữ màu loại và hiện lỗi trên thẻ và trong Chi tiết; thanh công cụ có chạy, chạy đến đây, nhân bản, lấy nét, lưu công thức và xóa. Node tạo ảnh đang có kết quả có thể ghim thành tài sản ảnh.", "Phóng to sẽ thu thanh điều hướng trên.", "Có thể vào nhóm; viewport trong nhóm và toàn đồ được nhớ riêng.", "Khi đang lưu đồ, thêm, chi tiết, gắn ảnh và công thức tạm khóa.", "Thả tài sản vào vùng trống canvas tạo node ảnh đã gắn nhưng chưa nối; thả vào cổng tham chiếu sẽ tạo hoặc tái sử dụng rồi nối cạnh tham chiếu.", "Di chuột lên nút chạy canvas sẽ tô màu node sẽ tạo, tái sử dụng, đóng băng hoặc bị chặn; nhấp là gửi ngay. Yêu cầu mới khi đang chạy sẽ vào hàng đợi.", "Có thể lưu toàn đồ, nhóm hiện tại hoặc node đã chọn thành mẫu. Áp dụng sang sản phẩm khác sẽ xem trước node và đường nối sẽ tạo. Mẫu đầy đủ xung đột nếu sản phẩm đã có quy trình; đoạn không gộp được cũng trả về xung đột rõ ràng."] }] },
      ],
    },
    {
      slug: "visual-system",
      title: "Hệ thống hình ảnh và prompt",
      description: "Quy tắc hình ảnh chung và kế hoạch nội dung từng ảnh được lưu riêng rồi kết hợp khi tạo.",
      category: "Quy trình",
      icon: Sparkles,
      sections: [
        { id: "shared-system", title: "Hệ thống hình ảnh chung", blocks: [{ type: "list", items: ["Phong cách, màu sắc, phân cấp chữ và ngôn ngữ trang trí.", "Ánh sáng, độ sâu trường ảnh, ý định ống kính, bố cục và khoảng trắng.", "Khóa hình dáng sản phẩm, phạm vi thay đổi, độ phân giải và độ chân thực."] }] },
        { id: "per-image-prompt", title: "Kế hoạch theo loại và từng ảnh", blocks: [{ type: "paragraph", text: "Một loại ảnh có thể dùng chung mục tiêu, bố cục và yêu cầu chữ. Ảnh cần góc hoặc nội dung riêng sẽ có prompt riêng. Ảnh tham chiếu được liên kết qua node chuyên dụng và mỗi node ảnh giữ kết quả của mình." }] },
      ],
    },
    {
      slug: "product-library",
      title: "Thư viện sản phẩm",
      description: "Thư mục kiểu trình quản lý tệp chứa mọi ảnh thuộc về một sản phẩm.",
      category: "Tư liệu",
      icon: FolderTree,
      sections: [
        { id: "directories", title: "Thư mục và phân loại", blocks: [{ type: "list", items: ["Duyệt tất cả ảnh, ảnh mới tạo, ảnh tải lên, kết quả tạo, loại ảnh và nguồn.", "Tạo, đổi tên, xóa thư mục người dùng và di chuyển hàng loạt ảnh đã tải.", "Dùng chế độ lưới hoặc danh sách, sắp xếp theo tên hay thời gian, tải một ảnh hoặc tệp ZIP ảnh đã chọn.", "Agent có thể đọc tên và thư mục để sắp xếp, rồi chỉ mở ảnh cần dùng làm tham chiếu."] }] },
        { id: "cover-and-reference", title: "Ảnh bìa và liên kết tham chiếu", blocks: [{ type: "paragraph", text: "Ảnh bìa sản phẩm được chọn tự động và không mang ngữ nghĩa quy trình. Một tham chiếu quy trình luôn liên kết tài sản cụ thể trong thư viện vào node ảnh tham chiếu riêng." }] },
      ],
    },
    {
      slug: "media-library",
      title: "Thư viện tài nguyên toàn cục",
      description: "`/media-library` là cửa vào lưu dài hạn xuyên sản phẩm.",
      category: "Tư liệu",
      icon: Images,
      sections: [
        { id: "browse", title: "Duyệt và tổ chức", blocks: [{ type: "list", items: ["Tìm, lọc và phân trang tài nguyên dài hạn xuyên sản phẩm.", "Tạo thư mục và thẻ; di chuyển, gắn thẻ, lưu trữ hoặc khôi phục hàng loạt.", "Tải ảnh lên trực tiếp, hoặc lưu ứng viên phiên tạo ảnh vào thư viện."] }] },
        { id: "associate", title: "Liên kết quy trình", blocks: [{ type: "paragraph", text: "Tài nguyên toàn cục có thể gắn vào thư viện con của một quy trình. Cùng một media dùng cho nhiều quy trình mà không sao chép file. Agent chỉ đề xuất Draft sắp xếp; tên, thư mục, thẻ và trạng thái lưu trữ không đổi trước khi xác nhận." }] },
      ],
    },
    {
      slug: "image-chat",
      title: "Phiên tạo ảnh",
      description: "Thử các nhánh và ứng viên độc lập, rồi lưu kết quả vào thư viện tài nguyên toàn cục hoặc thư viện sản phẩm.",
      category: "Tư liệu",
      icon: Image,
      sections: [
        { id: "generation", title: "Ứng viên và nhánh", blocks: [{ type: "list", items: ["Chọn kích thước, số ứng viên và trường nâng cao mà nhà cung cấp hiện tại hỗ trợ.", "Tạo nhánh từ ảnh hoàn tất và đính kèm tối đa sáu ảnh tham chiếu của phiên.", "Mỗi ứng viên giữ thông tin phiên, prompt, kích thước, mô hình và quan hệ tạo."] }] },
        { id: "save-results", title: "Lưu kết quả", blocks: [{ type: "list", items: ["Lưu vào thư viện tài nguyên toàn cục, rồi tổ chức, lưu trữ hoặc liên kết từ `/media-library`.", "Lưu vào sản phẩm đích như một ảnh canonical trong thư viện sản phẩm, rồi phân loại, đổi tên hoặc gắn vào node tham chiếu."] }] },
      ],
    },
    {
      slug: "settings",
      title: "Mô hình và cài đặt chạy",
      description: "Cấu hình nhà cung cấp hiện có ba mục đích: prompt, Agent quy trình và ảnh.",
      category: "Cấu hình",
      icon: Settings,
      sections: [
        { id: "providers", title: "Mục đích nhà cung cấp", blocks: [{ type: "list", items: ["Prompt cung cấp mô hình cho hệ thống hình ảnh, brief sáng tạo và node tạo prompt.", "Agent quy trình làm rõ yêu cầu, sắp xếp thư viện và tạo quy trình.", "Ảnh cung cấp khả năng tạo ảnh cho quy trình và phiên tạo ảnh."] }] },
        { id: "runtime", title: "Cấu hình chạy", blocks: [{ type: "paragraph", text: "Trang cài đặt cũng quản lý các trường công cụ ảnh được phép, kích thước tạo tối đa, giới hạn tải lên, hàng đợi, bảo mật và nhập hoặc xuất định dạng cấu hình hiện tại. Hãy xác nhận kết quả qua phản hồi lưu trên trang." }] },
      ],
    },
  ],
} satisfies Record<Locale, DocPage[]>;

export function getHelpDocsForLocale(locale: Locale): DocPage[] {
  return HELP_DOCS[locale];
}

export function getHelpNavGroupsForLocale(locale: Locale): HelpNavGroup[] {
  const groups = new Map<string, string[]>();
  for (const page of HELP_DOCS[locale]) {
    const pages = groups.get(page.category) ?? [];
    pages.push(page.slug);
    groups.set(page.category, pages);
  }
  return Array.from(groups, ([title, pages]) => ({ title, pages }));
}

export function getMissingHelpDocTranslations(locale: Locale = "ja-JP"): string[] {
  const baseline = HELP_DOCS["zh-CN"];
  const localized = new Map(HELP_DOCS[locale].map((page) => [page.slug, page]));
  const missing: string[] = [];
  for (const page of baseline) {
    const candidate = localized.get(page.slug);
    if (!candidate?.title.trim() || !candidate.description.trim() || !candidate.category.trim()) {
      missing.push(page.slug);
      continue;
    }
    if (candidate.sections.length !== page.sections.length) {
      missing.push(`${page.slug}.sections`);
    }
  }
  return missing;
}

function blockText(block: SectionBlock): string {
  if (block.type === "paragraph") return block.text;
  if (block.type === "callout") return `${block.title} ${block.text}`;
  return block.items.join(" ");
}

function searchDocPages(query: string, pages: DocPage[]): SearchResult[] {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return [];
  return pages.flatMap((page) => {
    const matchingSection = page.sections.find((section) =>
      [section.title, ...section.blocks.map(blockText)].join(" ").toLocaleLowerCase().includes(normalized),
    );
    const pageMatches = [page.title, page.description, page.category].join(" ").toLocaleLowerCase().includes(normalized);
    if (!matchingSection && !pageMatches) return [];
    return [{
      page,
      matchedSectionTitle: matchingSection?.title ?? null,
      preview: matchingSection ? blockText(matchingSection.blocks[0]) : page.description,
    }];
  });
}

function renderBlock(block: SectionBlock) {
  if (block.type === "paragraph") {
    return <p className="text-[15px] leading-7 text-slate-700 dark:text-slate-300">{block.text}</p>;
  }
  if (block.type === "list") {
    return (
      <ul className="list-disc space-y-2 pl-5 text-[15px] leading-7 text-slate-700 marker:text-indigo-500 dark:text-slate-300 dark:marker:text-violet-300">
        {block.items.map((item) => <li key={item}>{item}</li>)}
      </ul>
    );
  }
  if (block.type === "steps") {
    return (
      <ol className="space-y-3">
        {block.items.map((item, index) => (
          <li key={item} className="grid grid-cols-[1.75rem_minmax(0,1fr)] gap-3 text-[15px] leading-7 text-slate-700 dark:text-slate-300">
            <span className="mt-0.5 flex h-7 w-7 items-center justify-center rounded-full border border-slate-300 bg-white text-xs font-semibold text-slate-600 dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100">
              {index + 1}
            </span>
            <span>{item}</span>
          </li>
        ))}
      </ol>
    );
  }
  return (
    <div className="rounded-lg border border-indigo-100 bg-indigo-50/70 px-4 py-3 dark:border-violet-400/35 dark:bg-violet-500/10">
      <div className="text-sm font-semibold text-indigo-950 dark:text-violet-100">{block.title}</div>
      <p className="mt-1 text-sm leading-6 text-indigo-950/80 dark:text-slate-300">{block.text}</p>
    </div>
  );
}

export function HelpPage() {
  const { locale, t } = useI18n();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [searchQuery, setSearchQuery] = useState("");
  const pages = getHelpDocsForLocale(locale);
  const navGroups = getHelpNavGroupsForLocale(locale);
  const requestedSlug = searchParams.get("page");
  const page = pages.find((item) => item.slug === requestedSlug) ?? pages[0];
  const currentIndex = pages.findIndex((item) => item.slug === page.slug);
  const previousPage = currentIndex > 0 ? pages[currentIndex - 1] : null;
  const nextPage = currentIndex < pages.length - 1 ? pages[currentIndex + 1] : null;
  const pagesBySlug = useMemo(() => new Map(pages.map((item) => [item.slug, item])), [pages]);
  const searchResults = useMemo(() => searchDocPages(searchQuery, pages), [pages, searchQuery]);
  const PageIcon = page.icon;

  const openPage = (slug: string) => {
    setSearchParams({ page: slug });
    setSearchQuery("");
    window.scrollTo({ top: 0, behavior: "smooth" });
  };

  return (
    <div className="flex min-h-screen flex-col bg-white dark:bg-[#060a12] dark:text-slate-100">
      <TopNav breadcrumbs={t("help.breadcrumb")} onHome={() => navigate("/home")} />

      <main className="mx-auto grid w-full max-w-[1440px] flex-1 grid-cols-1 lg:grid-cols-[248px_minmax(0,1fr)_208px]">
        <aside className="border-b border-slate-200 bg-slate-50/70 dark:border-slate-800 dark:bg-[#0f1726] lg:border-r lg:border-b-0">
          <div className="border-b border-slate-200 px-4 py-5 dark:border-slate-800">
            <button type="button" onClick={() => openPage("overview")} className="flex items-center gap-2 text-left text-base font-semibold text-slate-950 dark:text-white">
              <BookOpen size={18} className="text-indigo-600 dark:text-violet-300" />
              {t("help.title")}
            </button>
            <div className="relative mt-4">
              <label htmlFor="help-search" className="sr-only">{t("help.search")}</label>
              <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
              <input
                id="help-search"
                type="search"
                value={searchQuery}
                onChange={(event) => setSearchQuery(event.target.value)}
                placeholder={t("help.search")}
                className="h-9 w-full rounded-lg border border-slate-200 bg-white px-9 text-sm text-slate-900 outline-none placeholder:text-slate-400 focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:focus:border-violet-400 dark:focus:ring-violet-400/20"
              />
              {searchQuery.trim() ? (
                <div className="absolute left-0 right-0 top-11 z-20 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-lg dark:border-slate-700 dark:bg-[#151f33]">
                  {searchResults.length ? searchResults.map((result) => (
                    <button key={result.page.slug} type="button" onClick={() => openPage(result.page.slug)} className="block w-full border-b border-slate-100 px-3 py-2.5 text-left last:border-b-0 hover:bg-slate-50 dark:border-slate-800 dark:hover:bg-violet-500/10">
                      <div className="truncate text-sm font-semibold text-slate-950 dark:text-white">{result.page.title}</div>
                      <div className="mt-1 line-clamp-2 text-xs leading-5 text-slate-500 dark:text-slate-400">{result.preview}</div>
                    </button>
                  )) : <div className="px-3 py-3 text-sm text-slate-500 dark:text-slate-400">{t("help.noSearchResults")}</div>}
                </div>
              ) : null}
            </div>
          </div>

          <nav className="hidden space-y-5 px-3 py-5 lg:block" aria-label={t("help.nav")}>
            {navGroups.map((group) => (
              <div key={group.title}>
                <div className="px-2 text-xs font-semibold text-slate-500 dark:text-slate-400">{group.title}</div>
                <div className="mt-2 space-y-1">
                  {group.pages.map((slug) => {
                    const item = pagesBySlug.get(slug)!;
                    const Icon = item.icon;
                    const active = item.slug === page.slug;
                    return (
                      <button key={item.slug} type="button" onClick={() => openPage(item.slug)} aria-current={active ? "page" : undefined} className={`flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors ${active ? "bg-white font-semibold text-indigo-700 shadow-sm ring-1 ring-slate-200 dark:bg-violet-500/15 dark:text-violet-100 dark:ring-violet-400/35" : "text-slate-600 hover:bg-white hover:text-slate-950 dark:text-slate-300 dark:hover:bg-violet-500/10 dark:hover:text-white"}`}>
                        <Icon size={15} className={active ? "text-indigo-600 dark:text-violet-200" : "text-slate-400"} />
                        <span className="min-w-0 truncate">{item.title}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </nav>

          <div className="p-4 lg:hidden">
            <label htmlFor="doc-page" className="mb-2 block text-xs font-semibold text-slate-500 dark:text-slate-400">{t("help.pageSelect")}</label>
            <SelectField
              id="doc-page"
              value={page.slug}
              groups={navGroups.map((group) => ({
                label: group.title,
                options: group.pages.map((slug) => ({ value: slug, label: pagesBySlug.get(slug)!.title })),
              }))}
              onChange={openPage}
            />
          </div>
        </aside>

        <article className="min-w-0 bg-white px-5 py-8 dark:bg-[#0b1220] sm:px-8 lg:px-10 lg:py-10">
          <header className="max-w-3xl">
            <div className="mb-4 flex items-center gap-2 text-sm text-slate-500 dark:text-slate-400">
              <span>{page.category}</span><ChevronRight size={14} /><span>{page.title}</span>
            </div>
            <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-indigo-600 dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100"><PageIcon size={20} /></div>
            <h1 className="text-3xl font-semibold text-slate-950 dark:text-white">{page.title}</h1>
            <p className="mt-3 text-base leading-7 text-slate-600 dark:text-slate-300">{page.description}</p>
          </header>

          <div className="mt-9 max-w-3xl space-y-9">
            {page.sections.map((section) => (
              <section key={section.id} id={section.id} className="scroll-mt-6">
                <h2 className="text-xl font-semibold text-slate-950 dark:text-white">{section.title}</h2>
                <div className="mt-4 space-y-4">{section.blocks.map((block, index) => <div key={`${section.id}-${index}`}>{renderBlock(block)}</div>)}</div>
              </section>
            ))}
          </div>

          <footer className="mt-12 grid max-w-3xl gap-3 border-t border-slate-200 pt-6 dark:border-slate-800 sm:grid-cols-2">
            {previousPage ? <button type="button" onClick={() => openPage(previousPage.slug)} className="rounded-lg border border-slate-200 px-4 py-3 text-left hover:bg-slate-50 dark:border-slate-700 dark:bg-[#0f1726] dark:hover:bg-violet-500/10"><div className="text-xs text-slate-500">{t("help.previous")}</div><div className="mt-1 text-sm font-semibold text-slate-950 dark:text-white">{previousPage.title}</div></button> : <div />}
            {nextPage ? <button type="button" onClick={() => openPage(nextPage.slug)} className="rounded-lg border border-slate-200 px-4 py-3 text-left hover:bg-slate-50 dark:border-slate-700 dark:bg-[#0f1726] dark:hover:bg-violet-500/10 sm:text-right"><div className="text-xs text-slate-500">{t("help.next")}</div><div className="mt-1 inline-flex items-center text-sm font-semibold text-indigo-700 dark:text-violet-200">{nextPage.title}<ArrowRight size={14} className="ml-1" /></div></button> : null}
          </footer>
        </article>

        <aside className="hidden border-l border-slate-200 bg-slate-50/70 px-4 py-10 dark:border-slate-800 dark:bg-[#0f1726] lg:block">
          <div className="sticky top-8">
            <div className="text-sm font-semibold text-slate-950 dark:text-white">{t("help.onThisPage")}</div>
            <nav className="mt-3 space-y-2" aria-label={t("help.onThisPage")}>
              {page.sections.map((section) => <a key={section.id} href={`#${section.id}`} className="block border-l border-slate-200 pl-3 text-sm leading-5 text-slate-500 hover:border-indigo-400 hover:text-slate-950 dark:border-slate-700 dark:text-slate-400 dark:hover:border-violet-400 dark:hover:text-white">{section.title}</a>)}
            </nav>
            <div className="mt-8 border-t border-slate-200 pt-5 dark:border-slate-800">
              <div className="flex items-center gap-2 text-sm font-semibold text-slate-950 dark:text-white"><CircleHelp size={15} className="text-indigo-600 dark:text-violet-300" />{t("help.needAction")}</div>
              <div className="mt-3 grid gap-2">
                <button type="button" onClick={() => navigate("/products")} className="rounded-md bg-slate-950 px-3 py-2 text-sm font-semibold text-white hover:bg-slate-800 dark:bg-violet-500 dark:hover:bg-violet-400">{t("help.openProducts")}</button>
                <button type="button" onClick={() => navigate("/media-library")} className="rounded-md border border-slate-200 bg-white px-3 py-2 text-sm font-semibold text-slate-700 hover:text-slate-950 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:bg-violet-500/10 dark:hover:text-white"><Images size={14} className="mr-1.5 inline" />{t("help.openMediaLibrary")}</button>
                <button type="button" onClick={() => navigate("/image-chat")} className="rounded-md border border-slate-200 bg-white px-3 py-2 text-sm font-semibold text-slate-700 hover:text-slate-950 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:bg-violet-500/10 dark:hover:text-white"><GalleryHorizontalEnd size={14} className="mr-1.5 inline" />{t("help.openImageChat")}</button>
              </div>
            </div>
          </div>
        </aside>
      </main>
    </div>
  );
}
