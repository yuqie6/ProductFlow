import { Type, type Static } from "typebox";

const jsonValueSchema = Type.Unknown();
const nonEmptyStringSchema = Type.String({ minLength: 1 });
const scopeSchema = Type.Union([Type.Literal("product_workflow"), Type.Literal("global")]);
const suiteSchema = Type.Union([
  Type.Literal("regression"),
  Type.Literal("capability"),
  Type.Literal("adversarial"),
]);
const caseTypeSchema = Type.Union([Type.Literal("positive"), Type.Literal("negative")]);
const layerSchema = Type.Union([
  Type.Literal("l0"),
  Type.Literal("l1"),
  Type.Literal("l2"),
  Type.Literal("l3"),
  Type.Literal("l4"),
  Type.Literal("l5"),
  Type.Literal("l6"),
]);
const terminalSchema = Type.Union([
  Type.Literal("queued"),
  Type.Literal("running"),
  Type.Literal("requires_input"),
  Type.Literal("awaiting_confirmation"),
  Type.Literal("succeeded"),
  Type.Literal("failed"),
  Type.Literal("cancel_requested"),
  Type.Literal("canceled"),
  Type.Literal("unknown"),
]);

export const EvalCallRecordSchema = Type.Object(
  {
    name: nonEmptyStringSchema,
    params: jsonValueSchema,
    ts: nonEmptyStringSchema,
    outcome: Type.Union([Type.Literal("succeeded"), Type.Literal("failed"), Type.Literal("unknown")]),
    observed_injections: Type.Optional(Type.Array(nonEmptyStringSchema)),
  },
  { additionalProperties: false },
);

export const EvalTrialGraderSchema = Type.Object(
  {
    passed: Type.Boolean(),
    failure_categories: Type.Array(nonEmptyStringSchema, { uniqueItems: true }),
    results: Type.Record(
      Type.String(),
      Type.Object(
        {
          passed: Type.Boolean(),
          errors: Type.Array(Type.String()),
        },
        { additionalProperties: false },
      ),
    ),
  },
  { additionalProperties: false },
);

export const EvalReferenceCallSchema = Type.Object(
  { name: nonEmptyStringSchema, params: jsonValueSchema },
  { additionalProperties: false },
);

const pageContextSchema = Type.Object(
  {
    snapshot_id: nonEmptyStringSchema,
    route: nonEmptyStringSchema,
    page_type: nonEmptyStringSchema,
    product_id: Type.Union([nonEmptyStringSchema, Type.Null()]),
    workflow_id: Type.Union([nonEmptyStringSchema, Type.Null()]),
    selected_asset_ids: Type.Array(nonEmptyStringSchema),
    visible_asset_ids: Type.Array(nonEmptyStringSchema),
    filters: Type.Record(Type.String(), Type.String()),
    workflow_revision: Type.Union([Type.Integer({ minimum: 0 }), Type.Null()]),
    library_revision: Type.Union([Type.Integer({ minimum: 0 }), Type.Null()]),
    digest: Type.String({ pattern: "^[0-9a-f]{64}$" }),
    captured_at: nonEmptyStringSchema,
  },
  { additionalProperties: false },
);

const writeExpectationSchema = Type.Object(
  {
    tool: nonEmptyStringSchema,
    match: Type.Record(Type.String(), jsonValueSchema),
  },
  { additionalProperties: false },
);

export const EvalStateExpectSchema = Type.Object(
  {
    node_titles: Type.Optional(Type.Record(Type.String(), Type.String())),
    min_node_count: Type.Optional(Type.Integer({ minimum: 0 })),
    min_revision: Type.Optional(Type.Integer({ minimum: 0 })),
    pending_proposals: Type.Optional(Type.Integer({ minimum: 0 })),
    pending_run_requests: Type.Optional(Type.Integer({ minimum: 0 })),
    require_source_run_id: Type.Optional(Type.Boolean()),
    intake_image_type_keys: Type.Optional(Type.Array(Type.String())),
    pending_library_drafts: Type.Optional(Type.Integer({ minimum: 0 })),
    failed_run_present: Type.Optional(Type.Boolean()),
    product_name_contains: Type.Optional(Type.String()),
  },
  { additionalProperties: false },
);

export const EvalUserSimSchema = Type.Object(
  {
    persona: nonEmptyStringSchema,
    hidden_goal: nonEmptyStringSchema,
    facts: Type.Record(Type.String(), Type.String()),
    policy: nonEmptyStringSchema,
    max_turns: Type.Integer({ minimum: 1, maximum: 12 }),
    scripted_answers: Type.Array(
      Type.Object(
        {
          when: Type.Union([
            Type.Literal("question"),
            Type.Literal("proposal"),
            Type.Literal("run_request"),
            Type.Literal("library_draft"),
            Type.Literal("follow_up"),
          ]),
          action: Type.Union([
            Type.Literal("answer"),
            Type.Literal("confirm"),
            Type.Literal("discard"),
            Type.Literal("follow_up"),
          ]),
          text: Type.Optional(Type.String()),
          answer: Type.Optional(jsonValueSchema),
          authorize_writes: Type.Optional(Type.Boolean()),
        },
        { additionalProperties: false },
      ),
    ),
  },
  { additionalProperties: false },
);

