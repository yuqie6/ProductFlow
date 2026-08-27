/**
 * 暴露给 Pi 的 ProductFlow 工具。
 *
 * 业务变更走 ProductFlow HTTP，并写 intent/result checkpoint。
 * 无法对账的 5xx 记 unknown，不能猜成 failed。
 * `load_productflow_skill` 只读打包好的说明，不碰业务数据。
 */

import {
  defineTool,
  type AgentToolResult,
  type ToolDefinition,
} from "@earendil-works/pi-coding-agent";
import { Type, type TSchema } from "typebox";
import type { ImageContent } from "@earendil-works/pi-ai/compat";
import {
  CheckpointKind,
  JsonObject,
  MAX_PRODUCT_CONTEXT_BYTES,
  ProductFlowError,
  Scope,
  TurnAnswer,
  TurnArtifact,
  TurnQuestion,
  ToolStepDetails,
  toolKind,
} from "./contracts.js";
import {
  PreparedWorkflowRunRequest,
  ProductFlowClient,
  ReconcileResult,
} from "./productflow.js";
import { PRODUCTFLOW_SKILL_TOOL_NAME } from "./skills.js";

const MAX_INSPECTED_ASSETS = 6;
const MAX_TOTAL_IMAGE_BYTES = 20 << 20;
const MAX_TOOL_TEXT_BYTES = 96 << 10;
const MAX_SKILL_INSTRUCTION_EXCERPT_BYTES = 12 << 10;
const MAX_LISTED_ASSETS = 100;
const MAX_LISTED_WORKFLOW_RUNS = 20;
const MAX_GLOBAL_PRODUCTS = 100;
const MAX_GLOBAL_PRODUCT_INSPECTION = 20;
const MAX_GLOBAL_WORKFLOW_INSPECTION = 20;
const MAX_GLOBAL_WORKFLOW_RUNS = 10;

const EMPTY_OBJECT = Type.Object({}, { additionalProperties: false });
const pagination = (maximum: number) =>
  Type.Object(
    {
      query: Type.String({ maxLength: 255 }),
      cursor: Type.String({ maxLength: 4096 }),
      limit: Type.Integer({ minimum: 1, maximum }),
    },
    { additionalProperties: false },
  );

/** 工具层用来写 checkpoint、等待用户、以及标记无法证明副作用的回调。 */
export interface ToolRuntime {
  readonly client: ProductFlowClient;
  readonly scope: Scope;
  readonly pageType?: string | null;
  readonly signal: AbortSignal;
  loadSkill(name: string, resourcePath?: string): Promise<string>;
  recordToolFailure(toolCallID: string, details: ToolStepDetails): void;
  askUser(question: TurnQuestion): Promise<TurnAnswer>;
  proposeArtifact(artifact: TurnArtifact): Promise<void>;
  markWorkflowRunRequested(): void;
  checkpoint(kind: CheckpointKind, payload: JsonObject): Promise<void>;
  markEffectUnknown(toolCallID: string, reason?: string): void;
  idempotencyKey(toolCallID: string): string;
}

type Result = AgentToolResult<JsonObject>;

