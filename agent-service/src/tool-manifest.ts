/**
 * ProductFlow 工具的唯一清单。
 *
 * 工具实现可以把业务校验留在 tools.ts，但名称、描述、作用域、效果等级、
 * UI 卡片类型、输入 schema、结果元数据 schema 和结果压缩策略只能在这里声明。
 * 动态草案 schema 由 ProductFlow contract 注入。会话级 tool_contract_version 是
 * sha256(静态清单版本 + "\\n" + 规范 JSON(draft_schema))，Go 下发、agent-service 校验。
 */

import { createHash } from "node:crypto";
import { Type, type Static, type TSchema } from "typebox";
import { Value } from "typebox/value";

import { applyGraphChangeSetParameters, proposeGraphChangeSetParameters } from "./graph-command-schema.js";

export type ToolEffect = "read" | "ui_effect" | "mutate" | "approval";
export type ToolScope = "both" | "product" | "global" | "synthetic";
export type ToolResultReducer = "none" | "list" | "context" | "detail";
export type ToolInputSchemaSource = "static" | "contract:draft_schema";
export type ToolRecoveryPolicy = "none" | "reconcile_only" | "reconcile_then_retry";

export const TOOL_EFFECTS = ["read", "ui_effect", "mutate", "approval"] as const;

const idSchema = Type.String({ minLength: 1, maxLength: 64 });
const refSchema = Type.String({ minLength: 1, maxLength: 80 });
const emptyInputSchema = Type.Object({}, { additionalProperties: false });
const responseFormatSchema = Type.Union([Type.Literal("concise"), Type.Literal("detailed")]);

const skillInputSchema = Type.Object(
  {
    skill_name: Type.String({ minLength: 1, maxLength: 64 }),
    resource_path: Type.Optional(Type.String({ maxLength: 256 })),
  },
  { additionalProperties: false },
);

const askUserInputSchema = Type.Object(
  {
    header: Type.String({ minLength: 1, maxLength: 32 }),
    question: Type.String({ minLength: 1, maxLength: 2_000 }),
    options: Type.Array(
      Type.Object(
        {
          label: Type.String({ minLength: 1, maxLength: 80 }),
          description: Type.Optional(Type.String({ maxLength: 240 })),
        },
        { additionalProperties: false },
      ),
      { minItems: 2, maxItems: 5 },
    ),
  },
  { additionalProperties: false },
);

const productImageListInputSchema = Type.Object(
  {
    directory_kind: Type.Union([
      Type.Literal("all"),
      Type.Literal("recent_generated"),
      Type.Literal("uploads"),
      Type.Literal("generated"),
      Type.Literal("image_type"),
      Type.Literal("source"),
      Type.Literal("unorganized"),
      Type.Literal("user_folder"),
    ]),
    directory_key: Type.Optional(Type.Union([Type.String({ maxLength: 120 }), Type.Null()])),
    query: Type.Optional(Type.String({ maxLength: 255 })),
    sort: Type.Optional(
      Type.Union([
        Type.Literal("created_desc"),
        Type.Literal("created_asc"),
        Type.Literal("name_asc"),
        Type.Literal("name_desc"),
      ]),
    ),
    after: Type.Optional(Type.String({ maxLength: 1_024 })),
    limit: Type.Integer({ minimum: 1, maximum: 100 }),
  },
  { additionalProperties: false },
);

const imageInspectionInputSchema = Type.Object(
  { asset_ids: Type.Array(idSchema, { minItems: 1, maxItems: 6 }) },
  { additionalProperties: false },
);

const workflowRunFields = {
  expected_workflow_revision: Type.Integer({ minimum: 1 }),
  source_run_id: Type.Optional(idSchema),
  scope: Type.Optional(Type.Union([Type.Literal("graph"), Type.Literal("node"), Type.Literal("to_node"), Type.Literal("selection")])),
  node_id: Type.Optional(idSchema),
  node_ids: Type.Optional(Type.Array(idSchema, { minItems: 1, maxItems: 64 })),
  force: Type.Optional(Type.Boolean()),
  document_action: Type.Optional(Type.Union([
    Type.Literal("complete"),
    Type.Literal("rewrite"),
    Type.Literal("replace"),
  ])),
};

