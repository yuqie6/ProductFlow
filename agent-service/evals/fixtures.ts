import { PRODUCTFLOW_SKILL_TOOL_NAME } from "../src/skills.js";
import { toolManifestEntry } from "../src/tool-manifest.js";
import { validatePageContext, type PageContext } from "../src/contracts.js";

export const EVAL_PRODUCT_ID = "22222222-2222-4222-8222-222222222222";
export const EVAL_WORKFLOW_ID = "33333333-3333-4333-8333-333333333333";
export const EVAL_ASSET_ID = "11111111-1111-4111-8111-111111111111";
export const EVAL_RUN_ID = "44444444-4444-4444-8444-444444444444";
export const EVAL_NODE_ID = "node-prompt-1";
export const EVAL_FOLDER_ID = "55555555-5555-4555-8555-555555555555";
const EVAL_DIGEST = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

export interface EvalCall {
  name: string;
  params: unknown;
}

export interface SkillEvalRepair {
  toolName: string;
  illegalParams: unknown;
  repairedParams: unknown;
}

export interface EvalWorld {
  intake: Record<string, unknown> | null;
  birthExpandable: boolean;
  liveGraph: {
    id: string;
    title: string;
    revision: number;
    node_count: number;
    edge_count: number;
    group_count: number;
    nodes: Array<{ id: string; node_type: string; title: string }>;
  };
  failedRun?: { id: string; status: "failed"; failed_node_id: string };
    listedAssets?: Array<{
      id: string;
      display_name: string;
      revision?: number;
      folder_id?: string | null;
      tag_names?: string[];
      is_archived?: boolean;
    }>;
    listedFolders?: Array<{ id: string; title: string }>;
}

export interface SkillEvalFixture {
  skillName: string;
  userRequest: string;
  contractScope: "product_workflow" | "global";
  pageContext: PageContext;
  world: EvalWorld;
  /** 脚本化模型应答：与用户话语和 world 对齐的金标工具序列。 */
  scriptedCalls: readonly EvalCall[];
  neverTools?: readonly string[];
  neverOps?: readonly string[];
  repair?: SkillEvalRepair;
  /** Live 档：第一次写工具返回 422，第二次必须成功（两次内修复）。 */
  liveForceFirstWriteFailure?: string;
}

export interface SkillEvalCatalogView {
  names: readonly string[];
  prompt: string;
}

export function evalPageContext(
  scope: "product_workflow" | "global",
  pageType: string,
  extras?: { selected_asset_ids?: string[]; filters?: Record<string, string> },
): PageContext {
  const product = scope === "product_workflow";
  return {
    snapshot_id: `eval-${pageType}`,
    route: product ? `/products/${EVAL_PRODUCT_ID}` : "/media-library",
    page_type: pageType,
    product_id: product ? EVAL_PRODUCT_ID : null,
    workflow_id: product ? EVAL_WORKFLOW_ID : null,
    selected_asset_ids: extras?.selected_asset_ids ?? [],
    visible_asset_ids: [],
    filters: extras?.filters ?? (product ? {} : { product_id: EVAL_PRODUCT_ID, workflow_id: EVAL_WORKFLOW_ID }),
    workflow_revision: product ? 1 : null,
    library_revision: product ? null : 1,
    digest: EVAL_DIGEST,
    captured_at: "2026-08-31T00:00:00.000Z",
  };
}

function assetBefore(displayName = "旧名"): Record<string, unknown> {
  return {
    revision: 1,
    display_name: displayName,
    folder_id: null,
    tag_names: [],
    is_archived: false,
  };
}

export function sampleLibraryOrganizationDraft(): Record<string, unknown> {
  return {
    schema_version: 1,
    draft_kind: "library_organization",
    library_payload: {
      schema_version: 1,
      confirmation_summary: "把这张图改名",
      operations: [
        {
          operation: "rename",
          asset_id: EVAL_ASSET_ID,
          expected_revision: 1,
          reason: "更清晰的商品名",
          before: assetBefore(),
          target: { display_name: "新名" },
        },
      ],
    },
  };
}

