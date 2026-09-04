import { randomUUID } from "node:crypto";

import { ProductFlowError, resolvedToolContractVersion, type JsonObject } from "../src/contracts.js";
import type { ProductFlowClient } from "../src/productflow.js";
import type { EvalCallRecord, EvalTask, EvalWorld } from "./schema.js";

export const EVAL_PRODUCT_ID = "22222222-2222-4222-8222-222222222222";
export const EVAL_WORKFLOW_ID = "33333333-3333-4333-8333-333333333333";
export const EVAL_ASSET_ID = "11111111-1111-4111-8111-111111111111";
export const EVAL_RUN_ID = "44444444-4444-4444-8444-444444444444";
export const EVAL_NODE_ID = "node-prompt-1";
export const EVAL_FOLDER_ID = "55555555-5555-4555-8555-555555555555";

const PIXEL_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

export interface StubWorld {
  client: ProductFlowClient;
  calls: EvalCallRecord[];
}

export function overlayEvalPageContext(task: EvalTask, world: EvalWorld): EvalTask["page_context"] {
  const page = { ...task.page_context, filters: { ...task.page_context.filters } };
  if (page.workflow_id != null) page.workflow_id = world.live_graph.id;
  if (page.workflow_revision != null) page.workflow_revision = world.live_graph.revision;
  if (page.filters.workflow_id) page.filters.workflow_id = world.live_graph.id;
  return page;
}