const productWorkflowRunInputSchema = Type.Object(workflowRunFields, { additionalProperties: false });
const globalWorkflowRunInputSchema = Type.Object(
  {
    product_id: idSchema,
    workflow_id: idSchema,
    ...workflowRunFields,
  },
  { additionalProperties: false },
);

const intakeInputSchema = Type.Object(
  {
    selection: Type.Object(
      {
        schema_version: Type.Literal(1),
        delivery_preset_key: Type.Optional(Type.String({ minLength: 1, maxLength: 80 })),
        image_types: Type.Array(
          Type.Object(
            {
              key: Type.String({ minLength: 1, maxLength: 64 }),
              quantity: Type.Integer({ minimum: 1, maximum: 6 }),
              order: Type.Integer({ minimum: 0 }),
            },
            { additionalProperties: false },
          ),
          { minItems: 1, maxItems: 15 },
        ),
      },
      { additionalProperties: false },
    ),
    reference_asset_ids: Type.Array(idSchema, { minItems: 1, maxItems: 6 }),
  },
  { additionalProperties: false },
);

function paginationInputSchema(maximum: number) {
  return Type.Object(
    {
      query: Type.Optional(Type.String({ maxLength: 255 })),
      cursor: Type.Optional(Type.String({ maxLength: 4_096 })),
      limit: Type.Integer({ minimum: 1, maximum: maximum }),
    },
    { additionalProperties: false },
  );
}

const productInspectionInputSchema = Type.Object(
  { product_ids: Type.Array(idSchema, { minItems: 1, maxItems: 20 }) },
  { additionalProperties: false },
);

const globalWorkflowInspectionInputSchema = Type.Object(
  {
    workflow_ids: Type.Array(idSchema, { minItems: 1, maxItems: 20 }),
    limit: Type.Integer({ minimum: 1, maximum: 10 }),
  },
  { additionalProperties: false },
);

const canvasFocusInputSchema = Type.Object(
  {
    node_ids: Type.Optional(Type.Array(idSchema, { maxItems: 20 })),
    edge_ids: Type.Optional(Type.Array(idSchema, { maxItems: 20 })),
    group_ids: Type.Optional(Type.Array(idSchema, { maxItems: 20 })),
  },
  { additionalProperties: false },
);

const globalProductContextInputSchema = Type.Object(
  {
    product_id: idSchema,
    response_format: Type.Optional(responseFormatSchema),
  },
  { additionalProperties: false },
);

const productContextInputSchema = Type.Object(
  { response_format: Type.Optional(responseFormatSchema) },
  { additionalProperties: false },
);

const workflowRunDetailInputSchema = Type.Object(
  { run_id: idSchema },
  { additionalProperties: false },
);

export const TOOL_PARAMETER_SCHEMAS = {
  load_productflow_skill: skillInputSchema,
  ask_user: askUserInputSchema,
  productflow_context_injection: emptyInputSchema,
  get_product_workflow_context_v1: productContextInputSchema,
  inspect_workflow_runs_v1: Type.Object(
    { limit: Type.Integer({ minimum: 1, maximum: 20 }) },
    { additionalProperties: false },
  ),
  list_product_image_assets_v2: productImageListInputSchema,
  inspect_product_image_assets_v1: imageInspectionInputSchema,
  request_workflow_run_v1: productWorkflowRunInputSchema,
  request_global_workflow_run_v1: globalWorkflowRunInputSchema,
  finalize_product_intake_v1: intakeInputSchema,
  list_products_v1: paginationInputSchema(100),
  inspect_products_v1: productInspectionInputSchema,
  inspect_global_workflow_context_v1: globalProductContextInputSchema,
  inspect_global_workflow_runs_v1: globalWorkflowInspectionInputSchema,
  list_global_media_library_assets_v1: paginationInputSchema(100),
  inspect_global_media_library_assets_v1: imageInspectionInputSchema,
  create_product_workspace_v1: Type.Object(
    { name: Type.String({ minLength: 1, maxLength: 255 }) },
    { additionalProperties: false },
  ),
  // The actual schema is resolved from ProductFlowContract.draft_schema at runtime.
  propose_global_draft: Type.Object({}, { additionalProperties: true }),
  get_node_detail_v1: Type.Object(
    { node_id: Type.String({ minLength: 1, maxLength: 80 }) },
    { additionalProperties: false },
  ),
  get_workflow_run_detail_v1: workflowRunDetailInputSchema,
  apply_graph_change_set_v1: applyGraphChangeSetParameters,
  propose_graph_change_set_v1: proposeGraphChangeSetParameters,
  discard_workflow_proposal_v1: Type.Object(
    { proposal_id: Type.Optional(idSchema) },
    { additionalProperties: false },
  ),
  cancel_workflow_run_v1: Type.Object(
    { run_id: idSchema },
    { additionalProperties: false },
  ),
  focus_canvas_items_v1: canvasFocusInputSchema,
} as const;

