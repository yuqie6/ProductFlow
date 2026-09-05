/**
 * 暴露给 Pi 的 ProductFlow 工具。
 *
 * 名称、描述、schema、效果等级来自 tool-manifest.ts。
 * 业务变更走 ProductFlow HTTP；mutate 经 withEffect 统一对账。
 * `load_productflow_skill` 只读打包好的说明，不碰业务数据。
 */

import {
  defineTool,
  type AgentToolResult,
  type ToolDefinition,
} from "@earendil-works/pi-coding-agent";
import type { TSchema } from "typebox";
import type { ImageContent } from "@earendil-works/pi-ai/compat";
import {
  CheckpointKind,
  JsonObject,
  ProductFlowError,
  questionAnswerToolPayload,
  Scope,
  TurnAnswer,
  TurnQuestion,
  ToolStepDetails,
  assertToolManifestCoverage,
} from "./contracts.js";
import {
  PreparedWorkflowRunRequest,
  ProductFlowClient,
  workflowRunRequestPayload,
} from "./productflow.js";
import { PRODUCTFLOW_SKILL_TOOL_NAME } from "./skills.js";
import {
  expectedToolNamesForScope,
  toolDescription,
  toolParameters,
  type ToolName,
  type ToolParams,
} from "./tool-manifest.js";
import { withEffect, type EffectRuntime } from "./tool-effect.js";
import { encodeSkillResult, encodeToolResult } from "./tool-result.js";

const MAX_INSPECTED_ASSETS = 6;
const MAX_TOTAL_IMAGE_BYTES = 20 << 20;
const MAX_SKILL_INSTRUCTION_EXCERPT_BYTES = 12 << 10;
const MAX_GLOBAL_PRODUCT_INSPECTION = 20;
const MAX_GLOBAL_WORKFLOW_INSPECTION = 20;
const MAX_CANVAS_FOCUS_ITEMS = 20;

/** 工具层用来写 checkpoint、等待用户、以及标记无法证明副作用的回调。 */
export interface ToolRuntime extends EffectRuntime {
  readonly client: ProductFlowClient;
  readonly scope: Scope;
  readonly pageType?: string | null;
  readonly signal: AbortSignal;
  loadSkill(name: string, resourcePath?: string): Promise<string>;
  recordToolFailure(toolCallID: string, details: ToolStepDetails): void;
  askUser(question: TurnQuestion): Promise<TurnAnswer>;
}

type Result = AgentToolResult<JsonObject>;

/** 为当前 Turn 组装 Pi 工具列表；工具只能看见本 runtime 的 scope。 */
export function createProductFlowTools(runtime: ToolRuntime): ToolDefinition[] {
  const shared: ToolDefinition[] = [createSkillTool(runtime), createAskUserTool(runtime)];
  const tools: ToolDefinition[] = runtime.scope.scope_type === "product_workflow"
    ? [
      ...shared,
      createProductContextTool(runtime),
      createWorkflowRunsTool(runtime),
      createWorkflowRunDetailTool(runtime),
      createProductImageListTool(runtime),
      createImageInspectionTool(runtime, false),
      createWorkflowRunRequestTool(runtime, false),
      createProductIntakeTool(runtime),
      ...(runtime.scope.has_live_graph
        ? [
          createGetNodeDetailTool(runtime),
          createApplyGraphChangeSetTool(runtime),
          createProposeGraphChangeSetTool(runtime),
          createDiscardWorkflowProposalTool(runtime),
          createCancelWorkflowRunTool(runtime),
          createFocusCanvasItemsTool(runtime),
        ]
        : []),
    ]
    : [
      ...shared,
      createGlobalProductListTool(runtime),
      createGlobalProductInspectTool(runtime),
      createGlobalWorkflowContextTool(runtime),
      createGlobalWorkflowRunsTool(runtime),
      createWorkflowRunDetailTool(runtime),
      createGlobalMediaListTool(runtime),
      createImageInspectionTool(runtime, true),
      createGlobalWorkspaceTool(runtime),
      createWorkflowRunRequestTool(runtime, true),
      createDraftTool(runtime, runtime.scope.draft_schema),
    ];
  assertToolManifestCoverage(
    tools.map((tool) => tool.name),
    expectedToolNamesForScope(runtime.scope.scope_type, runtime.scope.has_live_graph),
  );
  return tools;
}

function createSkillTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("load_productflow_skill", {
    label: "Load ProductFlow skill",
    promptSnippet: "Load matching ProductFlow Skill instructions",
    execute: async (_toolCallID, params: ToolParams<"load_productflow_skill">): Promise<Result> => {
      const name = params.skill_name.trim();
      const resourcePath = params.resource_path?.trim() || undefined;
      const content = await runtime.loadSkill(name, resourcePath);
      return encodeSkillResult("load_productflow_skill", `ProductFlow Skill: ${name}${resourcePath ? `\nResource: ${resourcePath}` : ""}\n\n${content}`, {
        skill_name: name,
        ...(resourcePath ? { resource_path: resourcePath } : {}),
        ...buildSkillInstructionDetails(content),
      });
    },
  });
}

function createAskUserTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("ask_user", {
    label: "Ask user",
    promptSnippet: "Ask one bounded clarification question",
    execute: async (_toolCallID, params: ToolParams<"ask_user">): Promise<Result> => {
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
      return encodeToolResult("ask_user", questionAnswerToolPayload(answer), { question_id: question.id });
    },
  });
}

function createProductContextTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("get_product_workflow_context_v1", {
    label: "Read product context",
    promptSnippet: "Read current product, workflow facts, and node catalog",
    execute: async (_toolCallID, params: ToolParams<"get_product_workflow_context_v1"> = {}): Promise<Result> => {
      const responseFormat = params.response_format ?? "concise";
      return encodeToolResult(
        "get_product_workflow_context_v1",
        await runtime.client.productContext(runtime.scope.conversation_id, runtime.signal, responseFormat),
        { response_format: responseFormat },
      );
    },
  });
}

function createWorkflowRunsTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("inspect_workflow_runs_v1", {
    label: "Inspect workflow runs",
    execute: async (_toolCallID, params: ToolParams<"inspect_workflow_runs_v1">): Promise<Result> =>
      encodeToolResult(
        "inspect_workflow_runs_v1",
        await runtime.client.workflowRuns(runtime.scope.conversation_id, params.limit, runtime.signal),
      ),
  });
}

function createProductImageListTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("list_product_image_assets_v2", {
    label: "List product images",
    promptSnippet: "List bounded product image metadata",
    execute: async (_toolCallID, params: ToolParams<"list_product_image_assets_v2">): Promise<Result> =>
      encodeToolResult(
        "list_product_image_assets_v2",
        await runtime.client.listAssets(
          runtime.scope.conversation_id,
          {
            directory_kind: params.directory_kind,
            directory_key: params.directory_key ?? null,
            query: params.query ?? "",
            sort: params.sort ?? "created_desc",
            after: params.after ?? "",
            limit: params.limit,
          },
          runtime.signal,
        ),
      ),
  });
}

function createProductIntakeTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("finalize_product_intake_v1", {
    label: "Save product intake",
    execute: async (toolCallID, params: ToolParams<"finalize_product_intake_v1">): Promise<Result> => {
      const body = {
        selection: params.selection,
        reference_asset_ids: uniqueIDs(params.reference_asset_ids, 6),
        task_id: runtime.scope.task_id,
      };
      return withEffect(runtime, "finalize_product_intake_v1", toolCallID, {
        intentPayload: jsonObject(body),
        mutate: (idempotencyKey) =>
          runtime.client.finalizeProductIntake(runtime.scope.conversation_id, body, idempotencyKey, runtime.signal),
        unknownReason: "Product intake result is unknown",
        resultMeta: intakeResultMeta,
      });
    },
  });
}

function createGlobalProductListTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("list_products_v1", {
    label: "List products",
    execute: async (_toolCallID, params: ToolParams<"list_products_v1">): Promise<Result> =>
      encodeToolResult(
        "list_products_v1",
        await runtime.client.listProducts(
          runtime.scope.conversation_id,
          params.query ?? "",
          params.cursor ?? "",
          params.limit,
          runtime.signal,
        ),
      ),
  });
}

function createGlobalProductInspectTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("inspect_products_v1", {
    label: "Inspect products",
    execute: async (_toolCallID, params: ToolParams<"inspect_products_v1">): Promise<Result> =>
      encodeToolResult(
        "inspect_products_v1",
        await runtime.client.inspectProducts(
          runtime.scope.conversation_id,
          uniqueIDs(params.product_ids, MAX_GLOBAL_PRODUCT_INSPECTION),
          runtime.signal,
        ),
      ),
  });
}

function createGlobalWorkflowContextTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("inspect_global_workflow_context_v1", {
    label: "Inspect product workflow",
    execute: async (_toolCallID, params: ToolParams<"inspect_global_workflow_context_v1">): Promise<Result> => {
      const responseFormat = params.response_format ?? "concise";
      return encodeToolResult(
        "inspect_global_workflow_context_v1",
        await runtime.client.globalWorkflowContext(
          runtime.scope.conversation_id,
          params.product_id.trim(),
          runtime.signal,
          responseFormat,
        ),
        { response_format: responseFormat },
      );
    },
  });
}

function createGlobalWorkflowRunsTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("inspect_global_workflow_runs_v1", {
    label: "Inspect workflow history",
    execute: async (_toolCallID, params: ToolParams<"inspect_global_workflow_runs_v1">): Promise<Result> =>
      encodeToolResult(
        "inspect_global_workflow_runs_v1",
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
  return productFlowTool("list_global_media_library_assets_v1", {
    label: "List media assets",
    execute: async (_toolCallID, params: ToolParams<"list_global_media_library_assets_v1">): Promise<Result> =>
      encodeToolResult(
        "list_global_media_library_assets_v1",
        await runtime.client.listGlobalMediaAssets(
          runtime.scope.conversation_id,
          params.query ?? "",
          params.cursor ?? "",
          params.limit,
          runtime.signal,
          { include_archived: params.include_archived, folder_query: params.folder_query,
            folders_after_id: params.folders_after_id, workflow_id: params.workflow_id },
        ),
      ),
  });
}

function createImageInspectionTool(runtime: ToolRuntime, global: boolean): ToolDefinition {
  const name = global ? "inspect_global_media_library_assets_v1" : "inspect_product_image_assets_v1";
  return productFlowTool(name, {
    label: global ? "Inspect media images" : "Inspect product images",
    execute: async (_toolCallID, params: ToolParams<"inspect_product_image_assets_v1">): Promise<Result> => {
      const assetIDs = uniqueIDs(params.asset_ids, MAX_INSPECTED_ASSETS);
      const metadata = global
        ? await runtime.client.inspectGlobalMediaAssets(runtime.scope.conversation_id, assetIDs, runtime.signal)
        : await runtime.client.inspectAssets(runtime.scope.conversation_id, assetIDs, runtime.signal);
      const byID = new Set(metadataIDs(metadata));
      if (byID.size !== assetIDs.length || assetIDs.some((assetID) => !byID.has(assetID))) {
        throw new Error("ProductFlow returned an incomplete asset inspection result");
      }
      const images: ImageContent[] = [];
      let totalBytes = 0;
      for (const assetID of assetIDs) {
        const image = await runtime.client.assetContent(runtime.scope.conversation_id, assetID, global, runtime.signal);
        totalBytes += image.sizeBytes;
        if (totalBytes > MAX_TOTAL_IMAGE_BYTES) throw new Error("selected asset bytes exceed the tool result limit");
        images.push({ type: "image", data: image.data, mimeType: image.mediaType });
      }
      return encodeToolResult(name, metadata, { asset_count: assetIDs.length }, { images });
    },
  });
}

function createWorkflowRunRequestTool(runtime: ToolRuntime, global: boolean): ToolDefinition {
  const name = global ? "request_global_workflow_run_v1" : "request_workflow_run_v1";
  return productFlowTool(name, {
    label: "Request workflow run",
    execute: async (
      toolCallID,
      params: ToolParams<"request_workflow_run_v1"> | ToolParams<"request_global_workflow_run_v1">,
    ): Promise<Result> => {
      const taskID = runtime.scope.task_id;
      const sourceRunID = params.source_run_id ?? null;
      const expectedRevision = params.expected_workflow_revision;
      const scope = params.scope;
      const nodeID = params.node_id;
      const nodeIDs = params.node_ids;
      const force = params.force;
      const documentAction = params.document_action;
      let prepared: PreparedWorkflowRunRequest = global
        ? await runtime.client.prepareGlobalWorkflowRunRequest(
          runtime.scope.conversation_id,
          {
            product_id: "product_id" in params ? params.product_id : "",
            workflow_id: "workflow_id" in params ? params.workflow_id : "",
            expected_workflow_revision: expectedRevision,
            task_id: taskID,
            source_run_id: sourceRunID,
          },
          runtime.signal,
        )
        : await runtime.client.prepareWorkflowRunRequest(
          runtime.scope.conversation_id,
          { expected_workflow_revision: expectedRevision, task_id: taskID, source_run_id: sourceRunID },
          runtime.signal,
        );
      prepared = { ...prepared, scope, node_id: nodeID, node_ids: nodeIDs, force, document_action: documentAction };
      return withEffect(runtime, name, toolCallID, {
        intentPayload: workflowIntentPayload(prepared, toolCallID, global),
        mutate: (idempotencyKey) =>
          global
            ? runtime.client.executeGlobalWorkflowRunRequest(runtime.scope.conversation_id, prepared, toolCallID, idempotencyKey, runtime.signal)
            : runtime.client.executeWorkflowRunRequest(runtime.scope.conversation_id, prepared, toolCallID, idempotencyKey, runtime.signal),
        unknownReason: "WorkflowRun request result is unknown",
        terminate: true,
        afterAppliedCheckpoints: [
          {
            kind: "external_job_submitted",
            payload: {
              tool_name: name,
              tool_call_id: toolCallID,
              idempotency_key: runtime.idempotencyKey(toolCallID),
              workflow_id: prepared.workflow_id,
            },
          },
        ],
        onApplied: (_result, idempotencyKey) => {
          runtime.requestApproval({
            approval_id: idempotencyKey,
            approval_kind: "workflow_run",
            request_idempotency_key: idempotencyKey,
            workflow_id: prepared.workflow_id,
            task_id: prepared.task_id,
          });
        },
        meta: {
          pending_confirmation: true,
          product_id: prepared.product_id,
          workflow_id: prepared.workflow_id,
          ...(prepared.workflow_title.trim()
            ? { workflow_title: prepared.workflow_title.trim().slice(0, 240) }
            : {}),
          ...(params.scope ? { summary: `${prepared.workflow_title.trim()} · ${params.scope}`.slice(0, 240) } : {}),
        },
        resultMeta: (result) => ({
          ...requestIdMeta(result),
          expected_workflow_revision: expectedRevision,
        }),
      });
    },
  });
}

function createGlobalWorkspaceTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("create_product_workspace_v1", {
    label: "Create product workspace",
    execute: async (toolCallID, params: ToolParams<"create_product_workspace_v1">): Promise<Result> =>
      withEffect(runtime, "create_product_workspace_v1", toolCallID, {
        intentPayload: { name: params.name.trim() },
        mutate: (idempotencyKey) =>
          runtime.client.createProductWorkspace(runtime.scope.conversation_id, params.name.trim(), idempotencyKey, runtime.signal),
        unknownReason: "Product workspace creation result is unknown",
        meta: { product_workspace_created: true },
      }),
  });
}

function createGetNodeDetailTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("get_node_detail_v1", {
    label: "Get node detail",
    execute: async (_toolCallID, params: ToolParams<"get_node_detail_v1">): Promise<Result> =>
      encodeToolResult(
        "get_node_detail_v1",
        await runtime.client.getNodeDetail(runtime.scope.conversation_id, params.node_id, runtime.signal),
      ),
  });
}

function createWorkflowRunDetailTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("get_workflow_run_detail_v1", {
    label: "Get workflow run detail",
    execute: async (_toolCallID, params: ToolParams<"get_workflow_run_detail_v1">): Promise<Result> => {
      return encodeToolResult(
        "get_workflow_run_detail_v1",
        await runtime.client.workflowRunDetail(
          runtime.scope.conversation_id,
          params.run_id.trim(),
          runtime.signal,
        ),
      );
    },
  });
}

function createApplyGraphChangeSetTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("apply_graph_change_set_v1", {
    label: "Apply graph change",
    execute: async (toolCallID, params: ToolParams<"apply_graph_change_set_v1">): Promise<Result> => {
      const body = jsonObject(params);
      return withEffect(runtime, "apply_graph_change_set_v1", toolCallID, {
        intentPayload: { change_set: body },
        mutate: (idempotencyKey) =>
          runtime.client.applyGraphChangeSet(runtime.scope.conversation_id, body, idempotencyKey, runtime.signal),
        unknownReason: "Graph apply result is unknown",
        meta: operationMeta(params.operations),
      });
    },
  });
}

function createProposeGraphChangeSetTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("propose_graph_change_set_v1", {
    label: "Propose graph change",
    execute: async (toolCallID, params: ToolParams<"propose_graph_change_set_v1">): Promise<Result> => {
      const body = jsonObject(params);
      return withEffect(runtime, "propose_graph_change_set_v1", toolCallID, {
        intentPayload: { change_set: body },
        mutate: (idempotencyKey) =>
          runtime.client.proposeGraphChangeSet(runtime.scope.conversation_id, body, idempotencyKey, runtime.signal),
        unknownReason: "Graph proposal result is unknown",
        terminate: true,
        meta: { pending_confirmation: true, ...operationMeta(params.operations) },
        resultMeta: (result) => {
          const record = result && typeof result === "object" && !Array.isArray(result)
            ? result as Record<string, unknown>
            : {};
          const proposalID = typeof record.proposal_id === "string" ? record.proposal_id.trim() : "";
          const summary = typeof record.summary === "string" ? record.summary.trim().slice(0, 240) : "";
          return {
            ...(proposalID ? { proposal_id: proposalID } : {}),
            ...(summary ? { summary } : {}),
          };
        },
        onApplied: (result) => {
          const record = result && typeof result === "object" && !Array.isArray(result)
            ? result as Record<string, unknown>
            : {};
          const proposalID = typeof record.proposal_id === "string" ? record.proposal_id.trim() : "";
          if (!proposalID) return;
          const summary = typeof record.summary === "string" ? record.summary.trim().slice(0, 240) : "";
          runtime.requestApproval({
            approval_id: proposalID,
            approval_kind: "graph_proposal",
            proposal_id: proposalID,
            pending_confirmation: true,
            ...(summary ? { summary } : {}),
            ...operationMeta(params.operations),
          });
        },
      });
    },
  });
}

function createDiscardWorkflowProposalTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("discard_workflow_proposal_v1", {
    label: "Discard graph proposal",
    execute: async (toolCallID, params: ToolParams<"discard_workflow_proposal_v1">): Promise<Result> =>
      withEffect(runtime, "discard_workflow_proposal_v1", toolCallID, {
        intentPayload: jsonObject({ proposal_id: params.proposal_id ?? null }),
        mutate: (idempotencyKey) =>
          runtime.client.discardGraphProposal(runtime.scope.conversation_id, params.proposal_id ?? null, idempotencyKey, runtime.signal),
        unknownReason: "Graph proposal discard result is unknown",
        meta: params.proposal_id ? { proposal_id: params.proposal_id } : {},
      }),
  });
}

function createCancelWorkflowRunTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("cancel_workflow_run_v1", {
    label: "Cancel workflow run",
    execute: async (toolCallID, params: ToolParams<"cancel_workflow_run_v1">): Promise<Result> =>
      withEffect(runtime, "cancel_workflow_run_v1", toolCallID, {
        intentPayload: { run_id: params.run_id },
        mutate: (idempotencyKey) =>
          runtime.client.cancelWorkflowRun(runtime.scope.conversation_id, params.run_id, idempotencyKey, runtime.signal),
        unknownReason: "Workflow run cancel result is unknown",
        meta: { run_id: params.run_id },
      }),
  });
}

function createFocusCanvasItemsTool(runtime: ToolRuntime): ToolDefinition {
  return productFlowTool("focus_canvas_items_v1", {
    label: "Focus canvas items",
    execute: async (toolCallID, params: ToolParams<"focus_canvas_items_v1">): Promise<Result> => {
      const body = {
        node_ids: uniqueIDs(params.node_ids ?? [], MAX_CANVAS_FOCUS_ITEMS),
        edge_ids: uniqueIDs(params.edge_ids ?? [], MAX_CANVAS_FOCUS_ITEMS),
        group_ids: uniqueIDs(params.group_ids ?? [], MAX_CANVAS_FOCUS_ITEMS),
      };
      return withEffect(runtime, "focus_canvas_items_v1", toolCallID, {
        intentPayload: jsonObject(body),
        mutate: (idempotencyKey) =>
          runtime.client.focusCanvasItems(runtime.scope.conversation_id, body, idempotencyKey, runtime.signal),
        unknownReason: "Canvas focus result is unknown",
        meta: {
          affected_node_ids: body.node_ids,
          affected_edge_ids: body.edge_ids,
          affected_group_ids: body.group_ids,
        },
      });
    },
  });
}

function createDraftTool(runtime: ToolRuntime, schema: JsonObject): ToolDefinition {
  return productFlowTool("propose_global_draft", {
    label: "Propose global draft",
    parameters: toolParameters("propose_global_draft", schema as TSchema),
    execute: async (toolCallID, params: ToolParams<"propose_global_draft">): Promise<Result> => {
      try {
        await runtime.client.validateGlobalDraft(runtime.scope.conversation_id, params, runtime.signal);
      } catch (error) {
        runtime.recordToolFailure(toolCallID, toolFailureDetails(error));
        throw error;
      }
      runtime.requestApproval({
        approval_id: toolCallID,
        approval_kind: "artifact",
        artifact: { name: "propose_global_draft", value: params as JsonObject, step_id: toolCallID },
      });
      return {
        ...encodeToolResult("propose_global_draft", { accepted: true, pending_confirmation: true }, {
          artifact_name: "propose_global_draft",
          pending_confirmation: true,
        }),
        terminate: true,
      };
    },
  });
}

function productFlowTool(
  name: ToolName,
  args: {
    label: string;
    promptSnippet?: string;
    parameters?: TSchema;
    execute: ToolDefinition["execute"];
  },
): ToolDefinition {
  return defineTool({
    name,
    label: args.label,
    description: toolDescription(name),
    ...(args.promptSnippet ? { promptSnippet: args.promptSnippet } : {}),
    parameters: args.parameters ?? toolParameters(name),
    execute: args.execute,
  });
}