export function createStubWorld(
  task: EvalTask,
  world: EvalWorld,
  conversationID: string,
  runID: string,
  draftSchema: Record<string, unknown>,
): StubWorld {
  const calls: EvalCallRecord[] = [];
  const attempts = new Map<string, number>();
  let graphRevision = world.live_graph.revision;
  const record = (name: string, params: unknown): void => {
    calls.push({ name, params, ts: new Date().toISOString() });
  };
  const maybeReadError = (name: string): void => {
    const error = task.inject?.read_error;
    if (!error || error.tool !== name) return;
    if (error.status === "timeout") {
      throw new ProductFlowError(504, "timeout", `${name} timed out`);
    }
    throw new ProductFlowError(500, "internal", `${name} failed`);
  };
  const write = (name: string, params: unknown): void => {
    record(name, params);
    maybeReadError(name);
    validateRevisions(params, graphRevision, world);
    const attempt = (attempts.get(name) ?? 0) + 1;
    attempts.set(name, attempt);
    const conflictLimit = task.inject?.write_409_count;
    if (conflictLimit && attempt <= conflictLimit) {
      throw revisionConflict(name, graphRevision);
    }
    if (task.inject?.first_write_409 === name && attempt === 1) {
      graphRevision += 1;
      throw revisionConflict(name, graphRevision);
    }
  };
  const read = (name: string, params: unknown): void => {
    record(name, params);
    maybeReadError(name);
  };
  const payload = task.inject?.payload;
  const productName = payload?.product_name ?? "评测商品";
  const failedReason = payload?.failure_reason ?? "provider_error";
  const listedAssets = (world.listed_assets ?? []).map((asset) => ({
    ...asset,
    display_name: payload?.display_name ?? asset.display_name,
  }));
  const listedFolders = (world.listed_folders ?? [{ id: EVAL_FOLDER_ID, title: "季节" }]).map((folder) => ({
    ...folder,
    title: payload?.folder_title ?? folder.title,
  }));
  const graphNodes = world.live_graph.nodes.map((node) => (
    payload?.node_title && node.id === EVAL_NODE_ID ? { ...node, title: payload.node_title } : node
  ));
  const failedRun = {
    id: world.failed_run?.id ?? EVAL_RUN_ID,
    status: world.failed_run?.status ?? "failed",
  };
  const recentRun = world.recent_run ?? failedRun;
  const lease = {
    execution_id: "execution-eval",
    projection_id: "projection-eval",
    harness_turn_id: "",
    owner_id: "agent-eval",
    lease_token: "lease-eval",
    attempt: 1,
    fencing_token: 1,
    phase: "claimed" as const,
    lease_expires_at: "2099-01-01T00:00:00.000Z",
  };
  const prepared = (params: Record<string, unknown>) => ({
    product_id: typeof params.product_id === "string" ? params.product_id : EVAL_PRODUCT_ID,
    workflow_id: typeof params.workflow_id === "string" ? params.workflow_id : EVAL_WORKFLOW_ID,
    workflow_title: "评测工作流",
    workflow_revision: graphRevision,
    runnable_node_count: 2,
    task_id: null,
    source_run_id: typeof params.source_run_id === "string" ? params.source_run_id : null,
  });

  const client = {
    conversationContract: async () => ({
      schema_version: 1,
      scope_type: task.scope,
      conversation_id: conversationID,
      task_id: null,
      task_goal: null,
      product_id: task.scope === "product_workflow" ? EVAL_PRODUCT_ID : null,
      harness_run_id: runID,
      current_draft_version: 1,
      system_prompt: "ProductFlow",
      draft_kind: task.scope === "global" ? "global" : "workflow",
      draft_schema: draftSchema,
      tool_contract_version: resolvedToolContractVersion(draftSchema),
      has_live_graph: task.scope === "product_workflow",
    }),
    runtimeContext: async () => ({
      schema_version: 1,
      session_id: "session-eval",
      conversation_id: conversationID,
      task_id: null,
      session_summary: null,
      task_summary: null,
    }),
    providerConfig: async () => ({
      schema_version: 1,
      provider_kind: process.env.AGENT_PROVIDER_KIND?.trim() || "openai",
      api_key: process.env.AGENT_PROVIDER_API_KEY?.trim() || "",
      base_url: process.env.AGENT_PROVIDER_BASE_URL?.trim() || null,
      model: process.env.AGENT_PROVIDER_MODEL?.trim() || "gpt-4.1",
      reasoning_effort: process.env.AGENT_PROVIDER_REASONING_EFFORT?.trim() || null,
      reasoning_summary: process.env.AGENT_PROVIDER_REASONING_SUMMARY?.trim() || null,
      text_verbosity: process.env.AGENT_PROVIDER_TEXT_VERBOSITY?.trim() || null,
      service_tier: process.env.AGENT_PROVIDER_SERVICE_TIER?.trim() || null,
      background_resumable: false,
    }),
    claimTurnExecution: async (_conversationID: string, args: { harness_turn_id: string }) => ({
      ...lease,
      harness_turn_id: args.harness_turn_id,
    }),
    heartbeatTurnExecution: async () => lease,
    releaseTurnExecution: async () => ({ released: true }),
    appendTurnCheckpoint: async () => ({
      id: `checkpoint-${randomUUID()}`,
      projection_id: lease.projection_id,
      execution_id: lease.execution_id,
      attempt: 1,
      fencing_token: 1,
      sequence: 1,
      kind: "eval",
      created_at: new Date().toISOString(),
    }),
    appendTurnEvents: async (_c: string, _e: string, args: { events: Array<{ sequence: number; kind: string }> }) =>
      args.events.map((event) => ({
        id: `event-${event.sequence}`,
        projection_id: lease.projection_id,
        execution_id: lease.execution_id,
        sequence: event.sequence,
        schema_version: 1 as const,
        kind: event.kind,
        created_at: new Date().toISOString(),
      })),
    productContext: async (_conversationID: string, _signal: AbortSignal | undefined, responseFormat: string) => {
      read("get_product_workflow_context_v1", { response_format: responseFormat });
      return {
        schema_version: 1,
        product: { id: EVAL_PRODUCT_ID, name: productName },
        intake: world.intake,
        birth_expandable: world.birth_expandable,
        live_graph: { ...world.live_graph, nodes: graphNodes, revision: graphRevision },
        node_catalog: { nodes: [] },
        response_format: responseFormat,
      };
    },
    globalWorkflowContext: async (_conversationID: string, productID: string, _signal: AbortSignal | undefined, responseFormat: string) => {
      read("inspect_global_workflow_context_v1", { product_id: productID, response_format: responseFormat });
      return {
        schema_version: 1,
        product: { id: productID, name: productName },
        intake: world.intake,
        live_graph: { ...world.live_graph, nodes: graphNodes, revision: graphRevision },
        response_format: responseFormat,
      };
    },
    workflowRuns: async (_conversationID: string, limit: number) => {
      read("inspect_workflow_runs_v1", { limit });
      return { items: [recentRun], total_count: 1 };
    },
    inspectGlobalWorkflowRuns: async (_conversationID: string, workflowIDs: string[], limit: number) => {
      read("inspect_global_workflow_runs_v1", { workflow_ids: workflowIDs, limit });
      return { items: [recentRun], total_count: 1 };
    },
    workflowRunDetail: async (_conversationID: string, runIDValue: string) => {
      read("get_workflow_run_detail_v1", { run_id: runIDValue });
      return {
        run_id: runIDValue,
        status: failedRun.status,
        nodes: [{
          id: world.failed_run?.failed_node_id ?? EVAL_NODE_ID,
          status: "failed",
          failure_reason: failedReason,
        }],
      };
    },
    listAssets: async (_conversationID: string, params: Record<string, unknown>) => {
      read("list_product_image_assets_v2", params);
      return { items: listedAssets, total_count: listedAssets.length };
    },
    inspectAssets: async (_conversationID: string, assetIDs: string[]) => {
      read("inspect_product_image_assets_v1", { asset_ids: assetIDs });
      return { items: listedAssets.length > 0 ? listedAssets : assetIDs.map((id) => ({ id })) };
    },
    listGlobalMediaAssets: async (_conversationID: string, query: string, cursor: string, limit: number) => {
      read("list_global_media_library_assets_v1", { query, cursor, limit });
      return {
        items: listedAssets.length > 0 ? listedAssets : defaultAssets(payload?.display_name),
        folders: listedFolders,
        total_count: listedAssets.length > 0 ? listedAssets.length : 1,
      };
    },
    inspectGlobalMediaAssets: async (_conversationID: string, assetIDs: string[]) => {
      read("inspect_global_media_library_assets_v1", { asset_ids: assetIDs });
      return { items: listedAssets.length > 0 ? listedAssets : defaultAssets(payload?.display_name) };
    },
    listProducts: async (_conversationID: string, query: string, cursor: string, limit: number) => {
      read("list_products_v1", { query, cursor, limit });
      return { items: [{ id: EVAL_PRODUCT_ID, name: productName, workflow_id: EVAL_WORKFLOW_ID }], total_count: 1 };
    },
    inspectProducts: async (_conversationID: string, productIDs: string[]) => {
      read("inspect_products_v1", { product_ids: productIDs });
      return { items: productIDs.map((id) => ({ id, name: productName })) };
    },
    assetContent: async () => ({ data: PIXEL_PNG.toString("base64"), mediaType: "image/png", sizeBytes: PIXEL_PNG.length }),
    getNodeDetail: async (_conversationID: string, nodeID: string) => {
      read("get_node_detail_v1", { node_id: nodeID });
      return {
        id: nodeID,
        node_type: "image_prompt",
        title: payload?.node_title ?? "主图提示词",
        config_status: "ready",
      };
    },
    applyGraphChangeSet: async (_conversationID: string, changeSet: JsonObject) => {
      write("apply_graph_change_set_v1", changeSet);
      graphRevision += 1;
      return { accepted: true, applied: true, revision: graphRevision };
    },
    reconcileApplyGraphChangeSet: async () => ({ state: "applied", result: { accepted: true } }),
    proposeGraphChangeSet: async (_conversationID: string, changeSet: JsonObject) => {
      write("propose_graph_change_set_v1", changeSet);
      return { accepted: true, applied: false, pending_confirmation: true, proposal_id: "p1" };
    },
    reconcileProposeGraphChangeSet: async () => ({ state: "applied", result: { accepted: true } }),
    discardGraphProposal: async (_conversationID: string, proposalID: string | null) => {
      const params = proposalID ? { proposal_id: proposalID } : {};
      write("discard_workflow_proposal_v1", params);
      return { discarded: true };
    },
    reconcileDiscardGraphProposal: async () => ({ state: "applied", result: { discarded: true } }),
    cancelWorkflowRun: async (_conversationID: string, runIDValue: string) => {
      write("cancel_workflow_run_v1", { run_id: runIDValue });
      return { canceled: true };
    },
    reconcileCancelWorkflowRun: async () => ({ state: "applied", result: { canceled: true } }),
    focusCanvasItems: async (_conversationID: string, params: JsonObject) => {
      write("focus_canvas_items_v1", params);
      return { accepted: true };
    },
    finalizeProductIntake: async (_conversationID: string, params: JsonObject) => {
      write("finalize_product_intake_v1", withoutTaskID(params));
      return { accepted: true, node_count: 19, group_count: 4 };
    },
    reconcileProductIntake: async () => ({ state: "applied", result: { accepted: true } }),
    validateGlobalDraft: async (_conversationID: string, params: JsonObject) => {
      write("propose_global_draft", params);
    },
    createProductWorkspace: async (_conversationID: string, name: string) => {
      write("create_product_workspace_v1", { name });
      return { product_id: EVAL_PRODUCT_ID };
    },
    reconcileProductWorkspace: async () => ({ state: "applied", result: { product_id: EVAL_PRODUCT_ID } }),
    prepareWorkflowRunRequest: async (_conversationID: string, params: Record<string, unknown>) => {
      validateRevisions(params, graphRevision, world);
      return prepared(params);
    },
    prepareGlobalWorkflowRunRequest: async (_conversationID: string, params: Record<string, unknown>) => {
      validateRevisions(params, graphRevision, world);
      return prepared(params);
    },
    executeWorkflowRunRequest: async (_conversationID: string, request: Record<string, unknown>) => {
      record("request_workflow_run_v1", workflowRunToolParams(request, false));
      return { request_id: "req-1", status: "awaiting_confirmation" };
    },
    executeGlobalWorkflowRunRequest: async (_conversationID: string, request: Record<string, unknown>) => {
      record("request_global_workflow_run_v1", workflowRunToolParams(request, true));
      return { request_id: "req-1", status: "awaiting_confirmation" };
    },
    reconcileWorkflowRunRequest: async () => ({ state: "applied", result: { request_id: "req-1" } }),
  } as unknown as ProductFlowClient;

  return { client, calls };
}