export type ToolName = keyof typeof TOOL_PARAMETER_SCHEMAS;
export type ToolParams<Name extends ToolName> = Static<(typeof TOOL_PARAMETER_SCHEMAS)[Name]>;

export const TOOL_RECOVERY_POLICIES = {
  load_productflow_skill: "none",
  ask_user: "none",
  productflow_context_injection: "none",
  get_product_workflow_context_v1: "none",
  inspect_workflow_runs_v1: "none",
  list_product_image_assets_v2: "none",
  inspect_product_image_assets_v1: "none",
  request_workflow_run_v1: "reconcile_then_retry",
  request_global_workflow_run_v1: "reconcile_then_retry",
  finalize_product_intake_v1: "reconcile_then_retry",
  list_products_v1: "none",
  inspect_products_v1: "none",
  inspect_global_workflow_context_v1: "none",
  inspect_global_workflow_runs_v1: "none",
  list_global_media_library_assets_v1: "none",
  inspect_global_media_library_assets_v1: "none",
  create_product_workspace_v1: "reconcile_then_retry",
  propose_global_draft: "none",
  get_node_detail_v1: "none",
  get_workflow_run_detail_v1: "none",
  apply_graph_change_set_v1: "reconcile_then_retry",
  propose_graph_change_set_v1: "reconcile_then_retry",
  discard_workflow_proposal_v1: "reconcile_then_retry",
  cancel_workflow_run_v1: "reconcile_then_retry",
  focus_canvas_items_v1: "none",
} as const satisfies Record<ToolName, ToolRecoveryPolicy>;

export const LIVE_GRAPH_TOOL_NAMES = [
  "get_node_detail_v1",
  "apply_graph_change_set_v1",
  "propose_graph_change_set_v1",
  "discard_workflow_proposal_v1",
  "cancel_workflow_run_v1",
  "focus_canvas_items_v1",
] as const satisfies readonly ToolName[];

const resultMetaFields = {
  summary: Type.Optional(Type.String({ maxLength: 240 })),
  scope: Type.Optional(Type.Union([Type.Literal("global"), Type.Literal("product_workflow")])),
  product_id: Type.Optional(idSchema),
  workflow_id: Type.Optional(idSchema),
  run_id: Type.Optional(idSchema),
  proposal_id: Type.Optional(idSchema),
  request_id: Type.Optional(idSchema),
  expected_workflow_revision: Type.Optional(Type.Integer({ minimum: 1 })),
  artifact_name: Type.Optional(Type.String({ maxLength: 120 })),
  workflow_title: Type.Optional(Type.String({ maxLength: 240 })),
  skill_name: Type.Optional(Type.String({ minLength: 1, maxLength: 64 })),
  resource_path: Type.Optional(Type.String({ maxLength: 256 })),
  instruction_excerpt: Type.Optional(Type.String({ maxLength: 12 * 1024 })),
  instruction_truncated: Type.Optional(Type.Boolean()),
  question_id: Type.Optional(idSchema),
  product_workspace_created: Type.Optional(Type.Boolean()),
  asset_count: Type.Optional(Type.Integer({ minimum: 0, maximum: 100 })),
  item_count: Type.Optional(Type.Integer({ minimum: 0, maximum: 128 })),
  node_count: Type.Optional(Type.Integer({ minimum: 0, maximum: 10_000 })),
  group_count: Type.Optional(Type.Integer({ minimum: 0, maximum: 10_000 })),
  affected_node_ids: Type.Optional(Type.Array(refSchema, { maxItems: 128 })),
  affected_edge_ids: Type.Optional(Type.Array(refSchema, { maxItems: 128 })),
  affected_group_ids: Type.Optional(Type.Array(refSchema, { maxItems: 128 })),
  operation_summaries: Type.Optional(Type.Array(Type.String({ maxLength: 240 }), { maxItems: 128 })),
  reconciled: Type.Optional(Type.Boolean()),
  pending_confirmation: Type.Optional(Type.Boolean()),
  response_format: Type.Optional(Type.Union([Type.Literal("concise"), Type.Literal("detailed")])),
  truncated: Type.Optional(Type.Boolean()),
  original_bytes: Type.Optional(Type.Integer({ minimum: 0 })),
  max_bytes: Type.Optional(Type.Integer({ minimum: 1 })),
} as const;