/** 为当前 Turn 组装 Pi 工具列表；工具只能看见本 runtime 的 scope。 */
export function createProductFlowTools(runtime: ToolRuntime): ToolDefinition[] {
  const tools: ToolDefinition[] = [
    defineTool({
      name: PRODUCTFLOW_SKILL_TOOL_NAME,
      label: "Load ProductFlow skill",
      description:
        "Load one exact, versioned ProductFlow Skill when its description matches the task. This reads only packaged instructions or static references; it cannot access business data, storage, providers, databases, or execute scripts.",
      promptSnippet: "Load matching ProductFlow Skill instructions",
      parameters: Type.Object(
        {
          skill_name: Type.String({ minLength: 1, maxLength: 64 }),
          resource_path: Type.Optional(Type.String({ maxLength: 256 })),
        },
        { additionalProperties: false },
      ),
      execute: async (
        _toolCallID: string,
        params: { skill_name: string; resource_path?: string },
      ): Promise<Result> => {
        const name = params.skill_name.trim();
        const resourcePath = params.resource_path?.trim() || undefined;
        const content = await runtime.loadSkill(name, resourcePath);
        const instructionDetails = buildSkillInstructionDetails(content);
        return {
          content: [
            {
              type: "text",
              text: `ProductFlow Skill: ${name}${resourcePath ? `\nResource: ${resourcePath}` : ""}\n\n${content}`,
            },
          ],
          details: {
            skill_name: name,
            ...(resourcePath ? { resource_path: resourcePath } : {}),
            ...instructionDetails,
          },
        };
      },
    }),
    defineTool({
      name: "ask_user",
      label: "Ask user",
      description:
        "Ask one focused clarification question when a missing product fact or explicit choice changes the result. Stop and wait for the user's answer.",
      promptSnippet: "Ask one bounded clarification question",
      parameters: Type.Object(
        {
          header: Type.String({ minLength: 1, maxLength: 32 }),
          question: Type.String({ minLength: 1, maxLength: 2000 }),
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
      ),
      execute: async (
        _toolCallID: string,
        params: { header: string; question: string; options: Array<{ label: string; description?: string }> },
      ): Promise<Result> => {
        const question: TurnQuestion = {
          id: `question_${crypto.randomUUID()}`,
          header: params.header.trim(),
          question: params.question.trim(),
          options: params.options.map((option) => ({
            label: option.label.trim(),
            ...(option.description?.trim() ? { description: option.description.trim() } : {}),
          })),
        };
        const answer = await runtime.askUser(question);
        return textResult({ accepted: true, answer }, { question_id: question.id });
      },
    }),
    defineTool({
      name: "get_product_workflow_context_v1",
      label: "Read product context",
      description:
        "Read current bounded product facts, intake, live graph summary, reference asset IDs, and the Node Catalog config_fields document. Inspector forms and node config writes use this same catalog. This is read-only.",
      promptSnippet: "Read current product, workflow facts, and node catalog",
      parameters: EMPTY_OBJECT,
      execute: async (): Promise<Result> =>
        productContextResult(await runtime.client.productContext(runtime.scope.conversation_id, runtime.signal)),
    }),
    defineTool({
      name: "inspect_workflow_runs_v1",
      label: "Inspect workflow runs",
      description:
        "Read a bounded list of recent WorkflowRun and WorkflowNodeRun statuses for the current product workflow. This never starts, cancels, or retries a run.",
      parameters: Type.Object(
        { limit: Type.Integer({ minimum: 1, maximum: MAX_LISTED_WORKFLOW_RUNS }) },
        { additionalProperties: false },
      ),
      execute: async (_toolCallID: string, params: { limit: number }): Promise<Result> =>
        textResult(await runtime.client.workflowRuns(runtime.scope.conversation_id, params.limit, runtime.signal)),
    }),
    defineTool({
      name: "list_product_image_assets_v2",
      label: "List product images",
      description:
        "List one bounded page of image metadata from the current product. This never returns image bytes or URLs.",
      promptSnippet: "List bounded product image metadata",
      parameters: Type.Object(
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
          directory_key: Type.Union([Type.String({ maxLength: 120 }), Type.Null()]),
          query: Type.String({ maxLength: 255 }),
          sort: Type.Union([
            Type.Literal("created_desc"),
            Type.Literal("created_asc"),
            Type.Literal("name_asc"),
            Type.Literal("name_desc"),
          ]),
          after: Type.String({ maxLength: 1024 }),
          limit: Type.Integer({ minimum: 1, maximum: MAX_LISTED_ASSETS }),
        },
        { additionalProperties: false },
      ),
      execute: async (
        _toolCallID: string,
        params: { directory_kind: string; directory_key: string | null; query: string; sort: string; after: string; limit: number },
      ): Promise<Result> =>
        textResult(
          await runtime.client.listAssets(
            runtime.scope.conversation_id,
            { ...params, directory_key: params.directory_key },
            runtime.signal,
          ),
        ),
    }),
    createImageInspectionTool(runtime, false),
    createWorkflowRunRequestTool(runtime, false),
    createProductIntakeTool(runtime),
  ];

  if (runtime.scope.scope_type === "product_workflow") {
    if (runtime.scope.has_live_graph) {
      tools.push(
        createGetNodeDetailTool(runtime),
        createApplyGraphChangeSetTool(runtime),
        createProposeGraphChangeSetTool(runtime),
        createDiscardWorkflowProposalTool(runtime),
        createCancelWorkflowRunTool(runtime),
        createFocusCanvasItemsTool(runtime),
      );
    }
    return tools.filter((tool) => !tool.name.startsWith("global_"));
  }

  const globalTools: ToolDefinition[] = [
    createGlobalProductListTool(runtime),
    createGlobalProductInspectTool(runtime),
    createGlobalWorkflowContextTool(runtime),
    createGlobalWorkflowRunsTool(runtime),
    createGlobalMediaListTool(runtime),
    createImageInspectionTool(runtime, true),
    createGlobalWorkspaceTool(runtime),
    createWorkflowRunRequestTool(runtime, true),
    createDraftTool(runtime, "propose_global_draft", "Propose global draft", runtime.scope.draft_schema),
    tools.find((tool) => tool.name === PRODUCTFLOW_SKILL_TOOL_NAME)!,
    tools.find((tool) => tool.name === "ask_user")!,
  ];
  return globalTools;
}

function createProductIntakeTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "finalize_product_intake_v1",
    label: "Save product intake",
    description:
      "Persist image types and already-uploaded reference asset IDs as this product's immutable intake. Use after the user sent photos and requirements in this conversation. This does not start a run.",
    parameters: Type.Object(
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
        reference_asset_ids: Type.Array(Type.String({ minLength: 1, maxLength: 64 }), { minItems: 1, maxItems: 6 }),
      },
      { additionalProperties: false },
    ),
    execute: async (
      toolCallID: string,
      params: {
        selection: {
          schema_version: 1;
          delivery_preset_key?: string;
          image_types: Array<{ key: string; quantity: number; order: number }>;
        };
        reference_asset_ids: string[];
      },
    ): Promise<Result> => {
      const idempotencyKey = runtime.idempotencyKey(toolCallID);
      const body = {
        selection: params.selection,
        reference_asset_ids: uniqueIDs(params.reference_asset_ids, 6),
        task_id: runtime.scope.task_id,
      };
      await runtime.checkpoint("tool_effect_intent", {
        tool_name: "finalize_product_intake_v1",
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        selection: body.selection,
        reference_asset_ids: body.reference_asset_ids,
      });
      try {
        const result = await runtime.client.finalizeProductIntake(
          runtime.scope.conversation_id,
          body,
          idempotencyKey,
          runtime.signal,
        );
        await runtime.checkpoint("tool_effect_result", {
          tool_name: "finalize_product_intake_v1",
          tool_call_id: toolCallID,
          idempotency_key: idempotencyKey,
          result: "applied",
        });
        return textResult(result);
      } catch (error) {
        // 客户端 4xx 是已证明的失败。5xx 可能已经生效，必须先对账再决定 failed 还是 unknown。
        if (!(error instanceof ProductFlowError) || error.status < 500) {
          await runtime.checkpoint("tool_effect_result", {
            tool_name: "finalize_product_intake_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "failed",
          });
          throw error;
        }
        let reconciled: ReconcileResult;
        try {
          reconciled = await runtime.client.reconcileProductIntake(
            runtime.scope.conversation_id,
            body,
            idempotencyKey,
            runtime.signal,
          );
        } catch (reconcileError) {
          await recordUnknownEffect(
            runtime,
            toolCallID,
            {
              tool_name: "finalize_product_intake_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "unknown",
              reconciliation_state: "unavailable",
            },
            "Product intake result is unknown",
          );
          throw reconcileError;
        }
        if (reconciled.state === "applied") {
          await runtime.checkpoint("tool_effect_result", {
            tool_name: "finalize_product_intake_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "applied",
          });
          return textResult(reconciled.result ?? { accepted: true, intake_finalized: true });
        }
        if (reconciled.state === "not_applied" || reconciled.state === "conflict") {
          await runtime.checkpoint("tool_effect_result", {
            tool_name: "finalize_product_intake_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "failed",
            reconciliation_state: reconciled.state,
          });
          throw error;
        }
        await recordUnknownEffect(
          runtime,
          toolCallID,
          {
            tool_name: "finalize_product_intake_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "unknown",
            reconciliation_state: reconciled.state,
          },
          "Product intake result is unknown",
        );
        throw error;
      }
    },
  });
}

function createGlobalProductListTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "list_products_v1",
    label: "List products",
    description:
      "List one bounded page of ProductFlow products and active workflow summaries. This is read-only and excludes image URLs and node configuration.",
    parameters: pagination(MAX_GLOBAL_PRODUCTS),
    execute: async (_toolCallID: string, params: { query: string; cursor: string; limit: number }): Promise<Result> =>
      textResult(await runtime.client.listProducts(runtime.scope.conversation_id, params.query, params.cursor, params.limit, runtime.signal)),
  });
}

function createGlobalProductInspectTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "inspect_products_v1",
    label: "Inspect products",
    description:
      "Inspect up to twenty explicitly selected products and their current active workflow summaries. This is read-only.",
    parameters: Type.Object(
      { product_ids: Type.Array(Type.String({ minLength: 1, maxLength: 64 }), { minItems: 1, maxItems: MAX_GLOBAL_PRODUCT_INSPECTION }) },
      { additionalProperties: false },
    ),
    execute: async (_toolCallID: string, params: { product_ids: string[] }): Promise<Result> =>
      textResult(
        await runtime.client.inspectProducts(
          runtime.scope.conversation_id,
          uniqueIDs(params.product_ids, MAX_GLOBAL_PRODUCT_INSPECTION),
          runtime.signal,
        ),
      ),
  });
}

function createGlobalWorkflowContextTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "inspect_global_workflow_context_v1",
    label: "Inspect product workflow",
    description:
      "Read the bounded live-graph context for one explicit product, including intake, reference assets, and Node Catalog config_fields. Inspector forms and node config writes use this same catalog. This is read-only.",
    parameters: Type.Object(
      { product_id: Type.String({ minLength: 1, maxLength: 64 }) },
      { additionalProperties: false },
    ),
    execute: async (_toolCallID: string, params: { product_id: string }): Promise<Result> =>
      productContextResult(
        await runtime.client.globalWorkflowContext(runtime.scope.conversation_id, params.product_id.trim(), runtime.signal),
      ),
  });
}

function createGlobalWorkflowRunsTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "inspect_global_workflow_runs_v1",
    label: "Inspect workflow history",
    description:
      "Inspect recent WorkflowRun status summaries for explicitly selected workflows. This never starts, cancels, or retries a run.",
    parameters: Type.Object(
      {
        workflow_ids: Type.Array(Type.String({ minLength: 1, maxLength: 64 }), { minItems: 1, maxItems: MAX_GLOBAL_WORKFLOW_INSPECTION }),
        limit: Type.Integer({ minimum: 1, maximum: MAX_GLOBAL_WORKFLOW_RUNS }),
      },
      { additionalProperties: false },
    ),
    execute: async (_toolCallID: string, params: { workflow_ids: string[]; limit: number }): Promise<Result> =>
      textResult(
        await runtime.client.inspectGlobalWorkflowRuns(
          runtime.scope.conversation_id,
          uniqueIDs(params.workflow_ids, MAX_GLOBAL_WORKFLOW_INSPECTION),
          params.limit,
          runtime.signal,
        ),
      ),
  });
}

function createGlobalMediaListTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "list_global_media_library_assets_v1",
    label: "List media assets",
    description:
      "List one bounded page of canonical global media-library metadata. This never returns image bytes or URLs.",
    parameters: pagination(MAX_LISTED_ASSETS),
    execute: async (_toolCallID: string, params: { query: string; cursor: string; limit: number }): Promise<Result> =>
      textResult(
        await runtime.client.listGlobalMediaAssets(
          runtime.scope.conversation_id,
          params.query,
          params.cursor,
          params.limit,
          runtime.signal,
        ),
      ),
  });
}