function validateRevisions(params: unknown, graphRevision: number, world: EvalWorld): void {
  visit(params, (key, value) => {
    if (key === "base_graph_revision" || key === "expected_workflow_revision") {
      if (value !== graphRevision) throw revisionConflict(key, graphRevision);
    }
    if (key === "expected_revision") {
      const revisions = new Set((world.listed_assets ?? []).map((asset) => asset.revision).filter((item) => item !== undefined));
      if (!revisions.has(value as number)) throw revisionConflict(key, Math.max(0, ...revisions));
    }
  });
}

function visit(value: unknown, read: (key: string, value: unknown) => void): void {
  if (!value || typeof value !== "object") return;
  if (Array.isArray(value)) {
    for (const item of value) visit(item, read);
    return;
  }
  for (const [key, child] of Object.entries(value)) {
    read(key, child);
    visit(child, read);
  }
}

function revisionConflict(field: string, currentRevision: number): ProductFlowError {
  return new ProductFlowError(409, "conflict", `revision conflict: current revision is ${currentRevision}`, {
    issues: [{ path: field, message: `current revision is ${currentRevision}; reread context before retrying` }],
  });
}

function defaultAssets(displayName?: string): Array<Record<string, unknown>> {
  return [{
    id: EVAL_ASSET_ID,
    display_name: displayName ?? "商品图",
    revision: 1,
    folder_id: null,
    tag_names: [],
    is_archived: false,
  }];
}