export function sampleLibraryMoveDraft(): Record<string, unknown> {
  return {
    schema_version: 1,
    draft_kind: "library_organization",
    library_payload: {
      schema_version: 1,
      confirmation_summary: "把选中图移到季节文件夹",
      operations: [
        {
          operation: "move",
          asset_id: EVAL_ASSET_ID,
          expected_revision: 1,
          reason: "归档到季节文件夹",
          before: assetBefore("商品图"),
          target: { folder_id: EVAL_FOLDER_ID },
        },
      ],
    },
  };
}

export function illegalLibraryOrganizationDraft(): Record<string, unknown> {
  return {
    schema_version: 1,
    draft_kind: "library_organization",
    library_payload: {
      schema_version: 1,
      confirmation_summary: "空操作",
      operations: [],
    },
  };
}

const NAME_ONLY_GRAPH: EvalWorld["liveGraph"] = {
  id: "g1",
  title: "评测图",
  revision: 1,
  node_count: 1,
  edge_count: 0,
  group_count: 0,
  nodes: [{ id: "source-1", node_type: "product_source", title: "商品资料" }],
};

const EXPANDED_GRAPH: EvalWorld["liveGraph"] = {
  id: "g1",
  title: "评测图",
  revision: 3,
  node_count: 6,
  edge_count: 5,
  group_count: 1,
  nodes: [
    { id: "source-1", node_type: "product_source", title: "商品资料" },
    { id: EVAL_NODE_ID, node_type: "prompt_generation", title: "主图提示词" },
    { id: "node-image-1", node_type: "image_generation", title: "主图 1" },
  ],
};

const EXISTING_INTAKE = {
  schema_version: 1,
  image_types: [
    { key: "hero", quantity: 2, order: 0 },
    { key: "detail", quantity: 2, order: 1 },
  ],
};

function worldNameOnlyEmptyIntake(): EvalWorld {
  return { intake: null, birthExpandable: true, liveGraph: NAME_ONLY_GRAPH };
}

function worldNameOnlyWithIntake(): EvalWorld {
  return { intake: EXISTING_INTAKE, birthExpandable: true, liveGraph: NAME_ONLY_GRAPH };
}

function worldExpanded(failed = false): EvalWorld {
  return {
    intake: EXISTING_INTAKE,
    birthExpandable: false,
    liveGraph: EXPANDED_GRAPH,
    ...(failed ? { failedRun: { id: EVAL_RUN_ID, status: "failed" as const, failed_node_id: EVAL_NODE_ID } } : {}),
  };
}

function worldGlobalLibrary(): EvalWorld {
  return {
    intake: null,
    birthExpandable: false,
    liveGraph: NAME_ONLY_GRAPH,
    listedAssets: [
      {
        id: EVAL_ASSET_ID,
        display_name: "商品图",
        revision: 1,
        folder_id: null,
        tag_names: [],
        is_archived: false,
      },
    ],
    listedFolders: [{ id: EVAL_FOLDER_ID, title: "季节" }],
  };
}

function loadSkill(skillName: string): EvalCall {
  return { name: "load_productflow_skill", params: { skill_name: skillName } };
}

function conciseProductContext(): EvalCall {
  return { name: "get_product_workflow_context_v1", params: { response_format: "concise" } };
}

function addShotOperations(): unknown[] {
  return [
    { op: "create_group", client_ref: "g-scene", title: "场景" },
    {
      op: "create_node",
      client_ref: "p-scene",
      node_type: "prompt_generation",
      title: "场景提示词",
      group_ref: "g-scene",
      config: { image_type_key: "scene", prompt: { design_goal: "场景镜头" } },
    },
    {
      op: "create_node",
      client_ref: "i-scene-1",
      node_type: "image_generation",
      title: "场景 1",
      group_ref: "g-scene",
      config: { image_type_key: "scene" },
    },
    { op: "connect_nodes", client_ref: "e-p-i", source_ref: "p-scene", target_ref: "i-scene-1" },
    { op: "connect_nodes", client_ref: "e-s-p", source_ref: "source-1", target_ref: "p-scene" },
  ];
}

