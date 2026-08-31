/**
 * ProductFlow 内部 Agent API 的 HTTP 客户端。
 *
 * 工具副作用、租约和 checkpoint 都在 ProductFlow 上 claim。
 * 缺少证明不能当失败；调用方必须对账或标 unknown。
 */

import {
  ProductFlowError,
  type AgentEventReceipt,
  type AgentEventConfirmation,
  type AgentCheckpointReceipt,
  type AgentExecutionLease,
  type CheckpointKind,
  type ExecutionPhase,
  type JsonObject,
  type ProductFlowContract,
  type ProviderConfig,
  type RuntimeContext,
} from "./contracts.js";

const JSON_LIMIT = 2 << 20;
const IMAGE_LIMIT = 20 << 20;

export interface AssetContent {
  data: string;
  mediaType: string;
  sizeBytes: number;
}

export interface PreparedWorkflowRunRequest {
  product_id: string;
  workflow_id: string;
  workflow_title: string;
  workflow_revision: number;
  runnable_node_count: number;
  task_id: string | null;
  source_run_id?: string | null;
  scope?: string;
  node_id?: string;
  node_ids?: string[];
  force?: boolean;
  document_action?: "complete" | "rewrite" | "replace";
}

/** ProductFlow 对一次变更的证明。unknown 表示副作用无法证明。 */
export interface ReconcileResult {
  state: "applied" | "not_applied" | "conflict" | "unknown" | string;
  result?: unknown;
  detail?: string;
}

export interface AgentEventInput {
  sequence: number;
  schema_version: 1;
  run_id: string;
  turn_id: string;
  kind: string;
  ignorable?: boolean;
  payload: JsonObject;
  created_at: string;
}

export class ProductFlowClient {
  constructor(
    readonly baseURL: string,
    readonly token: string,
    readonly timeoutMS: number,
  ) {
    if (!/^https?:\/\/[^/]+$/u.test(baseURL)) throw new Error("ProductFlow base URL must be absolute HTTP(S) without a path");
    if (!token.trim()) throw new Error("ProductFlow internal token is required");
  }

  async conversationContract(conversationID: string, signal?: AbortSignal): Promise<ProductFlowContract> {
    return this.json<ProductFlowContract>(this.conversationPath(conversationID) + "/contract", { signal });
  }

  async taskContract(taskID: string, signal?: AbortSignal): Promise<ProductFlowContract> {
    return this.json<ProductFlowContract>(this.taskPath(taskID) + "/contract", { signal });
  }

  async runtimeContext(conversationID: string, taskID: string | null, signal?: AbortSignal): Promise<RuntimeContext> {
    const query = taskID ? `?${new URLSearchParams({ task_id: taskID })}` : "";
    return this.json<RuntimeContext>(this.conversationPath(conversationID) + "/runtime-context" + query, { signal });
  }

  /** 模型或取消路径开始前，先在 ProductFlow 上独占执行。 */
  async claimTurnExecution(
    conversationID: string,
    args: { task_id: string | null; idempotency_key: string; harness_turn_id: string; owner_id: string },
    signal?: AbortSignal,
  ): Promise<AgentExecutionLease> {
    return this.json<AgentExecutionLease>(this.conversationPath(conversationID) + "/turn-executions/claim", {
      method: "POST",
      body: args,
      signal,
    });
  }

  async appendTurnCheckpoint(
    conversationID: string,
    executionID: string,
    args: {
      owner_id: string;
      lease_token: string;
      sequence: number;
      kind: CheckpointKind;
      payload: JsonObject;
    },
    signal?: AbortSignal,
  ): Promise<AgentCheckpointReceipt> {
    return this.json<AgentCheckpointReceipt>(
      this.conversationPath(conversationID) + `/turn-executions/${encodeURIComponent(executionID)}/checkpoints`,
      { method: "POST", body: args, signal },
    );
  }

  async appendTurnEvents(
    conversationID: string,
    executionID: string,
    args: {
      owner_id: string;
      lease_token: string;
      events: AgentEventInput[];
    },
    signal?: AbortSignal,
  ): Promise<AgentEventReceipt[]> {
    return this.json<{ items: AgentEventReceipt[] }>(
      this.conversationPath(conversationID) + `/turn-executions/${encodeURIComponent(executionID)}/events/batch`,
      { method: "POST", body: args, signal },
    ).then((response) => response.items);
  }