function operationMeta(operations: ReadonlyArray<{ op: string }>): JsonObject {
  const summaries = operations.map((operation) => operation.op).slice(0, 128);
  const nodes: string[] = [];
  const edges: string[] = [];
  const groups: string[] = [];
  for (const operation of operations) {
    collectOperationRefs(operation as { op: string } & Record<string, unknown>, nodes, edges, groups);
  }
  return {
    operation_summaries: summaries,
    item_count: summaries.length,
    ...optionalRefList("affected_node_ids", nodes),
    ...optionalRefList("affected_edge_ids", edges),
    ...optionalRefList("affected_group_ids", groups),
  };
}

function collectOperationRefs(
  operation: { op: string } & Record<string, unknown>,
  nodes: string[],
  edges: string[],
  groups: string[],
): void {
  switch (operation.op) {
    case "create_node":
      pushRef(nodes, operation.client_ref);
      pushRef(groups, operation.group_ref);
      break;
    case "update_node_config":
    case "rename_node":
    case "delete_node":
      pushRef(nodes, operation.node_ref);
      break;
    case "connect_nodes":
      pushRef(edges, operation.client_ref);
      pushRef(nodes, operation.source_ref);
      pushRef(nodes, operation.target_ref);
      break;
    case "disconnect_edge":
      pushRef(edges, operation.edge_ref);
      break;
    case "move_nodes":
      if (Array.isArray(operation.nodes)) {
        for (const item of operation.nodes) {
          if (Array.isArray(item)) pushRef(nodes, item[0]);
        }
      }
      break;
    case "create_group":
      pushRef(groups, operation.client_ref);
      if (Array.isArray(operation.member_refs)) {
        for (const member of operation.member_refs) pushRef(nodes, member);
      }
      break;
    case "move_nodes_to_group":
      if (Array.isArray(operation.node_refs)) {
        for (const nodeRef of operation.node_refs) pushRef(nodes, nodeRef);
      }
      pushRef(groups, operation.group_ref);
      break;
    case "rename_group":
    case "dissolve_group":
      pushRef(groups, operation.group_ref);
      break;
    case "reorder_edges":
      pushRef(nodes, operation.node_ref);
      if (Array.isArray(operation.edge_refs)) {
        for (const edgeRef of operation.edge_refs) pushRef(edges, edgeRef);
      }
      break;
    default:
      break;
  }
}

function pushRef(target: string[], value: unknown): void {
  if (typeof value !== "string") return;
  const trimmed = value.trim();
  if (!trimmed || trimmed.length > 80) return;
  target.push(trimmed);
}

function optionalRefList(key: string, values: readonly string[]): JsonObject {
  const unique = [...new Set(values)].slice(0, 128);
  return unique.length ? { [key]: unique } : {};
}

function intakeResultMeta(result: unknown): JsonObject {
  return {
    ...optionalCount(result, "node_count", 10_000),
    ...optionalCount(result, "group_count", 10_000),
  };
}

function requestIdMeta(result: unknown): JsonObject {
  if (!result || typeof result !== "object") return {};
  const requestID = (result as Record<string, unknown>).request_id;
  if (typeof requestID !== "string") return {};
  const trimmed = requestID.trim();
  if (!trimmed || trimmed.length > 64) return {};
  return { request_id: trimmed };
}

function optionalCount(result: unknown, key: string, maximum: number): JsonObject {
  if (!result || typeof result !== "object") return {};
  const value = (result as Record<string, unknown>)[key];
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0 || value > maximum) return {};
  return { [key]: value };
}

function jsonObject(value: object): JsonObject {
  return JSON.parse(JSON.stringify(value)) as JsonObject;
}

function workflowIntentPayload(prepared: PreparedWorkflowRunRequest, toolCallID: string, global: boolean): JsonObject {
  const body = workflowRunRequestPayload(prepared, toolCallID);
  return jsonObject(global ? { ...body, product_id: prepared.product_id } : body);
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
