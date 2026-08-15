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
  Search,
  Settings,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { SelectField } from "../components/SelectField";
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

const HELP_DOCS = {
  "zh-CN": [
    {
      slug: "overview",
      title: "ProductFlow 文档概览",
      description: "当前版本围绕 Agent 商品创建、V2 工作流画布和统一商品图库组织完整链路。",
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
                "从商品列表进入新建页，选择需要的图片类型和各自数量。",
                "上传 1 至 6 张真实商品参考图，与 Agent 补充价格、风格、文案和文字语种等信息。",
                "确认方案后进入工作台，画布会流式展示 Agent 创建的节点、连线和文件夹。",
                "在画布中编辑提示词、生成规格和参考绑定，所有上传图与生成图统一进入商品图库。",
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
      description: "先让 Agent 理解真实商品与交付目标，再由用户确认并生成工作流。",
      category: "快速开始",
      icon: Bot,
      sections: [
        {
          id: "image-plan",
          title: "图片类型与数量",
          blocks: [
            {
              type: "paragraph",
              text: "每种已选图片默认生成 2 张，可单独修改数量。多张主图通常用于候选挑选，多角度图则应在工作流中体现不同视角和内容目标。",
            },
            {
              type: "callout",
              title: "参考图是创建前提",
              text: "上传内容必须能证明商品真实外观与结构。Agent 不会凭空补造用户没有提供的 Logo、认证、包装或工厂素材。",
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
                "商品事实：名称、类目、价格、规格和需要强调的卖点。",
                "生成意图：图片风格、文字语种、文案密度、比例、品质和参考保真度。",
                "缺失信息：只补问会影响工作流或生成结果的问题。",
                "最终确认：用户确认图片计划、视觉体系、提示词计划和参考绑定后才创建画布。",
              ],
            },
          ],
        },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "V2 工作流画布",
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
                "商品资料节点：提供已确认的商品事实。",
                "参考图节点：一个节点承载一张具体图片，可从商品图库绑定。",
                "提示词生成节点：根据商品事实、视觉体系和图片目标生成单类或单张图片提示词。",
                "图片生成节点：保存比例、品质、文字策略、背景意图和参考保真度，并执行生图。",
              ],
            },
          ],
        },
        {
          id: "organization",
          title: "连接与局部组织",
          blocks: [
            {
              type: "paragraph",
              text: "节点使用清晰的输入与输出连接点。节点较多时可用画布文件夹收拢一组局部流程；文件夹只改善布局和阅读，不改变 DAG 执行语义。",
            },
            {
              type: "callout",
              title: "复用由用户决定",
              text: "完整流程或选中片段可以保存为工作流配方，后续复用只来自用户主动保存的内容。",
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
      slug: "image-chat",
      title: "生图会话",
      description: "独立试图、分支与候选选择，并把认可结果写入目标商品图库。",
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
          id: "save-product",
          title: "保存到商品",
          blocks: [
            {
              type: "paragraph",
              text: "选择目标商品并保存后，当前候选会作为一张 canonical 图片进入该商品图库。之后可在图库中分类、命名或绑定到工作流参考图节点。",
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
                "提示词：为提示词生成节点提供模型。",
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
      description: "The current product flow is built around Agent-led product creation, the V2 workflow canvas, and one canonical product library.",
      category: "Getting started",
      icon: BookOpen,
      sections: [
        { id: "current-baseline", title: "Current workflow", blocks: [{ type: "steps", items: ["Open product creation and choose the required image types and quantity for each type.", "Upload one to six real product reference images and clarify price, style, copy, and image-text language with the Agent.", "Confirm the plan, then enter the workbench while folders, nodes, and edges are revealed progressively.", "Edit prompts, generation specifications, and reference bindings. Every upload and generated result belongs to the product library."] }] },
        { id: "core-objects", title: "Core objects", blocks: [{ type: "list", items: ["Product: facts and real product imagery.", "Workflow: product context, reference image, prompt generation, and image generation nodes.", "Visual system: shared color, typography, photography, and quality rules.", "Product library: uploads, workflow output, image-chat attachments, and delivery images.", "Workflow recipe: a user-saved workflow or fragment for later reuse."] }] },
      ],
    },
    {
      slug: "agent-create",
      title: "Create with the Agent",
      description: "Let the Agent understand the real product and delivery goals before generating a workflow.",
      category: "Getting started",
      icon: Bot,
      sections: [
        { id: "image-plan", title: "Image types and quantities", blocks: [{ type: "paragraph", text: "Each selected type defaults to two images and can be adjusted independently. Multiple hero images are often candidates to choose from, while multi-angle images should carry distinct viewpoints and content goals." }, { type: "callout", title: "References are required", text: "Uploads must demonstrate the actual product. The Agent does not invent a logo, certification, packaging, or factory material that the user did not provide." }] },
        { id: "clarification", title: "Clarification and confirmation", blocks: [{ type: "list", items: ["Product facts: name, category, price, specifications, and selling points.", "Generation intent: style, image-text language, copy density, ratio, quality, and reference fidelity.", "Missing information: questions are limited to details that affect the workflow or output.", "Final confirmation: the canvas is created only after the image plan, visual system, prompt plan, and references are approved."] }] },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "V2 workflow canvas",
      description: "The canvas supports free node creation, connections, movement, local grouping, and detailed node editing.",
      category: "Workflow",
      icon: GitBranch,
      sections: [
        { id: "nodes", title: "Node responsibilities", blocks: [{ type: "list", items: ["Product context supplies confirmed product facts.", "A reference image node holds exactly one product-library image.", "Prompt generation combines facts, visual rules, and the image goal into a prompt.", "Image generation stores ratio, quality, text policy, background intent, reference fidelity, and execution state."] }] },
        { id: "organization", title: "Connections and local organization", blocks: [{ type: "paragraph", text: "Nodes expose clear input and output connection points. Canvas folders can group a local flow when the graph becomes large; they improve layout without changing DAG execution semantics." }, { type: "callout", title: "Reuse is user-owned", text: "Save a full workflow or selected fragment as a workflow recipe. Later reuse comes only from content the user explicitly saved." }] },
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
      slug: "image-chat",
      title: "Image chat",
      description: "Explore branches and candidates independently, then attach an accepted result to a product library.",
      category: "Assets",
      icon: Image,
      sections: [
        { id: "generation", title: "Candidates and branches", blocks: [{ type: "list", items: ["Choose image size, candidate count, and advanced fields supported by the active provider.", "Branch from a completed image and attach up to six session references.", "Every candidate retains its session, prompt, size, model, and generation relationships."] }] },
        { id: "save-product", title: "Save to a product", blocks: [{ type: "paragraph", text: "After selecting a target product, save the current candidate as one canonical image in that product library. It can then be classified, renamed, or bound to a workflow reference node." }] },
      ],
    },
    {
      slug: "settings",
      title: "Models and runtime settings",
      description: "Provider configuration has three current purposes: prompt, workflow Agent, and image.",
      category: "Configuration",
      icon: Settings,
      sections: [
        { id: "providers", title: "Provider purposes", blocks: [{ type: "list", items: ["Prompt supplies the model used by prompt generation nodes.", "Workflow Agent clarifies requirements, organizes the library, and creates workflows.", "Image powers workflow and image-chat generation."] }] },
        { id: "runtime", title: "Runtime configuration", blocks: [{ type: "paragraph", text: "Settings also manages allowed image-tool fields, maximum generation dimensions, upload limits, queues, security controls, and import or export of the current configuration format. Confirm changes through the page-level save feedback." }] },
      ],
    },
  ],
  "ja-JP": [
    {
      slug: "overview",
      title: "ProductFlow ドキュメント概要",
      description: "現在の ProductFlow は Agent による商品作成、V2 ワークフローキャンバス、統合商品ライブラリで構成されています。",
      category: "はじめに",
      icon: BookOpen,
      sections: [
        { id: "current-baseline", title: "現在の作業フロー", blocks: [{ type: "steps", items: ["商品作成画面で必要な画像タイプとタイプごとの枚数を選択します。", "実商品の参考画像を 1 から 6 枚アップロードし、価格、スタイル、コピー、画像内テキストの言語を Agent と確認します。", "計画を確認するとワークベンチへ移動し、フォルダ、ノード、エッジが順次表示されます。", "プロンプト、生成仕様、参考画像の紐付けを編集します。アップロード画像と生成結果は商品ライブラリに保存されます。"] }] },
        { id: "core-objects", title: "主要オブジェクト", blocks: [{ type: "list", items: ["商品：商品情報と実商品の画像。", "ワークフロー：商品情報、参考画像、プロンプト生成、画像生成ノード。", "ビジュアルシステム：色、書体、撮影、品質の共通ルール。", "商品ライブラリ：アップロード、ワークフロー生成、画像チャット、納品画像。", "ワークフローレシピ：ユーザーが保存したワークフローまたは部分フロー。"] }] },
      ],
    },
    {
      slug: "agent-create",
      title: "Agent で商品を作成",
      description: "ワークフロー生成前に、Agent が実商品と納品目標を理解します。",
      category: "はじめに",
      icon: Bot,
      sections: [
        { id: "image-plan", title: "画像タイプと枚数", blocks: [{ type: "paragraph", text: "選択した各タイプは初期値 2 枚で、個別に変更できます。複数のメイン画像は候補選択用の場合が多く、複数アングル画像には異なる視点と内容目標が必要です。" }, { type: "callout", title: "参考画像は必須です", text: "アップロード画像は実商品の外観と構造を示す必要があります。ユーザーが提供していないロゴ、認証、包装、工場素材を Agent が作り足すことはありません。" }] },
        { id: "clarification", title: "確認事項", blocks: [{ type: "list", items: ["商品情報：名称、カテゴリ、価格、仕様、訴求点。", "生成意図：スタイル、画像内テキスト言語、文字量、比率、品質、参考忠実度。", "不足情報：ワークフローや出力に影響する項目だけを質問します。", "最終確認：画像計画、ビジュアルシステム、プロンプト計画、参考画像を確認後にキャンバスを作成します。"] }] },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "V2 ワークフローキャンバス",
      description: "ノード追加、接続、移動、局所整理、ノード詳細編集を行えます。",
      category: "ワークフロー",
      icon: GitBranch,
      sections: [
        { id: "nodes", title: "ノードの役割", blocks: [{ type: "list", items: ["商品情報ノードは確認済みの商品事実を提供します。", "参考画像ノードは商品ライブラリの具体的な 1 画像を保持します。", "プロンプト生成ノードは商品情報、ビジュアルルール、画像目標を組み合わせます。", "画像生成ノードは比率、品質、文字方針、背景、参考忠実度、実行状態を保持します。"] }] },
        { id: "organization", title: "接続と局所整理", blocks: [{ type: "paragraph", text: "ノードには明確な入出力接続点があります。グラフが大きい場合はキャンバスフォルダで部分フローをまとめられます。フォルダは配置を整理し、DAG の実行意味は変更しません。" }, { type: "callout", title: "再利用はユーザーが管理", text: "ワークフロー全体または選択部分をワークフローレシピとして保存できます。後から再利用できるのは、ユーザーが明示的に保存した内容だけです。" }] },
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
      slug: "image-chat",
      title: "画像チャット",
      description: "分岐と候補を試し、採用結果を対象商品のライブラリへ保存します。",
      category: "素材",
      icon: Image,
      sections: [
        { id: "generation", title: "候補と分岐", blocks: [{ type: "list", items: ["画像サイズ、候補数、利用中プロバイダが対応する詳細項目を選択します。", "完了画像から分岐し、セッション参考画像を最大 6 枚添付できます。", "各候補にはセッション、プロンプト、サイズ、モデル、生成関係が保存されます。"] }] },
        { id: "save-product", title: "商品へ保存", blocks: [{ type: "paragraph", text: "対象商品を選択して保存すると、現在の候補が canonical 画像として商品ライブラリに入ります。その後、分類、名前変更、参考画像ノードへの紐付けができます。" }] },
      ],
    },
    {
      slug: "settings",
      title: "モデルと実行設定",
      description: "現在のプロバイダ用途はプロンプト、ワークフロー Agent、画像の 3 種類です。",
      category: "設定",
      icon: Settings,
      sections: [
        { id: "providers", title: "プロバイダ用途", blocks: [{ type: "list", items: ["プロンプト：プロンプト生成ノードのモデル。", "ワークフロー Agent：要件確認、ライブラリ整理、ワークフロー作成。", "画像：ワークフローと画像チャットの画像生成。"] }] },
        { id: "runtime", title: "実行設定", blocks: [{ type: "paragraph", text: "画像ツールの許可項目、最大生成サイズ、アップロード制限、キュー、セキュリティ、現在形式の設定インポートとエクスポートも管理します。保存後はページの結果表示を確認してください。" }] },
      ],
    },
  ],
  "vi-VN": [
    {
      slug: "overview",
      title: "Tổng quan tài liệu ProductFlow",
      description: "Luồng hiện tại gồm tạo sản phẩm bằng Agent, canvas quy trình V2 và một thư viện sản phẩm thống nhất.",
      category: "Bắt đầu",
      icon: BookOpen,
      sections: [
        { id: "current-baseline", title: "Cách làm việc hiện tại", blocks: [{ type: "steps", items: ["Mở trang tạo sản phẩm, chọn các loại ảnh cần thiết và số lượng cho từng loại.", "Tải lên từ một đến sáu ảnh tham chiếu của sản phẩm thật, rồi làm rõ giá, phong cách, nội dung và ngôn ngữ chữ trong ảnh với Agent.", "Xác nhận kế hoạch để vào workbench; thư mục, node và edge sẽ xuất hiện tuần tự.", "Chỉnh prompt, thông số tạo và liên kết ảnh tham chiếu. Mọi ảnh tải lên và kết quả tạo đều thuộc thư viện sản phẩm."] }] },
        { id: "core-objects", title: "Đối tượng cốt lõi", blocks: [{ type: "list", items: ["Sản phẩm: dữ kiện và ảnh sản phẩm thật.", "Quy trình: node thông tin sản phẩm, ảnh tham chiếu, tạo prompt và tạo ảnh.", "Hệ thống hình ảnh: quy tắc chung về màu, chữ, nhiếp ảnh và chất lượng.", "Thư viện sản phẩm: ảnh tải lên, kết quả quy trình, ảnh từ phiên tạo và ảnh bàn giao.", "Công thức quy trình: quy trình hoặc đoạn quy trình do người dùng lưu để tái sử dụng."] }] },
      ],
    },
    {
      slug: "agent-create",
      title: "Tạo sản phẩm bằng Agent",
      description: "Agent hiểu sản phẩm thật và mục tiêu bàn giao trước khi tạo quy trình.",
      category: "Bắt đầu",
      icon: Bot,
      sections: [
        { id: "image-plan", title: "Loại ảnh và số lượng", blocks: [{ type: "paragraph", text: "Mỗi loại đã chọn mặc định có hai ảnh và có thể chỉnh riêng. Nhiều ảnh chính thường là các ứng viên để chọn, còn ảnh đa góc cần có góc nhìn và mục tiêu nội dung khác nhau." }, { type: "callout", title: "Bắt buộc có ảnh tham chiếu", text: "Ảnh tải lên phải thể hiện sản phẩm thật. Agent không tự tạo logo, chứng nhận, bao bì hoặc tư liệu nhà máy mà người dùng chưa cung cấp." }] },
        { id: "clarification", title: "Làm rõ và xác nhận", blocks: [{ type: "list", items: ["Dữ kiện sản phẩm: tên, danh mục, giá, thông số và điểm bán hàng.", "Ý định tạo: phong cách, ngôn ngữ chữ trong ảnh, mật độ nội dung, tỷ lệ, chất lượng và độ trung thành tham chiếu.", "Thông tin thiếu: chỉ hỏi những chi tiết ảnh hưởng đến quy trình hoặc kết quả.", "Xác nhận cuối: chỉ tạo canvas sau khi kế hoạch ảnh, hệ thống hình ảnh, kế hoạch prompt và ảnh tham chiếu được duyệt."] }] },
      ],
    },
    {
      slug: "workflow-canvas",
      title: "Canvas quy trình V2",
      description: "Canvas hỗ trợ thêm node, nối dây, di chuyển, tổ chức cục bộ và chỉnh chi tiết node.",
      category: "Quy trình",
      icon: GitBranch,
      sections: [
        { id: "nodes", title: "Vai trò của node", blocks: [{ type: "list", items: ["Node thông tin sản phẩm cung cấp dữ kiện đã xác nhận.", "Mỗi node ảnh tham chiếu giữ đúng một ảnh trong thư viện sản phẩm.", "Node tạo prompt kết hợp dữ kiện, quy tắc hình ảnh và mục tiêu của ảnh.", "Node tạo ảnh lưu tỷ lệ, chất lượng, chính sách chữ, nền, độ trung thành tham chiếu và trạng thái chạy."] }] },
        { id: "organization", title: "Kết nối và tổ chức cục bộ", blocks: [{ type: "paragraph", text: "Node có điểm vào và ra rõ ràng. Khi đồ thị lớn, có thể gom một luồng cục bộ bằng thư mục canvas; thư mục chỉ cải thiện bố cục và không đổi ngữ nghĩa thực thi DAG." }, { type: "callout", title: "Người dùng sở hữu việc tái sử dụng", text: "Có thể lưu toàn bộ quy trình hoặc phần đã chọn thành công thức quy trình. Chỉ nội dung người dùng chủ động lưu mới được tái sử dụng sau này." }] },
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
      slug: "image-chat",
      title: "Phiên tạo ảnh",
      description: "Thử các nhánh và ứng viên độc lập, rồi lưu kết quả được chọn vào thư viện sản phẩm.",
      category: "Tư liệu",
      icon: Image,
      sections: [
        { id: "generation", title: "Ứng viên và nhánh", blocks: [{ type: "list", items: ["Chọn kích thước, số ứng viên và trường nâng cao mà nhà cung cấp hiện tại hỗ trợ.", "Tạo nhánh từ ảnh hoàn tất và đính kèm tối đa sáu ảnh tham chiếu của phiên.", "Mỗi ứng viên giữ thông tin phiên, prompt, kích thước, mô hình và quan hệ tạo."] }] },
        { id: "save-product", title: "Lưu vào sản phẩm", blocks: [{ type: "paragraph", text: "Sau khi chọn sản phẩm đích, lưu ứng viên hiện tại thành một ảnh canonical trong thư viện sản phẩm. Sau đó có thể phân loại, đổi tên hoặc liên kết ảnh vào node tham chiếu của quy trình." }] },
      ],
    },
    {
      slug: "settings",
      title: "Mô hình và cài đặt chạy",
      description: "Cấu hình nhà cung cấp hiện có ba mục đích: prompt, Agent quy trình và ảnh.",
      category: "Cấu hình",
      icon: Settings,
      sections: [
        { id: "providers", title: "Mục đích nhà cung cấp", blocks: [{ type: "list", items: ["Prompt cung cấp mô hình cho node tạo prompt.", "Agent quy trình làm rõ yêu cầu, sắp xếp thư viện và tạo quy trình.", "Ảnh cung cấp khả năng tạo ảnh cho quy trình và phiên tạo ảnh."] }] },
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
      <TopNav breadcrumbs={t("help.breadcrumb")} onHome={() => navigate("/products")} />

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
              radius="lg"
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
                <button type="button" onClick={() => navigate("/image-chat")} className="rounded-md border border-slate-200 bg-white px-3 py-2 text-sm font-semibold text-slate-700 hover:text-slate-950 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:bg-violet-500/10 dark:hover:text-white"><GalleryHorizontalEnd size={14} className="mr-1.5 inline" />{t("help.openImageChat")}</button>
              </div>
            </div>
          </div>
        </aside>
      </main>
    </div>
  );
}