  async confirmTurnEvents(
    conversationID: string,
    executionID: string,
    args: { events: AgentEventInput[] },
    signal?: AbortSignal,
  ): Promise<AgentEventConfirmation> {
    return this.json<AgentEventConfirmation>(
      this.conversationPath(conversationID) + `/turn-executions/${encodeURIComponent(executionID)}/events/confirm`,
      { method: "POST", body: args, signal },
    );
  }

  async heartbeatTurnExecution(
    conversationID: string,
    executionID: string,
    args: { owner_id: string; lease_token: string; phase: ExecutionPhase },
    signal?: AbortSignal,
  ): Promise<AgentExecutionLease> {
    return this.json<AgentExecutionLease>(
      this.conversationPath(conversationID) + `/turn-executions/${encodeURIComponent(executionID)}/heartbeat`,
      { method: "POST", body: args, signal },
    );
  }

  async releaseTurnExecution(
    conversationID: string,
    executionID: string,
    args: { owner_id: string; lease_token: string; phase: ExecutionPhase },
    signal?: AbortSignal,
  ): Promise<{ released: boolean }> {
    return this.json<{ released: boolean }>(
      this.conversationPath(conversationID) + `/turn-executions/${encodeURIComponent(executionID)}/release`,
      { method: "POST", body: args, signal },
    );
  }

  async providerConfig(signal?: AbortSignal): Promise<ProviderConfig> {
    return this.json<ProviderConfig>("/api/internal/v1/agent-runtime/provider-config", { signal });
  }

  async productContext(conversationID: string, signal?: AbortSignal, responseFormat = "concise"): Promise<unknown> {
    return this.json(
      this.conversationPath(conversationID) + "/product-context?" + new URLSearchParams({ response_format: responseFormat }),
      { signal },
    );
  }

  async globalWorkflowContext(
    conversationID: string,
    productID: string,
    signal?: AbortSignal,
    responseFormat = "concise",
  ): Promise<unknown> {
    return this.json(
      this.conversationPath(conversationID) + "/global-workflow-context?" + new URLSearchParams({
        product_id: productID,
        response_format: responseFormat,
      }),
      { signal },
    );
  }

  async workflowRunDetail(
    conversationID: string,
    runID: string,
    signal?: AbortSignal,
  ): Promise<unknown> {
    return this.json(
      this.conversationPath(conversationID) + `/workflow-runs/${encodeURIComponent(runID)}`,
      { signal },
    );
  }

  async workflowRuns(conversationID: string, limit: number, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/workflow-runs?" + new URLSearchParams({ limit: String(limit) }), { signal });
  }

  async listAssets(
    conversationID: string,
    args: { directory_kind: string; directory_key: string | null; query: string; sort: string; after: string; limit: number },
    signal?: AbortSignal,
  ): Promise<unknown> {
    const query = new URLSearchParams({
      directory_kind: args.directory_kind,
      query: args.query,
      sort: args.sort,
      after: args.after,
      limit: String(args.limit),
    });
    if (args.directory_key) query.set("directory_key", args.directory_key);
    return this.json(this.conversationPath(conversationID) + "/assets?" + query, { signal });
  }