function createImageInspectionTool(runtime: ToolRuntime, global: boolean): ToolDefinition {
  return defineTool({
    name: global ? "inspect_global_media_library_assets_v1" : "inspect_product_image_assets_v1",
    label: global ? "Inspect media images" : "Inspect product images",
    description: global
      ? "Inspect up to six explicitly selected global media assets as bounded native multimodal content."
      : "Inspect up to six explicitly selected product image assets as bounded native multimodal content.",
    parameters: Type.Object(
      { asset_ids: Type.Array(Type.String({ minLength: 1, maxLength: 64 }), { minItems: 1, maxItems: MAX_INSPECTED_ASSETS }) },
      { additionalProperties: false },
    ),
    execute: async (_toolCallID: string, params: { asset_ids: string[] }): Promise<Result> => {
      const assetIDs = uniqueIDs(params.asset_ids, MAX_INSPECTED_ASSETS);
      const metadata = global
        ? await runtime.client.inspectGlobalMediaAssets(runtime.scope.conversation_id, assetIDs, runtime.signal)
        : await runtime.client.inspectAssets(runtime.scope.conversation_id, assetIDs, runtime.signal);
      const byID = new Set(metadataIDs(metadata));
      if (byID.size !== assetIDs.length || assetIDs.some((assetID) => !byID.has(assetID))) {
        throw new Error("ProductFlow returned an incomplete asset inspection result");
      }
      const contents: Array<{ type: "text"; text: string } | ImageContent> = [
        { type: "text", text: boundedJSON(metadata) },
      ];
      let totalBytes = 0;
      for (const assetID of assetIDs) {
        const image = global
          ? await runtime.client.assetContent(runtime.scope.conversation_id, assetID, true, runtime.signal)
          : await runtime.client.assetContent(runtime.scope.conversation_id, assetID, false, runtime.signal);
        totalBytes += image.sizeBytes;
        if (totalBytes > MAX_TOTAL_IMAGE_BYTES) throw new Error("selected asset bytes exceed the tool result limit");
        contents.push({ type: "image", data: image.data, mimeType: image.mediaType });
      }
      return { content: contents, details: { kind: toolKind(global ? "media" : "asset"), asset_count: assetIDs.length } };
    },
  });
}

function createWorkflowRunRequestTool(runtime: ToolRuntime, global: boolean): ToolDefinition {
  const parameters = global
    ? Type.Object(
      {
        product_id: Type.String({ minLength: 1, maxLength: 64 }),
        workflow_id: Type.String({ minLength: 1, maxLength: 64 }),
        expected_workflow_revision: Type.Integer({ minimum: 1 }),
        source_run_id: Type.Optional(Type.String({ minLength: 1, maxLength: 64 })),
      },
      { additionalProperties: false },
    )
    : Type.Object(
      {
        expected_workflow_revision: Type.Integer({ minimum: 1 }),
        source_run_id: Type.Optional(Type.String({ minLength: 1, maxLength: 64 })),
      },
      { additionalProperties: false },
    );
  return defineTool({
    name: "request_workflow_run_v1",
    label: "Request workflow run",
    description: global
      ? "Create a pending request for one explicit product workflow. ProductFlow requires human confirmation; this never starts a WorkflowRun."
      : "Create a pending request for the current workflow. ProductFlow requires human confirmation; this never starts a WorkflowRun.",
    parameters,
    execute: async (toolCallID: string, params: Record<string, unknown>): Promise<Result> => {
      const taskID = runtime.scope.task_id;
      const sourceRunID = typeof params.source_run_id === "string" ? params.source_run_id : null;
      const expectedRevision = Number(params.expected_workflow_revision);
      let prepared: PreparedWorkflowRunRequest;
      if (global) {
        prepared = await runtime.client.prepareGlobalWorkflowRunRequest(
          runtime.scope.conversation_id,
          {
            product_id: String(params.product_id),
            workflow_id: String(params.workflow_id),
            expected_workflow_revision: expectedRevision,
            task_id: taskID,
            source_run_id: sourceRunID,
          },
          runtime.signal,
        );
      } else {
        prepared = await runtime.client.prepareWorkflowRunRequest(
          runtime.scope.conversation_id,
          { expected_workflow_revision: expectedRevision, task_id: taskID, source_run_id: sourceRunID },
          runtime.signal,
        );
      }
      const idempotencyKey = runtime.idempotencyKey(toolCallID);
      await runtime.checkpoint("tool_effect_intent", {
        tool_name: "request_workflow_run_v1",
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        product_id: prepared.product_id,
        task_id: prepared.task_id,
        workflow_id: prepared.workflow_id,
        workflow_revision: prepared.workflow_revision,
        source_run_id: prepared.source_run_id ?? null,
      });
      try {
        const result = global
          ? await runtime.client.executeGlobalWorkflowRunRequest(runtime.scope.conversation_id, prepared, toolCallID, idempotencyKey, runtime.signal)
          : await runtime.client.executeWorkflowRunRequest(runtime.scope.conversation_id, prepared, toolCallID, idempotencyKey, runtime.signal);
        await runtime.checkpoint("external_job_submitted", {
          tool_name: "request_workflow_run_v1",
          tool_call_id: toolCallID,
          idempotency_key: idempotencyKey,
          workflow_id: prepared.workflow_id,
        });
        await runtime.checkpoint("tool_effect_result", {
          tool_name: "request_workflow_run_v1",
          tool_call_id: toolCallID,
          idempotency_key: idempotencyKey,
          result: "applied",
        });
        runtime.markWorkflowRunRequested();
        return { ...textResult(result, { pending_confirmation: true, request_idempotency_key: idempotencyKey }), terminate: true };
      } catch (error) {
        let reconciled: ReconcileResult;
        try {
          reconciled = global
            ? await runtime.client.reconcileWorkflowRunRequest(runtime.scope.conversation_id, prepared, toolCallID, idempotencyKey, true, runtime.signal)
            : await runtime.client.reconcileWorkflowRunRequest(runtime.scope.conversation_id, prepared, toolCallID, idempotencyKey, false, runtime.signal);
        } catch (reconcileError) {
          await recordUnknownEffect(
            runtime,
            toolCallID,
            {
              tool_name: "request_workflow_run_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "unknown",
              reconciliation_state: "unavailable",
            },
            "WorkflowRun request reconciliation failed",
          );
          throw reconcileError;
        }
        if (reconciled.state === "applied") {
          if (reconciled.result === undefined) {
            await recordUnknownEffect(
              runtime,
              toolCallID,
              {
                tool_name: "request_workflow_run_v1",
                tool_call_id: toolCallID,
                idempotency_key: idempotencyKey,
                result: "unknown",
                reconciliation_state: "invalid_applied_result",
              },
              "WorkflowRun request reconciliation returned an incomplete applied result",
            );
            throw new ProductFlowError(502, "reconciliation_invalid", "WorkflowRun request reconciliation returned an incomplete result");
          }
          await runtime.checkpoint("external_job_submitted", {
            tool_name: "request_workflow_run_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            workflow_id: prepared.workflow_id,
          });
          await runtime.checkpoint("tool_effect_result", {
            tool_name: "request_workflow_run_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "applied",
          });
          runtime.markWorkflowRunRequested();
          return { ...textResult(reconciled.result, { pending_confirmation: true, reconciled: true }), terminate: true };
        }
        if (reconciled.state === "unknown") {
          await recordUnknownEffect(
            runtime,
            toolCallID,
            {
              tool_name: "request_workflow_run_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "unknown",
              reconciliation_state: "unknown",
            },
            "WorkflowRun request result is unknown",
          );
        } else if (!isReconcileState(reconciled.state)) {
          await recordUnknownEffect(
            runtime,
            toolCallID,
            {
              tool_name: "request_workflow_run_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "unknown",
              reconciliation_state: "invalid",
            },
            "WorkflowRun request reconciliation returned an unsupported state",
          );
        } else {
          await runtime.checkpoint("tool_effect_result", {
            tool_name: "request_workflow_run_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "failed",
            reconciliation_state: reconciled.state,
          });
        }
        throw error;
      }
    },
  });
}

function createGlobalWorkspaceTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "create_product_workspace_v1",
    label: "Create product workspace",
    description:
      "Create one ProductFlow product with a live canvas session. This does not upload images, write intake, or start a run. After success, send the user to that product workbench conversation to upload references and state requirements there.",
    parameters: Type.Object(
      { name: Type.String({ minLength: 1, maxLength: 255 }) },
      { additionalProperties: false },
    ),
    execute: async (toolCallID: string, params: { name: string }): Promise<Result> => {
      const idempotencyKey = runtime.idempotencyKey(toolCallID);
      await runtime.checkpoint("tool_effect_intent", {
        tool_name: "create_product_workspace_v1",
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        product_name: params.name.trim(),
      });
      let result: unknown;
      try {
        result = await runtime.client.createProductWorkspace(
          runtime.scope.conversation_id,
          params.name.trim(),
          idempotencyKey,
          runtime.signal,
        );
      } catch (error) {
        if (!(error instanceof ProductFlowError) || error.status < 500) {
          await runtime.checkpoint("tool_effect_result", {
            tool_name: "create_product_workspace_v1",
            tool_call_id: toolCallID,
            idempotency_key: idempotencyKey,
            result: "failed",
          });
          throw error;
        }
        let reconciled: ReconcileResult;
        try {
          reconciled = await runtime.client.reconcileProductWorkspace(
            runtime.scope.conversation_id,
            params.name.trim(),
            idempotencyKey,
            runtime.signal,
          );
        } catch (reconcileError) {
          await recordUnknownEffect(
            runtime,
            toolCallID,
            {
              tool_name: "create_product_workspace_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "unknown",
              reconciliation_state: "unavailable",
            },
            "Product workspace creation result is unknown",
          );
          throw reconcileError;
        }
        if (reconciled.state === "unknown") {
          await recordUnknownEffect(
            runtime,
            toolCallID,
            {
              tool_name: "create_product_workspace_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "unknown",
              reconciliation_state: "unknown",
            },
            "Product workspace creation result is unknown",
          );
          throw error;
        } else if (!isReconcileState(reconciled.state)) {
          await recordUnknownEffect(
            runtime,
            toolCallID,
            {
              tool_name: "create_product_workspace_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "unknown",
              reconciliation_state: "invalid",
            },
            "Product workspace reconciliation returned an unsupported state",
          );
          throw error;
        }
        if (reconciled.state !== "applied" || reconciled.result === undefined) {
          if (reconciled.state === "applied") {
            await recordUnknownEffect(
              runtime,
              toolCallID,
              {
                tool_name: "create_product_workspace_v1",
                tool_call_id: toolCallID,
                idempotency_key: idempotencyKey,
                result: "unknown",
                reconciliation_state: "invalid_applied_result",
              },
              "Product workspace reconciliation returned an incomplete applied result",
            );
            throw new ProductFlowError(502, "reconciliation_invalid", "Product workspace reconciliation returned an incomplete result");
          }
          if (isReconcileState(reconciled.state)) {
            await runtime.checkpoint("tool_effect_result", {
              tool_name: "create_product_workspace_v1",
              tool_call_id: toolCallID,
              idempotency_key: idempotencyKey,
              result: "failed",
              reconciliation_state: reconciled.state,
            });
          }
          throw error;
        }
        await runtime.checkpoint("tool_effect_result", {
          tool_name: "create_product_workspace_v1",
          tool_call_id: toolCallID,
          idempotency_key: idempotencyKey,
          result: "applied",
          reconciliation_state: "applied",
        });
        return textResult(reconciled.result, { product_workspace_created: true, reconciled: true });
      }
      await runtime.checkpoint("tool_effect_result", {
        tool_name: "create_product_workspace_v1",
        tool_call_id: toolCallID,
        idempotency_key: idempotencyKey,
        result: "applied",
      });
      return textResult(result, { product_workspace_created: true });
    },
  });
}

const applyGraphChangeSetParameters = Type.Object(
  {
    base_graph_revision: Type.Integer({ minimum: 0 }),
    summary: Type.String({ minLength: 1, maxLength: 500 }),
    operations: Type.Array(Type.Object({}, { additionalProperties: true }), { minItems: 1, maxItems: 1 }),
  },
  { additionalProperties: false },
);

