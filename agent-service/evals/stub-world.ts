import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { isDeepStrictEqual } from "node:util";

import { ProductFlowError, resolvedToolContractVersion, type JsonObject } from "../src/contracts.js";
import type { ProductFlowClient } from "../src/productflow.js";
import type { EvalCallRecord, EvalTask, EvalWorld } from "./schema.js";
import { libraryObservation } from "./library-observation.js";

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

const catalogs = JSON.parse(readFileSync(new URL("./fixtures/catalog.json", import.meta.url), "utf8"));
const intakeResults = JSON.parse(readFileSync(new URL("./fixtures/intake-results.json", import.meta.url), "utf8"));
const structuralWrites = JSON.parse(readFileSync(new URL("./fixtures/structural-writes.json", import.meta.url), "utf8")) as Array<{
  world: string; operations: unknown; removed_node_ids: string[]; removed_edge_ids: string[]; removed_group_ids: string[];
}>;

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
  const library = libraryObservation(task);
  const attempts = new Map<string, number>();
  let graphRevision = world.live_graph.revision;
  let intake = world.intake;
  let birthExpandable = world.birth_expandable;
  let graph = structuredClone(world.live_graph);
  let pendingProposalID = world.pending_proposal_id ?? null;
  const record = (name: string, params: unknown): void => {
    calls.push({ name, params: structuredClone(params), ts: new Date().toISOString(), outcome: "unknown" });
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
    validateRevisions(params, graphRevision, task.skill === "media-library-organization" && task.scope === "global"
      ? { ...world, listed_assets: library.snapshot().items } : world);
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
  const productName = ["评测商品", payload?.product_name].filter(Boolean).join("\n");
  const failedReason = ["provider_error", payload?.failure_reason].filter(Boolean).join("\n");
  const listedAssets = (world.listed_assets ?? []).map((asset) => ({
    ...asset,
    display_name: [asset.display_name, payload?.display_name].filter(Boolean).join("\n"),
  }));
  const graphNodes = () => graph.nodes.map((node) => (
    payload?.node_title && node.id === EVAL_NODE_ID ? { ...node, title: `${node.title}\n${payload.node_title}` } : node
  ));
  const graphSummary = (format: string) => {
    const groups = graph.groups ?? [];
    const nodes = graphNodes().map(({ id, node_type, title }) => ({
      id, node_type, title, group_id: groups.find((group) => group.member_ids.includes(id))?.id ?? null,
    }));
    const common = { id: graph.id, title: graph.title, schema_version: 3, revision: graphRevision, nodes };
    return format === "detailed" ? { ...common, groups,
      edges: (graph.edges ?? []).map(({ id, source_id, target_id, role }) => ({ id, source_node_id: source_id, target_node_id: target_id, role })),
    } : { ...common, node_count: nodes.length, edge_count: graph.edge_count, group_count: groups.length,
      groups: groups.map(({ id, title, member_ids }) => ({ id, title, member_count: member_ids.length })),
    };
  };
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
        intake,
        birth_expandable: birthExpandable,
        live_graph: graphSummary(responseFormat),
        node_catalog: responseFormat === "detailed" ? catalogs.node_catalog : catalogs.node_catalog_index,
        image_type_catalog: catalogs.image_type_catalog,
        response_format: responseFormat,
      };
    },
    globalWorkflowContext: async (_conversationID: string, productID: string, _signal: AbortSignal | undefined, responseFormat: string) => {
      read("inspect_global_workflow_context_v1", { product_id: productID, response_format: responseFormat });
      return {
        schema_version: 1,
        product: { id: productID, name: productName },
        intake,
        live_graph: graphSummary(responseFormat),
        response_format: responseFormat,
      };
    },
    workflowRuns: async (_conversationID: string, limit: number) => {
      read("inspect_workflow_runs_v1", { limit });
      return { workflow_id: graph.id, workflow_revision: graphRevision, items: [recentRun] };
    },
    inspectGlobalWorkflowRuns: async (_conversationID: string, workflowIDs: string[], limit: number) => {
      read("inspect_global_workflow_runs_v1", { workflow_ids: workflowIDs, limit });
      const available = world.listed_workflow_runs ?? [{ workflow_id: graph.id, workflow_title: graph.title, workflow_revision: graphRevision, items: [recentRun] }];
      const items = workflowIDs.map((id) => available.find((workflow) => workflow.workflow_id === id));
      if (items.some((workflow) => !workflow)) throw new ProductFlowError(404, "not_found", "workflow not found");
      return { items: items.map((workflow) => ({ ...workflow, items: workflow!.items.slice(0, limit || 5) })) };
    },
    workflowRunDetail: async (_conversationID: string, runIDValue: string) => {
      read("get_workflow_run_detail_v1", { run_id: runIDValue });
      if (runIDValue !== failedRun.id && runIDValue !== recentRun.id) throw new ProductFlowError(404, "not_found", "run not found");
      return {
        schema_version: 1,
        run_id: runIDValue,
        workflow_id: graph.id, graph_revision: graphRevision, scope: "graph", is_retryable: true, failure_reason: failedReason,
        status: failedRun.status,
        nodes: [{
          node_id: world.failed_run?.failed_node_id ?? EVAL_NODE_ID,
          node_title: graphNodes().find((node) => node.id === (world.failed_run?.failed_node_id ?? EVAL_NODE_ID))?.title ?? "",
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
      const available = listedAssets.length > 0 ? listedAssets : task.page_context.selected_asset_ids.map((id) => ({ id, display_name: ["已选参考图", payload?.display_name].filter(Boolean).join("\n") }));
      return { items: available.filter((asset) => assetIDs.includes(asset.id)) };
    },
    listGlobalMediaAssets: async (_conversationID: string, query: string, cursor: string, limit: number, _signal?: AbortSignal, options = {}) => {
      read("list_global_media_library_assets_v1", { query, cursor, limit, ...options });
      if (task.scope !== "global") throw new ProductFlowError(409, "conflict", "global scope required");
      return library.list(query, cursor, limit, options);
    },
    inspectGlobalMediaAssets: async (_conversationID: string, assetIDs: string[]) => {
      read("inspect_global_media_library_assets_v1", { asset_ids: assetIDs });
      if (task.scope !== "global") throw new ProductFlowError(409, "conflict", "global scope required");
      return library.inspect(assetIDs);
    },
    listProducts: async (_conversationID: string, query: string, cursor: string, limit: number) => {
      read("list_products_v1", { query, cursor, limit });
      const items = [{ id: EVAL_PRODUCT_ID, name: productName, workflow_id: EVAL_WORKFLOW_ID }].filter((product) => !query || product.name.includes(query));
      return { items, total_count: items.length };
    },
    inspectProducts: async (_conversationID: string, productIDs: string[]) => {
      read("inspect_products_v1", { product_ids: productIDs });
      return { items: productIDs.filter((id) => id === EVAL_PRODUCT_ID).map((id) => ({ id, name: productName, workflow_id: EVAL_WORKFLOW_ID })) };
    },
    assetContent: async () => ({ data: PIXEL_PNG.toString("base64"), mediaType: "image/png", sizeBytes: PIXEL_PNG.length }),
    getNodeDetail: async (_conversationID: string, nodeID: string) => {
      read("get_node_detail_v1", { node_id: nodeID });
      const node = graphNodes().find((node) => node.id === nodeID);
      if (!node) throw new ProductFlowError(404, "not_found", "node not found");
      return {
        ...node,
        config: (node as { config?: unknown }).config ?? {},
      };
    },
    applyGraphChangeSet: async (_conversationID: string, changeSet: JsonObject) => {
      write("apply_graph_change_set_v1", changeSet);
      const next = structuredClone(graph);
      for (const operation of changeSet.operations as Array<Record<string, unknown>>) {
        const node = next.nodes.find((node) => node.id === operation.node_ref);
        switch (operation.op) {
          case "rename_node":
          case "update_node_config":
            if (!node) throw new ProductFlowError(404, "not_found", "node not found");
            if (operation.op === "rename_node") node.title = String(operation.title);
            else node.config = structuredClone(operation.config as Record<string, unknown>);
            break;
          case "rename_group": {
            const group = next.groups?.find((group) => group.id === operation.group_ref);
            if (!group) throw new ProductFlowError(404, "not_found", "group not found");
            group.title = String(operation.title);
            break;
          }
          case "move_nodes":
            for (const [id, x, y] of operation.nodes as Array<[string, number, number]>) {
              const target = next.nodes.find((node) => node.id === id);
              if (!target) throw new ProductFlowError(404, "not_found", "node not found");
              Object.assign(target, { position_x: x, position_y: y });
            }
            break;
          case "move_nodes_to_group": {
            const ids = operation.node_refs as string[];
            const group = next.groups?.find((group) => group.id === operation.group_ref);
            if (!group || ids.some((id) => !next.nodes.some((node) => node.id === id))) throw new ProductFlowError(404, "not_found", "node or group not found");
            for (const current of next.groups ?? []) current.member_ids = current.member_ids.filter((id) => !ids.includes(id));
            group.member_ids.push(...ids);
            break;
          }
          default: {
            const observed = structuralWrites.find((snapshot) => snapshot.world === task.world && isDeepStrictEqual(snapshot.operations, changeSet.operations));
            if (!observed) throw new ProductFlowError(422, "eval_unobservable", "structural apply requires Go observation");
            if (observed.removed_node_ids.some((id) => !next.nodes.some((node) => node.id === id)) || observed.removed_edge_ids.some((id) => !next.edges?.some((edge) => edge.id === id))) throw new ProductFlowError(404, "not_found", "structural target not found");
            next.nodes = next.nodes.filter((node) => !observed.removed_node_ids.includes(node.id));
            next.edges = next.edges?.filter((edge) => !observed.removed_edge_ids.includes(edge.id));
            next.groups = next.groups?.filter((group) => !observed.removed_group_ids.includes(group.id)).map((group) => ({ ...group, member_ids: group.member_ids.filter((id) => !observed.removed_node_ids.includes(id)) }));
            next.node_count = next.nodes.length; next.edge_count = next.edges?.length ?? 0; next.group_count = next.groups?.length ?? 0;
          }
        }
      }
      graph = next;
      graphRevision += 1;
      return { accepted: true, applied: true, revision: graphRevision };
    },
    reconcileApplyGraphChangeSet: async () => ({ state: "applied", result: { accepted: true } }),
    proposeGraphChangeSet: async (_conversationID: string, changeSet: JsonObject) => {
      write("propose_graph_change_set_v1", changeSet);
      if (pendingProposalID) throw new ProductFlowError(409, "conflict", "pending proposal already exists");
      pendingProposalID = "p1";
      return { accepted: true, applied: false, pending_confirmation: true, proposal_id: "p1" };
    },
    reconcileProposeGraphChangeSet: async () => ({ state: "applied", result: { accepted: true } }),
    discardGraphProposal: async (_conversationID: string, proposalID: string | null) => {
      const params = proposalID ? { proposal_id: proposalID } : {};
      write("discard_workflow_proposal_v1", params);
      if (!pendingProposalID || (proposalID && proposalID !== pendingProposalID)) throw new ProductFlowError(404, "not_found", "pending proposal not found");
      pendingProposalID = null;
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
      const snapshot = Object.values(intakeResults).find((value) =>
        isDeepStrictEqual((value as { selection: unknown }).selection, params.selection)) as
        { intake: EvalWorld["intake"]; nodes: EvalWorld["live_graph"]["nodes"]; edges: NonNullable<EvalWorld["live_graph"]["edges"]>; groups: EvalWorld["live_graph"]["groups"]; node_count: number; group_count: number } | undefined;
      if (!snapshot) throw new ProductFlowError(422, "eval_unobservable", "no Go observation fixture for this intake selection");
      const ids = params.reference_asset_ids;
      if (!Array.isArray(ids) || ids.length === 0 || ids.some((id) => !task.page_context.selected_asset_ids.includes(String(id)))) {
        throw new ProductFlowError(422, "validation", "unknown reference selection");
      }
      intake = { ...structuredClone(snapshot.intake), reference_asset_ids: ids };
      birthExpandable = false;
      graphRevision += 1;
      graph = { ...graph, node_count: snapshot.node_count, group_count: snapshot.group_count,
        nodes: structuredClone(snapshot.nodes), edges: structuredClone(snapshot.edges),
        groups: structuredClone(snapshot.groups), edge_count: snapshot.edges.length };
      return { accepted: true, intake_finalized: true, intake, node_count: snapshot.node_count, group_count: snapshot.group_count, revision: graphRevision };
    },
    reconcileProductIntake: async () => ({ state: "applied", result: { accepted: true } }),
    validateGlobalDraft: async (_conversationID: string, params: JsonObject) => {
      write("propose_global_draft", params);
      const snapshot = library.snapshot();
      const payload = params.library_payload as { operations?: Array<Record<string, any>> };
      for (const op of payload?.operations ?? []) {
        if (op.operation === "move" && op.target.folder_id !== null && !snapshot.folders.some((folder) => folder.id === op.target.folder_id)) {
          throw new ProductFlowError(404, "not_found", "folder not found");
        }
        if (op.operation === "link_workflow") {
          const current = snapshot.workflow;
          if (!current || current.workflow_id !== op.target.workflow_id) throw new ProductFlowError(404, "not_found", "workflow not found");
          if (current.workflow_title !== op.target.workflow_title || current.workflow_revision !== op.target.expected_workflow_revision || current.linked[op.asset_id] !== op.target.expected_linked) {
            throw new ProductFlowError(409, "conflict", "workflow link facts changed");
          }
        }
      }
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

  // Stub methods record synchronously. Retain only this invocation's records before awaiting its result.
  for (const [key, method] of Object.entries(client)) {
    if (typeof method !== "function") continue;
    Object.assign(client, { [key]: async (...args: unknown[]) => {
      const start = calls.length;
      const pending = method(...args);
      const recorded = calls.slice(start);
      try {
        const value = await pending;
        for (const call of recorded) {
          call.outcome = "succeeded";
          if (payload) {
            call.observed_injections = Object.entries(payload).filter(([, text]) =>
              typeof text === "string" && JSON.stringify(value)?.includes(text)
            ).map(([point]) => point);
          }
        }
        return value;
      } catch (error) {
        for (const call of recorded) call.outcome = error instanceof ProductFlowError && error.code === "eval_unobservable" ? "unknown" : "failed";
        throw error;
      }
    } });
  }
  return { client, calls };
}

function validateRevisions(params: unknown, graphRevision: number, world: EvalWorld): void {
  visit(params, (key, value) => {
    if (key === "base_graph_revision" || key === "expected_workflow_revision") {
      if (value !== graphRevision) throw revisionConflict(key, graphRevision);
    }
  });
  const operations = (params as { library_payload?: { operations?: Array<Record<string, unknown>> } })?.library_payload?.operations;
  for (const operation of operations ?? []) {
    const asset = world.listed_assets?.find((asset) => asset.id === operation.asset_id);
    if (!asset) throw new ProductFlowError(404, "not_found", "asset not found");
    if (operation.expected_revision !== asset.revision) throw revisionConflict("expected_revision", asset.revision ?? 0);
    const before = operation.before as Record<string, unknown> | undefined;
    const actual = { revision: asset.revision, display_name: asset.display_name, folder_id: asset.folder_id,
      tag_names: [...(asset.tag_names ?? [])].sort(), is_archived: asset.is_archived };
    const wanted = before && { ...before, tag_names: Array.isArray(before.tag_names) ? [...before.tag_names].sort() : before.tag_names };
    if (!isDeepStrictEqual(wanted, actual)) {
      throw new ProductFlowError(409, "conflict", "asset before state changed");
    }
  }
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