  async inspectAssets(conversationID: string, assetIDs: string[], signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/assets/inspect", {
      method: "POST",
      body: { asset_ids: assetIDs },
      signal,
    });
  }

  async listGlobalMediaAssets(conversationID: string, query: string, cursor: string, limit: number, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/media-library?" + new URLSearchParams({ query, cursor, limit: String(limit) }), { signal });
  }

  async inspectGlobalMediaAssets(conversationID: string, assetIDs: string[], signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/media-library/inspect", {
      method: "POST",
      body: { asset_ids: assetIDs },
      signal,
    });
  }

  async listProducts(conversationID: string, query: string, cursor: string, limit: number, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/products?" + new URLSearchParams({ query, cursor, limit: String(limit) }), { signal });
  }

  async inspectProducts(conversationID: string, productIDs: string[], signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/products/inspect", {
      method: "POST",
      body: { product_ids: productIDs },
      signal,
    });
  }

  async inspectGlobalWorkflowRuns(conversationID: string, workflowIDs: string[], limit: number, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/workflow-runs/inspect", {
      method: "POST",
      body: { workflow_ids: workflowIDs, limit },
      signal,
    });
  }

  async assetContent(conversationID: string, assetID: string, global: boolean, signal?: AbortSignal): Promise<AssetContent> {
    const path = global ? `${this.conversationPath(conversationID)}/media-library/${encodeURIComponent(assetID)}/content` : `${this.conversationPath(conversationID)}/assets/${encodeURIComponent(assetID)}/content`;
    const { response, body } = await this.request(path, { signal }, IMAGE_LIMIT);
    const data = body.toString("base64");
    return { data, mediaType: response.headers.get("content-type")?.split(";", 1)[0] || "application/octet-stream", sizeBytes: body.byteLength };
  }

  async applyGraphChangeSet(
    conversationID: string,
    changeSet: JsonObject,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<JsonObject> {
    return this.json<JsonObject>(this.conversationPath(conversationID) + "/graph/apply-change-set", {
      method: "POST",
      body: { change_set: changeSet },
      idempotencyKey,
      signal,
    });
  }

  async reconcileApplyGraphChangeSet(
    conversationID: string,
    changeSet: JsonObject,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<ReconcileResult> {
    return this.json<ReconcileResult>(this.conversationPath(conversationID) + "/graph/apply-change-set/reconcile", {
      method: "POST",
      body: { change_set: changeSet },
      idempotencyKey,
      signal,
    });
  }

  async proposeGraphChangeSet(
    conversationID: string,
    changeSet: JsonObject,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<JsonObject> {
    return this.json<JsonObject>(this.conversationPath(conversationID) + "/graph/proposals", {
      method: "POST",
      body: { change_set: changeSet },
      idempotencyKey,
      signal,
    });
  }

  async reconcileProposeGraphChangeSet(
    conversationID: string,
    changeSet: JsonObject,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<ReconcileResult> {
    return this.json<ReconcileResult>(this.conversationPath(conversationID) + "/graph/proposals/reconcile", {
      method: "POST",
      body: { change_set: changeSet },
      idempotencyKey,
      signal,
    });
  }

  async getNodeDetail(conversationID: string, nodeID: string, signal?: AbortSignal): Promise<JsonObject> {
    return this.json<JsonObject>(
      this.conversationPath(conversationID) + `/graph/nodes/${encodeURIComponent(nodeID)}`,
      { signal },
    );
  }

  async discardGraphProposal(
    conversationID: string,
    proposalID: string | null,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<JsonObject> {
    return this.json<JsonObject>(this.conversationPath(conversationID) + "/graph/proposals/discard", {
      method: "POST",
      body: { proposal_id: proposalID },
      idempotencyKey,
      signal,
    });
  }

  async reconcileDiscardGraphProposal(
    conversationID: string,
    proposalID: string | null,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<ReconcileResult> {
    return this.json<ReconcileResult>(this.conversationPath(conversationID) + "/graph/proposals/discard/reconcile", {
      method: "POST",
      body: { proposal_id: proposalID },
      idempotencyKey,
      signal,
    });
  }

  async cancelWorkflowRun(
    conversationID: string,
    runID: string,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<JsonObject> {
    return this.json<JsonObject>(
      this.conversationPath(conversationID) + `/workflow-runs/${encodeURIComponent(runID)}/cancel`,
      { method: "POST", body: {}, idempotencyKey, signal },
    );
  }

  async reconcileCancelWorkflowRun(
    conversationID: string,
    runID: string,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<ReconcileResult> {
    return this.json<ReconcileResult>(
      this.conversationPath(conversationID) + `/workflow-runs/${encodeURIComponent(runID)}/cancel/reconcile`,
      { method: "POST", body: {}, idempotencyKey, signal },
    );
  }

  async focusCanvasItems(
    conversationID: string,
    focus: { node_ids: string[]; edge_ids: string[]; group_ids: string[] },
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<JsonObject> {
    return this.json<JsonObject>(this.conversationPath(conversationID) + "/canvas/focus", {
      method: "POST",
      body: focus,
      idempotencyKey,
      signal,
    });
  }

  async reconcileFocusCanvasItems(
    conversationID: string,
    focus: { node_ids: string[]; edge_ids: string[]; group_ids: string[] },
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<ReconcileResult> {
    return this.json<ReconcileResult>(this.conversationPath(conversationID) + "/canvas/focus/reconcile", {
      method: "POST",
      body: focus,
      idempotencyKey,
      signal,
    });
  }

  async validateGlobalDraft(conversationID: string, value: unknown, signal?: AbortSignal): Promise<void> {
    await this.json(this.conversationPath(conversationID) + "/global-draft/validate", { method: "POST", body: { value }, signal });
  }

  async finalizeProductIntake(
    conversationID: string,
    args: { selection: Record<string, unknown>; reference_asset_ids: string[]; task_id: string | null },
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/product-intake", {
      method: "POST",
      body: args,
      idempotencyKey,
      signal,
    });
  }

  async reconcileProductIntake(
    conversationID: string,
    args: { selection: Record<string, unknown>; reference_asset_ids: string[]; task_id: string | null },
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<ReconcileResult> {
    return this.json<ReconcileResult>(this.conversationPath(conversationID) + "/product-intake/reconcile", {
      method: "POST",
      body: args,
      idempotencyKey,
      signal,
    });
  }

  async createProductWorkspace(conversationID: string, name: string, idempotencyKey: string, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/product-workspaces", {
      method: "POST",
      body: { name },
      idempotencyKey,
      signal,
    });
  }

  async reconcileProductWorkspace(
    conversationID: string,
    name: string,
    idempotencyKey: string,
    signal?: AbortSignal,
  ): Promise<ReconcileResult> {
    return this.json<ReconcileResult>(this.conversationPath(conversationID) + "/product-workspaces/reconcile", {
      method: "POST",
      body: { name },
      idempotencyKey,
      signal,
    });
  }

  async prepareWorkflowRunRequest(
    conversationID: string,
    args: { expected_workflow_revision: number; task_id: string | null; source_run_id: string | null },
    signal?: AbortSignal,
  ): Promise<PreparedWorkflowRunRequest> {
    return this.json<PreparedWorkflowRunRequest>(this.conversationPath(conversationID) + "/workflow-run-requests/prepare", { method: "POST", body: args, signal });
  }

  async prepareGlobalWorkflowRunRequest(
    conversationID: string,
    args: { product_id: string; workflow_id: string; expected_workflow_revision: number; task_id: string | null; source_run_id: string | null },
    signal?: AbortSignal,
  ): Promise<PreparedWorkflowRunRequest> {
    return this.json<PreparedWorkflowRunRequest>(this.conversationPath(conversationID) + "/global-workflow-run-requests/prepare", { method: "POST", body: args, signal });
  }

  async executeWorkflowRunRequest(conversationID: string, prepared: PreparedWorkflowRunRequest, sourceStepID: string, idempotencyKey: string, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/workflow-run-requests", {
      method: "POST",
      body: workflowRunRequestPayload(prepared, sourceStepID),
      idempotencyKey,
      signal,
    });
  }

  async executeGlobalWorkflowRunRequest(conversationID: string, prepared: PreparedWorkflowRunRequest, sourceStepID: string, idempotencyKey: string, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/global-workflow-run-requests", {
      method: "POST",
      body: { ...workflowRunRequestPayload(prepared, sourceStepID), product_id: prepared.product_id },
      idempotencyKey,
      signal,
    });
  }

  async reconcileWorkflowRunRequest(conversationID: string, prepared: PreparedWorkflowRunRequest, sourceStepID: string, idempotencyKey: string, global: boolean, signal?: AbortSignal): Promise<ReconcileResult> {
    const suffix = global ? "/global-workflow-run-requests/reconcile" : "/workflow-run-requests/reconcile";
    const body = workflowRunRequestPayload(prepared, sourceStepID);
    return this.json<ReconcileResult>(this.conversationPath(conversationID) + suffix, {
      method: "POST",
      body: global ? { ...body, product_id: prepared.product_id } : body,
      idempotencyKey,
      signal,
    });
  }

  private conversationPath(conversationID: string): string {
    return `/api/internal/v1/agent-conversations/${encodeURIComponent(conversationID)}`;
  }

  private taskPath(taskID: string): string {
    return `/api/internal/v1/agent-tasks/${encodeURIComponent(taskID)}`;
  }

  private async json<T = unknown>(path: string, options: RequestOptions): Promise<T> {
    const { body } = await this.request(path, options, JSON_LIMIT);
    const text = body.toString("utf8");
    if (!text) return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch (error) {
      throw new ProductFlowError(502, "invalid_response", "ProductFlow returned invalid JSON");
    }
  }

  private async request(path: string, options: RequestOptions, maxBytes: number): Promise<{ response: Response; body: Buffer }> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMS);
    const signal = options.signal ? AbortSignal.any([controller.signal, options.signal]) : controller.signal;
    try {
      const body = options.body === undefined ? undefined : JSON.stringify(options.body);
      if (body !== undefined && Buffer.byteLength(body) > maxBytes) throw new ProductFlowError(413, "body_too_large", "ProductFlow request body exceeds the limit");
      const response = await fetch(this.baseURL + path, {
        method: options.method ?? "GET",
        headers: {
          Authorization: `Bearer ${this.token}`,
          Accept: "application/json",
          ...(body === undefined ? {} : { "Content-Type": "application/json" }),
          ...(options.idempotencyKey ? { "Idempotency-Key": options.idempotencyKey } : {}),
        },
        body,
        signal,
      });
      const length = Number(response.headers.get("content-length") ?? 0);
      if (length > maxBytes) throw new ProductFlowError(502, "response_too_large", "ProductFlow response exceeds the limit");
      if (!response.ok) {
        const raw = (await readBoundedBytes(response, Math.min(maxBytes, 8 << 10))).toString("utf8");
        let code = "upstream_error";
        let message = "ProductFlow request failed";
        let details: JsonObject | undefined;
        try {
          const parsed = JSON.parse(raw) as {
            detail?: unknown;
            error?: { code?: unknown; message?: unknown; details?: unknown };
          };
          if (typeof parsed.error?.code === "string" && parsed.error.code.trim()) code = parsed.error.code;
          if (typeof parsed.detail === "string" && parsed.detail.trim()) message = parsed.detail;
          else if (typeof parsed.error?.message === "string" && parsed.error.message.trim()) message = parsed.error.message;
          details = isJsonObject(parsed.error?.details) ? parsed.error.details : undefined;
        } catch {
          // 后端没返回 JSON 时使用有界的通用错误
        }
        throw new ProductFlowError(response.status, code, message.slice(0, 1000), details);
      }
      return { response, body: await readBoundedBytes(response, maxBytes) };
    } catch (error) {
      if (error instanceof ProductFlowError) throw error;
      if (error instanceof DOMException && error.name === "AbortError") throw new ProductFlowError(504, "timeout", "ProductFlow request timed out");
      throw new ProductFlowError(503, "unavailable", "ProductFlow service is unavailable");
    } finally {
      clearTimeout(timer);
    }
  }
}

async function readBoundedBytes(response: Response, maxBytes: number): Promise<Buffer> {
  const body = response.body;
  if (!body) return Buffer.alloc(0);
  const reader = body.getReader();
  const chunks: Buffer[] = [];
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > maxBytes) {
        await reader.cancel();
        throw new ProductFlowError(502, "response_too_large", "ProductFlow response exceeds the limit");
      }
      chunks.push(Buffer.from(value));
    }
  } finally {
    reader.releaseLock();
  }
  return Buffer.concat(chunks);
}

function workflowRunRequestPayload(prepared: PreparedWorkflowRunRequest, sourceStepID: string): Record<string, unknown> {
  return {
    expected_workflow_revision: prepared.workflow_revision,
    workflow_id: prepared.workflow_id,
    source_step_id: sourceStepID,
    task_id: prepared.task_id,
    source_run_id: prepared.source_run_id ?? null,
    scope: prepared.scope ?? "graph",
    node_id: prepared.node_id ?? null,
    node_ids: prepared.node_ids ?? [],
    force: prepared.force ?? false,
    document_action: prepared.document_action ?? null,
  };
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  idempotencyKey?: string;
  signal?: AbortSignal;
}

function isJsonObject(value: unknown): value is JsonObject {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}