const proposeGraphChangeSetParameters = Type.Object(
  {
    base_graph_revision: Type.Integer({ minimum: 0 }),
    summary: Type.String({ minLength: 1, maxLength: 500 }),
    operations: Type.Array(Type.Object({}, { additionalProperties: true }), { minItems: 1, maxItems: 128 }),
  },
  { additionalProperties: false },
);

const MAX_CANVAS_FOCUS_ITEMS = 20;

function createGetNodeDetailTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "get_node_detail_v1",
    label: "Get node detail",
    description:
      "Read one live-graph node's config, incoming and outgoing edges, and a bounded current artifact summary. Use this before editing or explaining a specific node.",
    parameters: Type.Object(
      { node_id: Type.String({ minLength: 1, maxLength: 80 }) },
      { additionalProperties: false },
    ),
    execute: async (_toolCallID: string, params: { node_id: string }): Promise<Result> =>
      textResult(await runtime.client.getNodeDetail(runtime.scope.conversation_id, params.node_id, runtime.signal)),
  });
}

function createApplyGraphChangeSetTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "apply_graph_change_set_v1",
    label: "Apply graph change",
    description:
      "Apply one reversible Graph Command to the live schema-v3 graph. operations must contain exactly one edit such as updating one node, connecting or disconnecting one edge, or renaming. Do not use this for multi-node reconstructs or bulk deletes.",
    parameters: applyGraphChangeSetParameters,
    execute: async (toolCallID: string, params: { base_graph_revision: number; summary: string; operations: object[] }): Promise<Result> =>
      executeGraphMutationTool(runtime, {
        toolCallID,
        toolName: "apply_graph_change_set_v1",
        params: params as JsonObject,
        mutate: (idempotencyKey) =>
          runtime.client.applyGraphChangeSet(runtime.scope.conversation_id, params as JsonObject, idempotencyKey, runtime.signal),
        reconcile: (idempotencyKey) =>
          runtime.client.reconcileApplyGraphChangeSet(
            runtime.scope.conversation_id,
            params as JsonObject,
            idempotencyKey,
            runtime.signal,
          ),
        unknownReason: "Graph apply result is unknown",
      }),
  });
}

function createProposeGraphChangeSetTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "propose_graph_change_set_v1",
    label: "Propose graph change",
    description:
      "Store an unapplied GraphProposal overlay on the live canvas. Use for multi-node reconstructs, bulk deletes, or preset overlays. The proposal cannot run. The user confirms or discards it on the canvas.",
    parameters: proposeGraphChangeSetParameters,
    execute: async (toolCallID: string, params: { base_graph_revision: number; summary: string; operations: object[] }): Promise<Result> =>
      executeGraphMutationTool(runtime, {
        toolCallID,
        toolName: "propose_graph_change_set_v1",
        params: params as JsonObject,
        mutate: (idempotencyKey) =>
          runtime.client.proposeGraphChangeSet(runtime.scope.conversation_id, params as JsonObject, idempotencyKey, runtime.signal),
        reconcile: (idempotencyKey) =>
          runtime.client.reconcileProposeGraphChangeSet(
            runtime.scope.conversation_id,
            params as JsonObject,
            idempotencyKey,
            runtime.signal,
          ),
        unknownReason: "Graph proposal result is unknown",
        extraDetails: { pending_confirmation: true },
      }),
  });
}

function createDiscardWorkflowProposalTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "discard_workflow_proposal_v1",
    label: "Discard graph proposal",
    description:
      "Discard the pending GraphProposal overlay on the live canvas. Does not write the live graph. Omit proposal_id to discard the current pending proposal.",
    parameters: Type.Object(
      { proposal_id: Type.Optional(Type.String({ minLength: 1, maxLength: 64 })) },
      { additionalProperties: false },
    ),
    execute: async (toolCallID: string, params: { proposal_id?: string }): Promise<Result> =>
      executeGraphMutationTool(runtime, {
        toolCallID,
        toolName: "discard_workflow_proposal_v1",
        params: params as JsonObject,
        mutate: (idempotencyKey) =>
          runtime.client.discardGraphProposal(runtime.scope.conversation_id, params.proposal_id ?? null, idempotencyKey, runtime.signal),
        reconcile: (idempotencyKey) =>
          runtime.client.reconcileDiscardGraphProposal(
            runtime.scope.conversation_id,
            params.proposal_id ?? null,
            idempotencyKey,
            runtime.signal,
          ),
        unknownReason: "Graph proposal discard result is unknown",
      }),
  });
}

function createCancelWorkflowRunTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "cancel_workflow_run_v1",
    label: "Cancel workflow run",
    description:
      "Cancel one live-graph WorkflowGraphRun that is still running. Already cancelled runs succeed idempotently. Terminal succeeded or failed runs cannot be cancelled.",
    parameters: Type.Object(
      { run_id: Type.String({ minLength: 1, maxLength: 64 }) },
      { additionalProperties: false },
    ),
    execute: async (toolCallID: string, params: { run_id: string }): Promise<Result> =>
      executeGraphMutationTool(runtime, {
        toolCallID,
        toolName: "cancel_workflow_run_v1",
        params: params as JsonObject,
        mutate: (idempotencyKey) =>
          runtime.client.cancelWorkflowRun(runtime.scope.conversation_id, params.run_id, idempotencyKey, runtime.signal),
        reconcile: (idempotencyKey) =>
          runtime.client.reconcileCancelWorkflowRun(
            runtime.scope.conversation_id,
            params.run_id,
            idempotencyKey,
            runtime.signal,
          ),
        unknownReason: "Workflow run cancel result is unknown",
      }),
  });
}