/**
 * 每个技能至少三个 fixture。scriptedCalls 是与话语对齐的脚本化模型应答。
 * 真实模型档由 evals/live-runner.ts 在 opt-in 下执行同一套 fixture。
 */
export const SKILL_EVAL_FIXTURES: readonly SkillEvalFixture[] = [
  {
    skillName: "product-intake",
    userRequest: "为新商品补充图片类型和参考图要求",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldNameOnlyEmptyIntake(),
    scriptedCalls: [
      loadSkill("product-intake"),
      conciseProductContext(),
      {
        name: "ask_user",
        params: {
          header: "图片类型",
          question: "需要哪些图片类型、各几张？参考图是否已经上传？",
          options: [{ label: "主图 2 张" }, { label: "主图 2 张 + 细节 2 张" }],
        },
      },
    ],
    neverTools: ["propose_graph_change_set_v1", "finalize_product_intake_v1"],
    neverOps: ["add_node", "connect"],
  },
  {
    skillName: "product-intake",
    userRequest: "intake 已经有了，但画布还只有商品资料节点，帮我展开模板",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench", { selected_asset_ids: [EVAL_ASSET_ID] }),
    world: worldNameOnlyWithIntake(),
    scriptedCalls: [
      loadSkill("product-intake"),
      conciseProductContext(),
      {
        name: "finalize_product_intake_v1",
        params: {
          selection: EXISTING_INTAKE,
          reference_asset_ids: [EVAL_ASSET_ID],
        },
      },
    ],
    neverTools: ["propose_graph_change_set_v1"],
  },
  {
    skillName: "product-intake",
    userRequest: "用户已经说了主图2张、细节图2张，参考图已上传",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench", { selected_asset_ids: [EVAL_ASSET_ID] }),
    world: worldNameOnlyEmptyIntake(),
    scriptedCalls: [
      loadSkill("product-intake"),
      conciseProductContext(),
      { name: "inspect_product_image_assets_v1", params: { asset_ids: [EVAL_ASSET_ID] } },
      {
        name: "finalize_product_intake_v1",
        params: {
          selection: {
            schema_version: 1,
            image_types: [
              { key: "hero", quantity: 2, order: 0 },
              { key: "detail", quantity: 2, order: 1 },
            ],
          },
          reference_asset_ids: [EVAL_ASSET_ID],
        },
      },
    ],
    neverTools: ["propose_graph_change_set_v1"],
    neverOps: ["add_node"],
  },
  {
    skillName: "workflow-run-request",
    userRequest: "运行当前已经配置好的工作流",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldExpanded(),
    scriptedCalls: [
      loadSkill("workflow-run-request"),
      conciseProductContext(),
      { name: "request_workflow_run_v1", params: { expected_workflow_revision: 3, scope: "graph" } },
    ],
  },
  {
    skillName: "workflow-run-request",
    userRequest: "在全局会话里跑这个商品的工作流",
    contractScope: "global",
    pageContext: evalPageContext("global", "global_agent"),
    world: worldExpanded(),
    scriptedCalls: [
      loadSkill("workflow-run-request"),
      {
        name: "inspect_global_workflow_context_v1",
        params: { product_id: EVAL_PRODUCT_ID, response_format: "concise" },
      },
      {
        name: "request_global_workflow_run_v1",
        params: {
          product_id: EVAL_PRODUCT_ID,
          workflow_id: EVAL_WORKFLOW_ID,
          expected_workflow_revision: 3,
        },
      },
    ],
  },
  {
    skillName: "workflow-run-request",
    userRequest: "只重试刚才失败的那一轮",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldExpanded(true),
    scriptedCalls: [
      loadSkill("workflow-run-request"),
      { name: "inspect_workflow_runs_v1", params: { limit: 10 } },
      {
        name: "request_workflow_run_v1",
        params: { expected_workflow_revision: 3, source_run_id: EVAL_RUN_ID, scope: "graph" },
      },
    ],
  },
  {
    skillName: "media-library-organization",
    userRequest: "把列表里的商品图改名为「新名」",
    contractScope: "global",
    pageContext: evalPageContext("global", "global_agent", { selected_asset_ids: [EVAL_ASSET_ID] }),
    world: worldGlobalLibrary(),
    scriptedCalls: [
      loadSkill("media-library-organization"),
      { name: "list_global_media_library_assets_v1", params: { limit: 20 } },
      { name: "propose_global_draft", params: sampleLibraryOrganizationDraft() },
    ],
  },
  {
    skillName: "media-library-organization",
    userRequest: "先看看这几张选中的图，再把商品图改名为新名",
    contractScope: "global",
    pageContext: evalPageContext("global", "global_agent", { selected_asset_ids: [EVAL_ASSET_ID] }),
    world: worldGlobalLibrary(),
    scriptedCalls: [
      loadSkill("media-library-organization"),
      { name: "list_global_media_library_assets_v1", params: { limit: 20 } },
      { name: "inspect_global_media_library_assets_v1", params: { asset_ids: [EVAL_ASSET_ID] } },
      { name: "propose_global_draft", params: sampleLibraryOrganizationDraft() },
    ],
  },
  {
    skillName: "media-library-organization",
    userRequest: "把选中的图移到季节文件夹",
    contractScope: "global",
    pageContext: evalPageContext("global", "global_agent", {
      selected_asset_ids: [EVAL_ASSET_ID],
      filters: { folder_id: EVAL_FOLDER_ID, folder_title: "季节" },
    }),
    world: worldGlobalLibrary(),
    scriptedCalls: [
      loadSkill("media-library-organization"),
      { name: "list_global_media_library_assets_v1", params: { limit: 20 } },
      { name: "propose_global_draft", params: sampleLibraryMoveDraft() },
    ],
    neverTools: ["apply_graph_change_set_v1"],
    repair: {
      toolName: "propose_global_draft",
      illegalParams: illegalLibraryOrganizationDraft(),
      repairedParams: sampleLibraryMoveDraft(),
    },
  },
  {
    skillName: "graph-editing",
    userRequest: "把节点「主图提示词」改名为新标题",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldExpanded(),
    scriptedCalls: [
      loadSkill("graph-editing"),
      conciseProductContext(),
      {
        name: "apply_graph_change_set_v1",
        params: {
          base_graph_revision: 3,
          summary: "改名",
          operations: [{ op: "rename_node", node_ref: EVAL_NODE_ID, title: "新标题" }],
        },
      },
    ],
    neverOps: ["add_node", "connect"],
    repair: {
      toolName: "apply_graph_change_set_v1",
      illegalParams: {
        base_graph_revision: 3,
        summary: "非法",
        operations: [{ op: "add_node" }],
      },
      repairedParams: {
        base_graph_revision: 3,
        summary: "改名",
        operations: [{ op: "rename_node", node_ref: EVAL_NODE_ID, title: "新标题" }],
      },
    },
    liveForceFirstWriteFailure: "apply_graph_change_set_v1",
  },
  {
    skillName: "graph-editing",
    userRequest: "再加一组场景镜头",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldExpanded(),
    scriptedCalls: [
      loadSkill("graph-editing"),
      conciseProductContext(),
      {
        name: "propose_graph_change_set_v1",
        params: {
          base_graph_revision: 3,
          summary: "加一组场景镜头",
          operations: addShotOperations(),
        },
      },
    ],
    neverOps: ["add_node"],
  },
  {
    skillName: "graph-editing",
    userRequest: "放弃刚才那份还没确认的图改动",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldExpanded(),
    scriptedCalls: [
      loadSkill("graph-editing"),
      { name: "discard_workflow_proposal_v1", params: {} },
    ],
  },
  {
    skillName: "run-diagnosis",
    userRequest: "刚才那次运行为什么失败了",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldExpanded(true),
    scriptedCalls: [
      loadSkill("run-diagnosis"),
      { name: "inspect_workflow_runs_v1", params: { limit: 10 } },
      { name: "get_workflow_run_detail_v1", params: { run_id: EVAL_RUN_ID } },
    ],
  },
  {
    skillName: "run-diagnosis",
    userRequest: "全局里看这个工作流最近一次失败",
    contractScope: "global",
    pageContext: evalPageContext("global", "global_agent"),
    world: worldExpanded(true),
    scriptedCalls: [
      loadSkill("run-diagnosis"),
      { name: "inspect_global_workflow_runs_v1", params: { workflow_ids: [EVAL_WORKFLOW_ID], limit: 5 } },
      {
        name: "get_workflow_run_detail_v1",
        params: { run_id: EVAL_RUN_ID },
      },
    ],
  },
  {
    skillName: "run-diagnosis",
    userRequest: "哪个节点报错了，下一步怎么修",
    contractScope: "product_workflow",
    pageContext: evalPageContext("product_workflow", "product_workbench"),
    world: worldExpanded(true),
    scriptedCalls: [
      loadSkill("run-diagnosis"),
      { name: "get_workflow_run_detail_v1", params: { run_id: EVAL_RUN_ID } },
      { name: "get_node_detail_v1", params: { node_id: EVAL_NODE_ID } },
    ],
    neverTools: ["request_workflow_run_v1"],
  },
];