type ResultMetaField = keyof typeof resultMetaFields;

/** 每个工具只声明 UI 回放所需字段，避免不同卡片的私有载荷互相渗漏。 */
const RESULT_META_FIELDS = {
  load_productflow_skill: ["skill_name", "resource_path", "instruction_excerpt", "instruction_truncated"],
  ask_user: ["question_id"],
  productflow_context_injection: ["response_format"],
  get_product_workflow_context_v1: ["response_format"],
  inspect_workflow_runs_v1: [],
  list_product_image_assets_v2: [],
  inspect_product_image_assets_v1: ["asset_count"],
  request_workflow_run_v1: ["pending_confirmation", "product_id", "workflow_id", "workflow_title", "summary", "request_id", "expected_workflow_revision", "reconciled"],
  request_global_workflow_run_v1: ["pending_confirmation", "product_id", "workflow_id", "workflow_title", "summary", "request_id", "expected_workflow_revision", "reconciled"],
  finalize_product_intake_v1: ["node_count", "group_count", "reconciled"],
  list_products_v1: [],
  inspect_products_v1: [],
  inspect_global_workflow_context_v1: ["response_format"],
  inspect_global_workflow_runs_v1: [],
  list_global_media_library_assets_v1: [],
  inspect_global_media_library_assets_v1: ["asset_count"],
  create_product_workspace_v1: ["product_workspace_created", "reconciled"],
  propose_global_draft: ["artifact_name", "pending_confirmation"],
  get_node_detail_v1: [],
  get_workflow_run_detail_v1: [],
  apply_graph_change_set_v1: ["item_count", "operation_summaries", "affected_node_ids", "affected_edge_ids", "affected_group_ids", "reconciled"],
  propose_graph_change_set_v1: ["pending_confirmation", "proposal_id", "summary", "item_count", "operation_summaries", "affected_node_ids", "affected_edge_ids", "affected_group_ids", "reconciled"],
  discard_workflow_proposal_v1: ["proposal_id", "reconciled"],
  cancel_workflow_run_v1: ["run_id", "reconciled"],
  focus_canvas_items_v1: ["affected_node_ids", "affected_edge_ids", "affected_group_ids"],
} as const satisfies Record<ToolName, readonly ResultMetaField[]>;

const resultMetaSchemas = new Map<ToolName, TSchema>();

function resultMetaSchemaFor(name: ToolName): TSchema {
  const cached = resultMetaSchemas.get(name);
  if (cached) return cached;
  const properties: Record<string, TSchema> = {
    schema_version: Type.Literal(1),
    kind: Type.String({ minLength: 1, maxLength: 64 }),
    truncated: resultMetaFields.truncated,
    original_bytes: resultMetaFields.original_bytes,
    max_bytes: resultMetaFields.max_bytes,
  };
  for (const field of RESULT_META_FIELDS[name]) properties[field] = resultMetaFields[field];
  const schema = Type.Object(properties, { additionalProperties: false });
  resultMetaSchemas.set(name, schema);
  return schema;
}

interface ToolManifestShape {
  name: ToolName;
  version: number;
  description: string;
  scope: ToolScope;
  effect: ToolEffect;
  ui_kind: string;
  truncation_bytes: number;
  result_reducer: ToolResultReducer;
  input_schema: TSchema;
  input_schema_source?: ToolInputSchemaSource;
  result_meta_schema: TSchema;
}

