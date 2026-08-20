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
  ProductFlowError,
  Scope,
  TurnAnswer,
  TurnArtifact,
  TurnQuestion,
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
const MAX_LISTED_ASSETS = 100;
const MAX_LISTED_ARCHIVES = 50;
const MAX_INSPECTED_ARCHIVE_ITEMS = 10;
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

export interface ToolRuntime {
  readonly client: ProductFlowClient;
  readonly scope: Scope;
  readonly signal: AbortSignal;
  loadSkill(name: string, resourcePath?: string): Promise<string>;
  askUser(question: TurnQuestion): Promise<TurnAnswer>;
  proposeArtifact(artifact: TurnArtifact): Promise<void>;
  markWorkflowRunRequested(): void;
  checkpoint(kind: CheckpointKind, payload: JsonObject): Promise<void>;
  markEffectUnknown(toolCallID: string, reason?: string): void;
  idempotencyKey(toolCallID: string): string;
}

type Result = AgentToolResult<JsonObject>;

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
        return {
          content: [
            {
              type: "text",
              text: `ProductFlow Skill: ${name}${resourcePath ? `\nResource: ${resourcePath}` : ""}\n\n${content}`,
            },
          ],
          details: { skill_name: name, ...(resourcePath ? { resource_path: resourcePath } : {}) },
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
        "Read current bounded product facts, WorkflowDraft summary, missing information, and reference asset IDs. This is read-only.",
      promptSnippet: "Read current product and workflow facts",
      parameters: EMPTY_OBJECT,
      execute: async (): Promise<Result> => textResult(await runtime.client.productContext(runtime.scope.conversation_id, runtime.signal)),
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
      name: "list_legacy_archives_v1",
      label: "List legacy history",
      description:
        "List one bounded page of legacy archive metadata. Payloads, image bytes, URLs, and run details are excluded.",
      parameters: Type.Object(
        {
          kind: Type.Union([Type.Literal("workflow"), Type.Literal("canvas_agent_thread"), Type.Literal("user_template")]),
          query: Type.String({ maxLength: 255 }),
          after: Type.String({ maxLength: 4096 }),
          limit: Type.Integer({ minimum: 1, maximum: MAX_LISTED_ARCHIVES }),
        },
        { additionalProperties: false },
      ),
      execute: async (
        _toolCallID: string,
        params: { kind: string; query: string; after: string; limit: number },
      ): Promise<Result> => textResult(await runtime.client.listLegacyArchives(runtime.scope.conversation_id, params, runtime.signal)),
    }),
    defineTool({
      name: "inspect_legacy_archive_v1",
      label: "Inspect legacy history",
      description:
        "Inspect one explicit bounded section of one legacy archive. Request only the section needed for the current redesign.",
      parameters: Type.Object(
        {
          kind: Type.Union([Type.Literal("workflow"), Type.Literal("canvas_agent_thread"), Type.Literal("user_template")]),
          archive_id: Type.String({ minLength: 1, maxLength: 64 }),
          section: Type.String({ minLength: 1, maxLength: 64 }),
          offset: Type.Integer({ minimum: 0 }),
          limit: Type.Integer({ minimum: 1, maximum: MAX_INSPECTED_ARCHIVE_ITEMS }),
        },
        { additionalProperties: false },
      ),
      execute: async (
        _toolCallID: string,
        params: { kind: string; archive_id: string; section: string; offset: number; limit: number },
      ): Promise<Result> => textResult(await runtime.client.inspectLegacyArchive(runtime.scope.conversation_id, params, runtime.signal)),
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
  ];

  if (runtime.scope.scope_type === "product_workflow") {
    if (runtime.scope.task_id === null) {
      tools.push(
        createDraftTool(runtime, "propose_workflow_draft", "Propose workflow draft", runtime.scope.workflow_draft_schema, false),
      );
    }
    return tools.filter((tool) => !tool.name.startsWith("global_"));
  }

  return [
    createGlobalProductListTool(runtime),
    createGlobalProductInspectTool(runtime),
    createGlobalWorkflowContextTool(runtime),
    createGlobalWorkflowRunsTool(runtime),
    createGlobalMediaListTool(runtime),
    createImageInspectionTool(runtime, true),
    createGlobalWorkspaceTool(runtime),
    createWorkflowRunRequestTool(runtime, true),
    createDraftTool(runtime, "propose_global_draft", "Propose global draft", runtime.scope.draft_schema, true),
    tools.find((tool) => tool.name === PRODUCTFLOW_SKILL_TOOL_NAME)!,
    tools.find((tool) => tool.name === "ask_user")!,
  ];
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
      "Read the bounded editable WorkflowDraft context for one explicit product, including the current revision and reference assets. This is read-only.",
    parameters: Type.Object(
      { product_id: Type.String({ minLength: 1, maxLength: 64 }) },
      { additionalProperties: false },
    ),
    execute: async (_toolCallID: string, params: { product_id: string }): Promise<Result> =>
      textResult(await runtime.client.globalWorkflowContext(runtime.scope.conversation_id, params.product_id.trim(), runtime.signal)),
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
      "Create one blank ProductFlow product onboarding workspace. This does not upload images, submit intake, materialize a workflow, or start a run.",
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

function createDraftTool(
  runtime: ToolRuntime,
  name: "propose_workflow_draft" | "propose_global_draft",
  label: string,
  schema: JsonObject,
  global: boolean,
): ToolDefinition {
  return defineTool({
    name,
    label,
    description: global
      ? "Submit one complete schema-valid global ProductFlow draft for review. The backend validates it and the user must confirm it."
      : "Submit one complete schema-valid ProductFlow WorkflowDraft for review. The backend validates it and the user must confirm it.",
    parameters: schema as TSchema,
    execute: async (toolCallID: string, params: JsonObject): Promise<Result> => {
      if (global) await runtime.client.validateGlobalDraft(runtime.scope.conversation_id, params, runtime.signal);
      else await runtime.client.validateWorkflowDraft(runtime.scope.conversation_id, params, runtime.signal);
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

function boundedJSON(value: unknown): string {
  let encoded: string;
  try {
    encoded = JSON.stringify(value) ?? "null";
  } catch {
    encoded = "[unserializable ProductFlow result]";
  }
  if (Buffer.byteLength(encoded, "utf8") <= MAX_TOOL_TEXT_BYTES) return encoded;
  return `${Buffer.from(encoded, "utf8").subarray(0, MAX_TOOL_TEXT_BYTES).toString("utf8")}...[truncated]`;
}

function isReconcileState(value: string): value is "applied" | "not_applied" | "conflict" | "unknown" {
  return value === "applied" || value === "not_applied" || value === "conflict" || value === "unknown";
}

async function recordUnknownEffect(
  runtime: ToolRuntime,
  toolCallID: string,
  payload: JsonObject,
  reason: string,
): Promise<void> {
  try {
    await runtime.checkpoint("tool_effect_result", payload);
  } catch {
    // The unknown marker remains authoritative when checkpoint persistence is unavailable.
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