function createFocusCanvasItemsTool(runtime: ToolRuntime): ToolDefinition {
  return defineTool({
    name: "focus_canvas_items_v1",
    label: "Focus canvas items",
    description:
      "Record a bounded canvas focus request. The workbench selects those live-graph nodes, edges, or groups. At least one id is required. Do not invent a second graph.",
    parameters: Type.Object(
      {
        node_ids: Type.Optional(Type.Array(Type.String({ minLength: 1, maxLength: 80 }), { maxItems: MAX_CANVAS_FOCUS_ITEMS })),
        edge_ids: Type.Optional(Type.Array(Type.String({ minLength: 1, maxLength: 80 }), { maxItems: MAX_CANVAS_FOCUS_ITEMS })),
        group_ids: Type.Optional(Type.Array(Type.String({ minLength: 1, maxLength: 80 }), { maxItems: MAX_CANVAS_FOCUS_ITEMS })),
      },
      { additionalProperties: false },
    ),
    execute: async (
      toolCallID: string,
      params: { node_ids?: string[]; edge_ids?: string[]; group_ids?: string[] },
    ): Promise<Result> => {
      const body = {
        node_ids: uniqueIDs(params.node_ids ?? [], MAX_CANVAS_FOCUS_ITEMS),
        edge_ids: uniqueIDs(params.edge_ids ?? [], MAX_CANVAS_FOCUS_ITEMS),
        group_ids: uniqueIDs(params.group_ids ?? [], MAX_CANVAS_FOCUS_ITEMS),
      };
      return executeGraphMutationTool(runtime, {
        toolCallID,
        toolName: "focus_canvas_items_v1",
        params: body,
        mutate: (idempotencyKey) =>
          runtime.client.focusCanvasItems(runtime.scope.conversation_id, body, idempotencyKey, runtime.signal),
        reconcile: (idempotencyKey) =>
          runtime.client.reconcileFocusCanvasItems(runtime.scope.conversation_id, body, idempotencyKey, runtime.signal),
        unknownReason: "Canvas focus result is unknown",
      });
    },
  });
}

async function executeGraphMutationTool(
  runtime: ToolRuntime,
  args: {
    toolCallID: string;
    toolName: string;
    params: JsonObject;
    mutate: (idempotencyKey: string) => Promise<JsonObject>;
    reconcile: (idempotencyKey: string) => Promise<ReconcileResult>;
    unknownReason: string;
    extraDetails?: JsonObject;
  },
): Promise<Result> {
  const idempotencyKey = runtime.idempotencyKey(args.toolCallID);
  await runtime.checkpoint("tool_effect_intent", {
    tool_name: args.toolName,
    tool_call_id: args.toolCallID,
    idempotency_key: idempotencyKey,
    change_set: args.params,
  });
  try {
    const result = await args.mutate(idempotencyKey);
    await runtime.checkpoint("tool_effect_result", {
      tool_name: args.toolName,
      tool_call_id: args.toolCallID,
      idempotency_key: idempotencyKey,
      result: "applied",
    });
    return textResult(result, args.extraDetails);
  } catch (error) {
    if (!(error instanceof ProductFlowError) || error.status < 500) {
      await runtime.checkpoint("tool_effect_result", {
        tool_name: args.toolName,
        tool_call_id: args.toolCallID,
        idempotency_key: idempotencyKey,
        result: "failed",
      });
      throw error;
    }
    let reconciled: ReconcileResult;
    try {
      reconciled = await args.reconcile(idempotencyKey);
    } catch (reconcileError) {
      await recordUnknownEffect(
        runtime,
        args.toolCallID,
        {
          tool_name: args.toolName,
          tool_call_id: args.toolCallID,
          idempotency_key: idempotencyKey,
          result: "unknown",
          reconciliation_state: "unavailable",
        },
        args.unknownReason,
      );
      throw reconcileError;
    }
    if (reconciled.state === "applied") {
      await runtime.checkpoint("tool_effect_result", {
        tool_name: args.toolName,
        tool_call_id: args.toolCallID,
        idempotency_key: idempotencyKey,
        result: "applied",
      });
      return textResult((reconciled.result as JsonObject) ?? { accepted: true }, args.extraDetails);
    }
    if (reconciled.state === "not_applied") {
      await runtime.checkpoint("tool_effect_result", {
        tool_name: args.toolName,
        tool_call_id: args.toolCallID,
        idempotency_key: idempotencyKey,
        result: "failed",
        reconciliation_state: reconciled.state,
      });
      throw error;
    }
    await recordUnknownEffect(
      runtime,
      args.toolCallID,
      {
        tool_name: args.toolName,
        tool_call_id: args.toolCallID,
        idempotency_key: idempotencyKey,
        result: "unknown",
        reconciliation_state: reconciled.state,
      },
      args.unknownReason,
    );
    throw error;
  }
}

function createDraftTool(
  runtime: ToolRuntime,
  name: "propose_global_draft",
  label: string,
  schema: JsonObject,
): ToolDefinition {
  return defineTool({
    name,
    label,
    description:
      "Submit one complete schema-valid library-organization draft for review. The backend validates it and the user must confirm it. Product workflow topology is not accepted.",
    parameters: schema as TSchema,
    execute: async (toolCallID: string, params: JsonObject): Promise<Result> => {
      try {
        await runtime.client.validateGlobalDraft(runtime.scope.conversation_id, params, runtime.signal);
      } catch (error) {
        runtime.recordToolFailure(toolCallID, toolFailureDetails(error));
        throw error;
      }
      await runtime.proposeArtifact({ name, value: params, step_id: toolCallID });
      return { ...textResult({ accepted: true, pending_confirmation: true }, { artifact_name: name }), terminate: true };
    },
  });
}