export const TOOL_MANIFEST = [
  {
    name: "load_productflow_skill",
    version: 1,
    description: "Load one exact, versioned ProductFlow Skill or static reference. It cannot access business data or execute code.",
    scope: "both",
    effect: "read",
    ui_kind: "load_skill",
    truncation_bytes: 12_288,
    result_reducer: "none",
    input_schema: TOOL_PARAMETER_SCHEMAS.load_productflow_skill,
    result_meta_schema: resultMetaSchemaFor("load_productflow_skill"),
  },
  {
    name: "ask_user",
    version: 1,
    description: "Ask one focused clarification question when a missing fact or explicit choice changes the result.",
    scope: "both",
    effect: "approval",
    ui_kind: "ask_question",
    truncation_bytes: 2_048,
    result_reducer: "none",
    input_schema: TOOL_PARAMETER_SCHEMAS.ask_user,
    result_meta_schema: resultMetaSchemaFor("ask_user"),
  },
  {
    name: "productflow_context_injection",
    version: 1,
    description: "Synthetic runtime step for the bounded context injected before the model turn.",
    scope: "synthetic",
    effect: "read",
    ui_kind: "inject_context",
    truncation_bytes: 32 * 1024,
    result_reducer: "context",
    input_schema: TOOL_PARAMETER_SCHEMAS.productflow_context_injection,
    result_meta_schema: resultMetaSchemaFor("productflow_context_injection"),
  },
  {
    name: "get_product_workflow_context_v1",
    version: 1,
    description: "Read current bounded product facts, intake, live graph summary, and Node Catalog index. Default response_format is concise (config_field_keys). Pass detailed for full config_fields and graph topology.",
    scope: "product",
    effect: "read",
    ui_kind: "inspect_context",
    truncation_bytes: 96 * 1024,
    result_reducer: "context",
    input_schema: TOOL_PARAMETER_SCHEMAS.get_product_workflow_context_v1,
    result_meta_schema: resultMetaSchemaFor("get_product_workflow_context_v1"),
  },
  {
    name: "inspect_workflow_runs_v1",
    version: 1,
    description: "Read a bounded list of recent WorkflowRun and WorkflowNodeRun status summaries for the current workflow.",
    scope: "product",
    effect: "read",
    ui_kind: "read_history",
    truncation_bytes: 96 * 1024,
    result_reducer: "list",
    input_schema: TOOL_PARAMETER_SCHEMAS.inspect_workflow_runs_v1,
    result_meta_schema: resultMetaSchemaFor("inspect_workflow_runs_v1"),
  },
  {
    name: "list_product_image_assets_v2",
    version: 1,
    description: "List one bounded page of image metadata from the current product. This never returns image bytes or URLs.",
    scope: "product",
    effect: "read",
    ui_kind: "inspect_context",
    truncation_bytes: 96 * 1024,
    result_reducer: "list",
    input_schema: TOOL_PARAMETER_SCHEMAS.list_product_image_assets_v2,
    result_meta_schema: resultMetaSchemaFor("list_product_image_assets_v2"),
  },
  {
    name: "inspect_product_image_assets_v1",
    version: 1,
    description: "Inspect up to six explicitly selected product image assets as bounded native multimodal content.",
    scope: "product",
    effect: "read",
    ui_kind: "inspect_image",
    truncation_bytes: 96 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.inspect_product_image_assets_v1,
    result_meta_schema: resultMetaSchemaFor("inspect_product_image_assets_v1"),
  },
  {
    name: "request_workflow_run_v1",
    version: 1,
    description: "Create a pending request for the current workflow. ProductFlow requires human confirmation and never starts a WorkflowRun directly.",
    scope: "product",
    effect: "approval",
    ui_kind: "request_workflow_run",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.request_workflow_run_v1,
    result_meta_schema: resultMetaSchemaFor("request_workflow_run_v1"),
  },
  {
    name: "request_global_workflow_run_v1",
    version: 1,
    description: "Create a pending request for one explicitly selected product workflow. ProductFlow requires human confirmation.",
    scope: "global",
    effect: "approval",
    ui_kind: "request_workflow_run",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.request_global_workflow_run_v1,
    result_meta_schema: resultMetaSchemaFor("request_global_workflow_run_v1"),
  },
  {
    name: "finalize_product_intake_v1",
    version: 1,
    description: "Persist the product intake and expand a name-only live graph into the photography and infographic template. This does not start a run.",
    scope: "product",
    effect: "mutate",
    ui_kind: "expand_intake",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.finalize_product_intake_v1,
    result_meta_schema: resultMetaSchemaFor("finalize_product_intake_v1"),
  },
  {
    name: "list_products_v1",
    version: 1,
    description: "List one bounded page of ProductFlow products and active workflow summaries. This is read-only.",
    scope: "global",
    effect: "read",
    ui_kind: "inspect_context",
    truncation_bytes: 96 * 1024,
    result_reducer: "list",
    input_schema: TOOL_PARAMETER_SCHEMAS.list_products_v1,
    result_meta_schema: resultMetaSchemaFor("list_products_v1"),
  },
  {
    name: "inspect_products_v1",
    version: 1,
    description: "Inspect up to twenty explicitly selected products and their active workflow summaries. This is read-only.",
    scope: "global",
    effect: "read",
    ui_kind: "inspect_context",
    truncation_bytes: 96 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.inspect_products_v1,
    result_meta_schema: resultMetaSchemaFor("inspect_products_v1"),
  },
  {
    name: "inspect_global_workflow_context_v1",
    version: 1,
    description: "Read bounded live-graph context for one explicit product, including intake and Node Catalog index. Default response_format is concise; pass detailed for full config_fields and topology.",
    scope: "global",
    effect: "read",
    ui_kind: "inspect_context",
    truncation_bytes: 96 * 1024,
    result_reducer: "context",
    input_schema: TOOL_PARAMETER_SCHEMAS.inspect_global_workflow_context_v1,
    result_meta_schema: resultMetaSchemaFor("inspect_global_workflow_context_v1"),
  },
  {
    name: "inspect_global_workflow_runs_v1",
    version: 1,
    description: "Inspect recent WorkflowRun status summaries for explicitly selected workflows. This never starts, cancels, or retries a run.",
    scope: "global",
    effect: "read",
    ui_kind: "read_history",
    truncation_bytes: 96 * 1024,
    result_reducer: "list",
    input_schema: TOOL_PARAMETER_SCHEMAS.inspect_global_workflow_runs_v1,
    result_meta_schema: resultMetaSchemaFor("inspect_global_workflow_runs_v1"),
  },
  {
    name: "list_global_media_library_assets_v1",
    version: 1,
    description: "List one bounded page of canonical global media-library metadata. This never returns image bytes or URLs.",
    scope: "global",
    effect: "read",
    ui_kind: "organize_assets",
    truncation_bytes: 96 * 1024,
    result_reducer: "list",
    input_schema: TOOL_PARAMETER_SCHEMAS.list_global_media_library_assets_v1,
    result_meta_schema: resultMetaSchemaFor("list_global_media_library_assets_v1"),
  },
  {
    name: "inspect_global_media_library_assets_v1",
    version: 1,
    description: "Inspect up to six explicitly selected global media assets as bounded native multimodal content.",
    scope: "global",
    effect: "read",
    ui_kind: "inspect_image",
    truncation_bytes: 96 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.inspect_global_media_library_assets_v1,
    result_meta_schema: resultMetaSchemaFor("inspect_global_media_library_assets_v1"),
  },
  {
    name: "create_product_workspace_v1",
    version: 1,
    description: "Create one ProductFlow product with a live canvas session. This does not upload images, write intake, or start a run.",
    scope: "global",
    effect: "mutate",
    ui_kind: "create_product",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.create_product_workspace_v1,
    result_meta_schema: resultMetaSchemaFor("create_product_workspace_v1"),
  },
  {
    name: "propose_global_draft",
    version: 1,
    description: "Submit one complete schema-valid library-organization draft for review. The backend validates it and the user must confirm it.",
    scope: "global",
    effect: "approval",
    ui_kind: "propose_draft",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.propose_global_draft,
    input_schema_source: "contract:draft_schema",
    result_meta_schema: resultMetaSchemaFor("propose_global_draft"),
  },
  {
    name: "get_node_detail_v1",
    version: 1,
    description: "Read one live-graph node's config, incoming and outgoing edges, and a bounded current artifact summary.",
    scope: "product",
    effect: "read",
    ui_kind: "inspect_context",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.get_node_detail_v1,
    result_meta_schema: resultMetaSchemaFor("get_node_detail_v1"),
  },
  {
    name: "get_workflow_run_detail_v1",
    version: 1,
    description: "Read one explicitly selected WorkflowRun with bounded node status and failure summaries. This is read-only.",
    scope: "both",
    effect: "read",
    ui_kind: "read_history",
    truncation_bytes: 64 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.get_workflow_run_detail_v1,
    result_meta_schema: resultMetaSchemaFor("get_workflow_run_detail_v1"),
  },
  {
    name: "apply_graph_change_set_v1",
    version: 1,
    description: "Apply exactly one reversible Graph Command to the live schema-v3 graph.",
    scope: "product",
    effect: "mutate",
    ui_kind: "apply_graph",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.apply_graph_change_set_v1,
    result_meta_schema: resultMetaSchemaFor("apply_graph_change_set_v1"),
  },
  {
    name: "propose_graph_change_set_v1",
    version: 1,
    description: "Store an unapplied GraphProposal overlay. The user confirms or discards it on the canvas.",
    scope: "product",
    effect: "approval",
    ui_kind: "propose_graph",
    truncation_bytes: 32 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.propose_graph_change_set_v1,
    result_meta_schema: resultMetaSchemaFor("propose_graph_change_set_v1"),
  },
  {
    name: "discard_workflow_proposal_v1",
    version: 1,
    description: "Discard the pending GraphProposal overlay on the live canvas. Omit proposal_id to discard the current proposal.",
    scope: "product",
    effect: "mutate",
    ui_kind: "discard_proposal",
    truncation_bytes: 16 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.discard_workflow_proposal_v1,
    result_meta_schema: resultMetaSchemaFor("discard_workflow_proposal_v1"),
  },
  {
    name: "cancel_workflow_run_v1",
    version: 1,
    description: "Cancel one live-graph WorkflowRun that is still running. Already cancelled runs succeed idempotently.",
    scope: "product",
    effect: "mutate",
    ui_kind: "cancel_run",
    truncation_bytes: 16 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.cancel_workflow_run_v1,
    result_meta_schema: resultMetaSchemaFor("cancel_workflow_run_v1"),
  },
  {
    name: "focus_canvas_items_v1",
    version: 1,
    description: "Request a bounded canvas focus for live-graph nodes, edges, or groups. This only changes the UI selection.",
    scope: "product",
    effect: "ui_effect",
    ui_kind: "focus_canvas",
    truncation_bytes: 8 * 1024,
    result_reducer: "detail",
    input_schema: TOOL_PARAMETER_SCHEMAS.focus_canvas_items_v1,
    result_meta_schema: resultMetaSchemaFor("focus_canvas_items_v1"),
  },
] as const satisfies readonly ToolManifestShape[];