function withoutTaskID(params: JsonObject): JsonObject {
  const { task_id: _taskID, ...rest } = params;
  return rest;
}

function withoutNullTaskID(params: Record<string, unknown>): Record<string, unknown> {
  const { task_id, ...rest } = params;
  return task_id === null ? rest : params;
}

function workflowRunToolParams(prepared: Record<string, unknown>, global: boolean): Record<string, unknown> {
  const scope = typeof prepared.scope === "string" && prepared.scope.trim() !== "" ? prepared.scope.trim() : "graph";
  const params: Record<string, unknown> = {
    expected_workflow_revision: prepared.workflow_revision,
    source_run_id: prepared.source_run_id ?? null,
    scope,
  };
  if (prepared.task_id != null) params.task_id = prepared.task_id;
  if (typeof prepared.node_id === "string" && prepared.node_id) params.node_id = prepared.node_id;
  if (Array.isArray(prepared.node_ids) && prepared.node_ids.length > 0) params.node_ids = prepared.node_ids;
  if (typeof prepared.force === "boolean") params.force = prepared.force;
  if (typeof prepared.document_action === "string" && prepared.document_action) {
    params.document_action = prepared.document_action;
  }
  if (global) {
    if (typeof prepared.product_id === "string") params.product_id = prepared.product_id;
    if (typeof prepared.workflow_id === "string") params.workflow_id = prepared.workflow_id;
  }
  return withoutNullTaskID(params);
}