export const EvalExpectSchema = Type.Object(
  {
    terminal: Type.Array(terminalSchema, { minItems: 1, uniqueItems: true }),
    tools: Type.Object(
      {
        required: Type.Array(nonEmptyStringSchema, { uniqueItems: true }),
        forbidden: Type.Array(nonEmptyStringSchema, { uniqueItems: true }),
        reads: Type.Optional(Type.Array(writeExpectationSchema)),
      },
      { additionalProperties: false },
    ),
    ops: Type.Object(
      {
        required: Type.Array(nonEmptyStringSchema, { uniqueItems: true }),
        forbidden: Type.Array(nonEmptyStringSchema, { uniqueItems: true }),
      },
      { additionalProperties: false },
    ),
    writes: Type.Array(writeExpectationSchema),
    state: Type.Optional(EvalStateExpectSchema),
    question: Type.Optional(
      Type.Object({ required: Type.Optional(Type.Boolean()) }, { additionalProperties: true }),
    ),
    budget: Type.Optional(
      Type.Object(
        {
          max_tool_calls: Type.Optional(Type.Integer({ minimum: 1 })),
          max_tokens: Type.Optional(Type.Integer({ minimum: 1 })),
          max_duration_ms: Type.Optional(Type.Integer({ minimum: 1 })),
        },
        { additionalProperties: false },
      ),
    ),
    rubric: Type.Optional(jsonValueSchema),
  },
  { additionalProperties: false },
);

const repairSchema = Type.Object(
  {
    tool_name: nonEmptyStringSchema,
    illegal_params: jsonValueSchema,
    repaired_params: jsonValueSchema,
  },
  { additionalProperties: false },
);

export const EvalTaskSchema = Type.Object(
  {
    schema_version: Type.Literal(1),
    id: Type.String({ minLength: 1, maxLength: 120, pattern: "^[a-z0-9]+(?:-[a-z0-9]+)*$" }),
    skill: nonEmptyStringSchema,
    scope: scopeSchema,
    suite: suiteSchema,
    case_type: caseTypeSchema,
    layers: Type.Array(layerSchema, { minItems: 1, uniqueItems: true }),
    split: Type.Optional(Type.Union([Type.Literal("held_in"), Type.Literal("held_out")])),
    utterances: Type.Array(nonEmptyStringSchema, { minItems: 1 }),
    world: nonEmptyStringSchema,
    page_context: pageContextSchema,
    expect: EvalExpectSchema,
    origin: nonEmptyStringSchema,
    observability_blocker: Type.Optional(nonEmptyStringSchema),
    reference: Type.Object(
      {
        scripted_calls: Type.Array(EvalReferenceCallSchema, { minItems: 1 }),
        repair: Type.Optional(repairSchema),
      },
      { additionalProperties: false },
    ),
    inject: Type.Optional(
      Type.Object(
        {
          first_write_409: Type.Optional(nonEmptyStringSchema),
          write_409_count: Type.Optional(Type.Integer({ minimum: 1, maximum: 8 })),
          read_error: Type.Optional(
            Type.Object(
              {
                tool: nonEmptyStringSchema,
                status: Type.Union([Type.Literal(500), Type.Literal("timeout")]),
              },
              { additionalProperties: false },
            ),
          ),
          payload: Type.Optional(
            Type.Object(
              {
                display_name: Type.Optional(Type.String()),
                product_name: Type.Optional(Type.String()),
                node_title: Type.Optional(Type.String()),
                failure_reason: Type.Optional(Type.String()),
                folder_title: Type.Optional(Type.String()),
              },
              { additionalProperties: false },
            ),
          ),
        },
        { additionalProperties: false },
      ),
    ),
    user_sim: Type.Optional(EvalUserSimSchema),
  },
  { additionalProperties: false },
);

const graphNodeSchema = Type.Object(
  {
    id: nonEmptyStringSchema,
    node_type: nonEmptyStringSchema,
    title: Type.String(),
    config: Type.Optional(Type.Record(Type.String(), jsonValueSchema)),
  },
  { additionalProperties: false },
);