export type ToolManifestEntry = (typeof TOOL_MANIFEST)[number];
export type ToolStepKind = ToolManifestEntry["ui_kind"];
export const TOOL_STEP_KINDS = [...new Set(TOOL_MANIFEST.map((entry) => entry.ui_kind))] as readonly ToolStepKind[];

const TOOL_BY_NAME = new Map<string, ToolManifestEntry>(TOOL_MANIFEST.map((entry) => [entry.name, entry]));

function manifestPayload(dynamicSchemas: Readonly<Partial<Record<ToolName, TSchema>>> = {}): unknown {
  return TOOL_MANIFEST.map((entry) => ({
    ...entry,
    recovery_policy: TOOL_RECOVERY_POLICIES[entry.name],
    input_schema: dynamicSchemas[entry.name] ?? entry.input_schema,
  }));
}

export function toolManifestVersion(dynamicSchemas: Readonly<Partial<Record<ToolName, TSchema>>> = {}): string {
  return createHash("sha256").update(JSON.stringify(manifestPayload(dynamicSchemas))).digest("hex");
}

export const TOOL_MANIFEST_VERSION = toolManifestVersion();

/** 会话合同哈希：静态清单版本叠上该会话的 draft_schema。 */
export function resolvedToolContractVersion(draftSchema: unknown = {}): string {
  return createHash("sha256")
    .update(TOOL_MANIFEST_VERSION)
    .update("\n")
    .update(canonicalJSON(draftSchema ?? {}))
    .digest("hex");
}

