export type OnboardingStatus = "idle" | "active" | "completed" | "skipped";

export type OnboardingPage = "products" | "product-create" | "workbench" | "image-chat";

export interface OnboardingStep {
  id: string;
  page: OnboardingPage;
  title: string;
  shortTitle: string;
  goal: string;
  instructions: string[];
  expected: string;
  ctaLabel: string;
}

export interface OnboardingState {
  status: OnboardingStatus;
  stepId: string;
  updatedAt: string;
}

export const ONBOARDING_STORAGE_KEY = "productflow.onboarding.v1";
export const ONBOARDING_CHANGE_EVENT = "productflow:onboarding-change";

export const ONBOARDING_STEPS: OnboardingStep[] = [
  {
    id: "create-product-entry",
    page: "products",
    title: "第 1 步：先建一个商品",
    shortTitle: "新建商品",
    goal: "从一张清楚的商品主图开始，给后面的文案和图片生成一个固定对象。",
    instructions: [
      "在商品列表确认你要做的是哪个商品。",
      "点击“新建商品”，准备上传一张清楚的主图。",
      "如果已经有商品，可以直接打开它，然后把引导推进到下一步。",
    ],
    expected: "完成后会进入新建商品页，或者打开已有商品的工作台。",
    ctaLabel: "去新建商品",
  },
  {
    id: "create-product-form",
    page: "product-create",
    title: "第 2 步：上传主图并命名",
    shortTitle: "上传主图",
    goal: "让系统知道这次要围绕哪件商品创作。",
    instructions: [
      "上传 JPEG / PNG / WebP 商品图，尽量让主体完整清晰。",
      "填写容易识别的商品名，例如“奶油白通勤托特包”。",
      "点击“创建并继续”。创建成功后，页面会自动进入商品工作台。",
    ],
    expected: "你会看到带有“商品、文案、生图、参考图”等卡片的工作台。",
    ctaLabel: "我已创建，继续",
  },
  {
    id: "workbench-product-context",
    page: "workbench",
    title: "第 3 步：补商品资料",
    shortTitle: "补资料",
    goal: "把本次想强调的卖点先写清楚，后面的文案和图片都会参考它。",
    instructions: [
      "点击画布里的“商品”卡片。",
      "在右侧补类目、价格、商品说明或本次主推方向。",
      "写完后保存；运行使用的是已保存内容，不是还没保存的输入框草稿。",
    ],
    expected: "保存后，商品资料会成为后续文案和图片的共同背景。",
    ctaLabel: "资料已保存，下一步",
  },
  {
    id: "workbench-copy-prompt",
    page: "workbench",
    title: "第 4 步：写文案生成要求",
    shortTitle: "写文案要求",
    goal: "先拿到一版可用标题、卖点和海报主标题。",
    instructions: [
      "点击“文案”卡片；如果画布里没有，就先新增一个文案卡片。",
      "在生成要求里写一句话，例如“突出通勤、轻便、大容量，语气高级但不夸张”。",
      "点击“运行当前节点”，或直接运行整个工作流。",
    ],
    expected: "文案卡片会出现标题、卖点、海报主标题和按钮文案。",
    ctaLabel: "文案已生成，下一步",
  },
  {
    id: "workbench-reference-connect",
    page: "workbench",
    title: "第 5 步：选择或连接参考图",
    shortTitle: "连接参考",
    goal: "用参考图告诉系统你想要的光线、构图或风格。",
    instructions: [
      "如果有风格图，选择“参考图”卡片上传；没有也可以先跳过。",
      "拖动卡片边上的连接点，把参考图连到“文案”或“生图”卡片。",
      "再确认“生图”卡片至少连到一个下游参考图卡片；生成结果会放到那个参考图卡片里。",
    ],
    expected: "画布上会出现连线；生图运行时会把新图片填到下游参考图卡片。",
    ctaLabel: "参考关系确认，下一步",
  },
  {
    id: "workbench-image-prompt-run",
    page: "workbench",
    title: "第 6 步：写生图要求并运行",
    shortTitle: "运行生图",
    goal: "把商品资料、文案和你的画面要求合在一起，生成第一张图。",
    instructions: [
      "点击“生图”卡片。",
      "如果还没连接下游参考图，先从生图卡片的输出点拖到一个参考图卡片。",
      "写清楚场景和画面，例如“白色托特包放在通勤桌面，旁边有笔记本电脑和咖啡，干净自然光”。",
      "点击“运行当前节点”或顶部的“运行”。",
    ],
    expected: "被连接的参考图卡片会出现可预览、可下载的图片。",
    ctaLabel: "已经运行，下一步",
  },
  {
    id: "workbench-inspect-iterate",
    page: "workbench",
    title: "第 7 步：看结果，只改一两个点",
    shortTitle: "检查结果",
    goal: "通过小步修改让图片越来越接近满意效果。",
    instructions: [
      "如果主体不清楚，加“商品占画面中心，主体完整，纹理清晰”。",
      "如果背景太乱，加“干净背景，只保留 1-2 个陪衬物”。",
      "每次只改一两个点，再重新运行，这样更容易知道哪句话有效。",
    ],
    expected: "满意后下载图片；还想继续微调，可以进入连续生图。",
    ctaLabel: "去连续生图微调",
  },
  {
    id: "image-chat-polish",
    page: "image-chat",
    title: "第 8 步：用连续生图继续微调",
    shortTitle: "连续微调",
    goal: "围绕同一商品或同一张结果继续对话式改图。",
    instructions: [
      "选择商品或使用当前会话。",
      "上传参考图，或选中上一轮生成结果。",
      "用一句话提出修改，例如“保持包的角度不变，背景换成更明亮的办公室，减少桌面杂物”。",
    ],
    expected: "得到满意结果后，可以下载，或回写到商品里继续作为参考素材。",
    ctaLabel: "完成引导",
  },
];

export const DEFAULT_ONBOARDING_STATE: OnboardingState = {
  status: "idle",
  stepId: ONBOARDING_STEPS[0].id,
  updatedAt: "",
};

export function getStepIndex(stepId: string): number {
  const index = ONBOARDING_STEPS.findIndex((step) => step.id === stepId);
  return index >= 0 ? index : 0;
}

export function getStepById(stepId: string): OnboardingStep {
  return ONBOARDING_STEPS[getStepIndex(stepId)];
}

export function resolveOnboardingPath(step: OnboardingStep, productId?: string): string {
  if (step.page === "products") {
    return "/products";
  }
  if (step.page === "product-create") {
    return "/products/new";
  }
  if (step.page === "workbench") {
    return productId ? `/products/${productId}` : "/products";
  }
  if (step.page === "image-chat") {
    return productId ? `/products/${productId}/image-chat` : "/image-chat";
  }
  return "/products";
}

export function pageLabel(page: OnboardingPage): string {
  const labels: Record<OnboardingPage, string> = {
    products: "商品列表",
    "product-create": "新建商品页",
    workbench: "商品工作台",
    "image-chat": "连续生图",
  };
  return labels[page];
}