function textResult(value: unknown, details: JsonObject = {}): Result {
  return {
    content: [{ type: "text", text: boundedJSON(value) }],
    details,
  };
}

function productContextResult(value: unknown): Result {
  return {
    content: [{ type: "text", text: boundedProductContextJSON(value) }],
    details: {},
  };
}

function boundedJSON(value: unknown): string {
  const encoded = encodeJSON(value);
  if (Buffer.byteLength(encoded, "utf8") <= MAX_TOOL_TEXT_BYTES) return encoded;
  return truncatedJSON(Buffer.byteLength(encoded, "utf8"), MAX_TOOL_TEXT_BYTES);
}

function boundedProductContextJSON(value: unknown): string {
  const encoded = encodeJSON(value);
  const originalBytes = Buffer.byteLength(encoded, "utf8");
  if (originalBytes <= MAX_PRODUCT_CONTEXT_BYTES) return encoded;

  const nodeCatalog = isRecord(value) ? value.node_catalog : undefined;
  if (nodeCatalog !== undefined) {
    const reduced = {
      schema_version: isRecord(value) && typeof value.schema_version === "number" ? value.schema_version : 1,
      truncated: true,
      original_bytes: originalBytes,
      max_bytes: MAX_PRODUCT_CONTEXT_BYTES,
      node_catalog: nodeCatalog,
    };
    const reducedEncoded = encodeJSON(reduced);
    if (Buffer.byteLength(reducedEncoded, "utf8") <= MAX_PRODUCT_CONTEXT_BYTES) return reducedEncoded;
  }
  return truncatedJSON(originalBytes, MAX_PRODUCT_CONTEXT_BYTES);
}

function encodeJSON(value: unknown): string {
  try {
    return JSON.stringify(value) ?? "null";
  } catch {
    return JSON.stringify({
      schema_version: 1,
      error: "unserializable ProductFlow result",
    });
  }
}

function truncatedJSON(originalBytes: number, maximumBytes: number): string {
  return JSON.stringify({
    schema_version: 1,
    truncated: true,
    original_bytes: originalBytes,
    max_bytes: maximumBytes,
  });
}

function buildSkillInstructionDetails(content: string): Pick<ToolStepDetails, "instruction_excerpt" | "instruction_truncated"> {
  const fullBytes = Buffer.byteLength(content, "utf8");
  if (fullBytes <= MAX_SKILL_INSTRUCTION_EXCERPT_BYTES) {
    return { instruction_excerpt: content, instruction_truncated: false };
  }

  const suffix = "\n...[truncated]";
  const availableBytes = MAX_SKILL_INSTRUCTION_EXCERPT_BYTES - Buffer.byteLength(suffix, "utf8");
  let low = 0;
  let high = content.length;
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    if (Buffer.byteLength(content.slice(0, middle), "utf8") <= availableBytes) low = middle;
    else high = middle - 1;
  }
  return {
    instruction_excerpt: `${content.slice(0, low)}${suffix}`,
    instruction_truncated: true,
  };
}

function toolFailureDetails(error: unknown): ToolStepDetails {
  if (!(error instanceof ProductFlowError)) {
    return {
      phase: "tool_result",
      error_message: "工具调用失败，详见当前 Turn 错误。",
    };
  }
  const issuesValue = error.details?.issues;
  const validationIssues = Array.isArray(issuesValue)
    ? issuesValue.flatMap((issue) => {
      if (
        !issue ||
        typeof issue !== "object" ||
        Array.isArray(issue) ||
        typeof (issue as { path?: unknown }).path !== "string" ||
        typeof (issue as { message?: unknown }).message !== "string"
      ) {
        return [];
      }
      return [
        {
          path: boundedDetailText((issue as { path: string }).path, 200, "$"),
          message: boundedDetailText((issue as { message: string }).message, 500, "ProductFlow validation failed"),
        },
      ];
    })
    : undefined;
  return {
    phase: "tool_result",
    error_code: boundedDetailText(error.code, 120, "productflow_error"),
    error_message: boundedDetailText(error.message, 1000, "工具调用失败，详见当前 Turn 错误。"),
    retryable: error.status >= 500,
    ...(validationIssues?.length ? { validation_issues: validationIssues.slice(0, 8) } : {}),
  };
}

function boundedDetailText(value: string, maximum: number, fallback: string): string {
  const normalized = value.replace(/[\r\n]+/gu, " ").trim();
  return (normalized || fallback).slice(0, maximum);
}

function isReconcileState(value: string): value is "applied" | "not_applied" | "conflict" | "unknown" {
  return value === "applied" || value === "not_applied" || value === "conflict" || value === "unknown";
}

/** 先写 unknown checkpoint 再中止；checkpoint 写失败也仍然中止。 */
async function recordUnknownEffect(
  runtime: ToolRuntime,
  toolCallID: string,
  payload: JsonObject,
  reason: string,
): Promise<void> {
  try {
    await runtime.checkpoint("tool_effect_result", payload);
  } catch {
    // checkpoint 写不进去时，unknown 标记本身仍是权威。
  } finally {
    runtime.markEffectUnknown(toolCallID, reason);
  }
}

function uniqueIDs(values: string[], maximum: number): string[] {
  const result = [...new Set(values.map((value) => value.trim()).filter(Boolean))];
  if (result.length !== values.length || result.length > maximum) throw new Error("asset or product IDs must be unique and within the limit");
  return result;
}

function metadataIDs(value: unknown): string[] {
  const items = Array.isArray(value)
    ? value
    : value && typeof value === "object" && Array.isArray((value as { items?: unknown }).items)
      ? (value as { items: unknown[] }).items
      : [];
  return items.flatMap((item) => {
    if (!item || typeof item !== "object" || typeof (item as { id?: unknown }).id !== "string") return [];
    return [(item as { id: string }).id];
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}