export const EvalWorldSchema = Type.Object(
  {
    schema_version: Type.Literal(1),
    name: Type.String({ minLength: 1, maxLength: 120, pattern: "^[a-z0-9]+(?:-[a-z0-9]+)*$" }),
    intake: Type.Union([Type.Record(Type.String(), jsonValueSchema), Type.Null()]),
    birth_expandable: Type.Boolean(),
    pending_proposal_id: Type.Optional(nonEmptyStringSchema),
    listed_workflow_runs: Type.Optional(Type.Array(Type.Object({
      workflow_id: nonEmptyStringSchema,
      workflow_title: nonEmptyStringSchema,
      workflow_revision: Type.Integer({ minimum: 0 }),
      items: Type.Array(Type.Object({ id: nonEmptyStringSchema, status: nonEmptyStringSchema }, { additionalProperties: false })),
    }, { additionalProperties: false }))),
    live_graph: Type.Object(
      {
        id: nonEmptyStringSchema,
        title: Type.String(),
        revision: Type.Integer({ minimum: 0 }),
        node_count: Type.Integer({ minimum: 0 }),
        edge_count: Type.Integer({ minimum: 0 }),
        group_count: Type.Integer({ minimum: 0 }),
        nodes: Type.Array(graphNodeSchema),
        edges: Type.Optional(
          Type.Array(
            Type.Object(
              {
                id: nonEmptyStringSchema,
                source_id: nonEmptyStringSchema,
                target_id: nonEmptyStringSchema,
                role: nonEmptyStringSchema,
                order: Type.Integer({ minimum: 0 }),
              },
              { additionalProperties: false },
            ),
          ),
        ),
        groups: Type.Optional(
          Type.Array(
            Type.Object(
              {
                id: nonEmptyStringSchema,
                title: Type.String(),
                member_ids: Type.Array(nonEmptyStringSchema),
              },
              { additionalProperties: false },
            ),
          ),
        ),
      },
      { additionalProperties: false },
    ),
    failed_run: Type.Optional(
      Type.Object(
        {
          id: nonEmptyStringSchema,
          status: Type.Literal("failed"),
          failed_node_id: nonEmptyStringSchema,
        },
        { additionalProperties: false },
      ),
    ),
    recent_run: Type.Optional(
      Type.Object(
        {
          id: nonEmptyStringSchema,
          status: Type.Union([
            Type.Literal("queued"),
            Type.Literal("running"),
            Type.Literal("succeeded"),
            Type.Literal("failed"),
            Type.Literal("cancelled"),
            Type.Literal("unknown"),
          ]),
        },
        { additionalProperties: false },
      ),
    ),
    listed_assets: Type.Optional(
      Type.Array(
        Type.Object(
          {
            id: nonEmptyStringSchema,
            display_name: Type.String(),
            revision: Type.Optional(Type.Integer({ minimum: 0 })),
            folder_id: Type.Optional(Type.Union([nonEmptyStringSchema, Type.Null()])),
            tag_names: Type.Optional(Type.Array(Type.String())),
            is_archived: Type.Optional(Type.Boolean()),
          },
          { additionalProperties: false },
        ),
      ),
    ),
    listed_folders: Type.Optional(
      Type.Array(
        Type.Object(
          { id: nonEmptyStringSchema, title: Type.String() },
          { additionalProperties: false },
        ),
      ),
    ),
  },
  { additionalProperties: false },
);

export const EvalTrialRecordSchema = Type.Object(
  {
    schema_version: Type.Literal(1),
    run_id: nonEmptyStringSchema,
    layer: layerSchema,
    task_id: nonEmptyStringSchema,
    skill: nonEmptyStringSchema,
    suite: suiteSchema,
    split: Type.Optional(Type.Union([Type.Literal("held_in"), Type.Literal("held_out")])),
    trial: Type.Integer({ minimum: 1 }),
    utterance: nonEmptyStringSchema,
    started_at: nonEmptyStringSchema,
    duration_ms: Type.Number({ minimum: 0 }),
    status: nonEmptyStringSchema,
    passed: Type.Boolean(),
    errors: Type.Array(Type.String()),
    terminal: Type.Union([terminalSchema, Type.Null()]),
    tool_calls: Type.Array(EvalCallRecordSchema),
    token_count: Type.Union([Type.Number({ minimum: 0 }), Type.Null()]),
    grader: Type.Optional(EvalTrialGraderSchema),
    details: Type.Optional(jsonValueSchema),
    transcript_path: Type.Optional(nonEmptyStringSchema),
  },
  { additionalProperties: false },
);

export type EvalCallRecord = Static<typeof EvalCallRecordSchema>;
export type EvalReferenceCall = Static<typeof EvalReferenceCallSchema>;
export type EvalExpect = Static<typeof EvalExpectSchema>;
export type EvalStateExpect = Static<typeof EvalStateExpectSchema>;
export type EvalUserSim = Static<typeof EvalUserSimSchema>;
export type EvalTask = Static<typeof EvalTaskSchema>;
export type EvalWorld = Static<typeof EvalWorldSchema>;
export type EvalTrialRecord = Static<typeof EvalTrialRecordSchema>;
export type EvalTrialGrader = Static<typeof EvalTrialGraderSchema>;

/** TypeBox documents are JSON Schema. Go loaders treat this as the shared contract. */
export function evalJSONSchemas(): { task: unknown; world: unknown; trial_record: unknown } {
  return {
    task: EvalTaskSchema,
    world: EvalWorldSchema,
    trial_record: EvalTrialRecordSchema,
  };
}