export function validateSkillEvalFixture(fixture: SkillEvalFixture, catalog: SkillEvalCatalogView): void {
  if (!fixture.userRequest.trim()) {
    throw new Error(`Skill eval fixture is missing request: ${fixture.skillName}`);
  }
  if (fixture.scriptedCalls.length === 0) {
    throw new Error(`Skill eval fixture is missing scriptedCalls: ${fixture.skillName}`);
  }
  validatePageContext(fixture.pageContext);
  if (fixture.contractScope === "global" && fixture.pageContext.product_id !== null) {
    throw new Error(`Global skill eval must not set page_context.product_id: ${fixture.skillName}`);
  }
  if (fixture.contractScope === "product_workflow" && !fixture.pageContext.product_id) {
    throw new Error(`Product skill eval must set page_context.product_id: ${fixture.skillName}`);
  }
  if (fixture.scriptedCalls[0]?.name !== PRODUCTFLOW_SKILL_TOOL_NAME) {
    throw new Error(`Skill eval must load the skill first: ${fixture.skillName}`);
  }
  if (!catalog.names.includes(fixture.skillName)) {
    throw new Error(`Skill eval references unknown catalog skill: ${fixture.skillName}`);
  }
  for (const call of fixture.scriptedCalls) {
    if (!toolManifestEntry(call.name)) throw new Error(`Skill eval references unknown tool: ${call.name}`);
  }
  assertSkillEvalGuardsTools(fixture, catalog);
}

export function assertSkillEvalGuardsTools(fixture: SkillEvalFixture, catalog: SkillEvalCatalogView): void {
  const guarded = new Set(guardsToolsFromCatalogPrompt(catalog.prompt, fixture.skillName));
  for (const call of fixture.scriptedCalls) {
    if (call.name === PRODUCTFLOW_SKILL_TOOL_NAME) continue;
    if (!guarded.has(call.name)) {
      throw new Error(
        `Skill eval ${fixture.skillName} expected tool ${call.name} is not in that skill's guards_tools`,
      );
    }
  }
}

function guardsToolsFromCatalogPrompt(prompt: string, skillName: string): string[] {
  const escapedName = skillName.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
  const match = new RegExp(
    `<skill><name>${escapedName}</name>[\\s\\S]*?<guards_tools>([^<]*)</guards_tools>`,
    "u",
  ).exec(prompt);
  if (!match) {
    throw new Error(`Skill catalog is missing guards_tools for ${skillName}`);
  }
  return match[1]
    .split(",")
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}