export function canonicalJSON(value: unknown): string {
  return encodeCanonical(value);
}

function encodeCanonical(value: unknown): string {
  if (value === null || value === undefined) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") {
    if (!Number.isFinite(value)) return "null";
    return JSON.stringify(value);
  }
  if (typeof value === "string") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(encodeCanonical).join(",")}]`;
  if (typeof value === "object") {
    const keys = Object.keys(value).sort();
    const record = value as Record<string, unknown>;
    return `{${keys.map((key) => `${JSON.stringify(key)}:${encodeCanonical(record[key])}`).join(",")}}`;
  }
  return "null";
}

export function toolKind(name: string): ToolStepKind {
  const exact = TOOL_BY_NAME.get(name);
  if (exact) return exact.ui_kind;
  throw new Error(`Unknown ProductFlow tool manifest entry: ${name}`);
}

export function toolManifestEntry(name: string): ToolManifestEntry | undefined {
  return TOOL_BY_NAME.get(name);
}

export function toolRecoveryPolicy(name: ToolName): ToolRecoveryPolicy {
  return TOOL_RECOVERY_POLICIES[name];
}

export function toolDescription<Name extends ToolName>(name: Name): string {
  const entry = TOOL_BY_NAME.get(name);
  if (!entry) throw new Error(`Unknown ProductFlow tool manifest entry: ${name}`);
  return entry.description;
}

export function toolParameters<Name extends ToolName>(name: Name): (typeof TOOL_PARAMETER_SCHEMAS)[Name];
export function toolParameters(name: "propose_global_draft", resolvedSchema: TSchema): TSchema;
export function toolParameters(name: string, resolvedSchema?: TSchema): TSchema {
  const entry = TOOL_BY_NAME.get(name);
  if (!entry) throw new Error(`Unknown ProductFlow tool manifest entry: ${name}`);
  if (resolvedSchema !== undefined) {
    if (name !== "propose_global_draft") throw new Error(`Only propose_global_draft accepts a runtime input schema`);
    return resolvedSchema;
  }
  return entry.input_schema;
}

export function expectedToolNamesForScope(scope: "global" | "product_workflow", hasLiveGraph: boolean): string[] {
  const liveGraph = new Set<string>(LIVE_GRAPH_TOOL_NAMES);
  return TOOL_MANIFEST
    .filter((entry) => {
      if (entry.scope === "synthetic") return false;
      if (liveGraph.has(entry.name) && !hasLiveGraph) return false;
      if (entry.scope === "both") return true;
      if (scope === "global") return entry.scope === "global";
      return entry.scope === "product";
    })
    .map((entry) => entry.name);
}

/** 既检查实现未声明，也检查指定作用域的清单项没有漏注册。 */
export function assertToolManifestCoverage(toolNames: readonly string[], expectedNames?: readonly string[]): void {
  const seen = new Set<string>();
  for (const name of toolNames) {
    if (seen.has(name)) throw new Error(`Duplicate ProductFlow tool registration: ${name}`);
    seen.add(name);
    if (!TOOL_BY_NAME.has(name)) throw new Error(`ProductFlow tool is missing from manifest: ${name}`);
  }
  const expected = expectedNames ?? TOOL_MANIFEST.filter((entry) => entry.scope !== "synthetic").map((entry) => entry.name);
  for (const name of expected) {
    if (!seen.has(name)) throw new Error(`ProductFlow manifest tool is not registered: ${name}`);
  }
}

export function validateToolResultMeta(name: string, value: unknown): void {
  const entry = toolManifestEntry(name);
  if (!entry || !Value.Check(entry.result_meta_schema, value)) {
    throw new Error(`Invalid ProductFlow result metadata for ${name}`);
  }
}
