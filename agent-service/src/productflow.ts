import { ProductFlowError, type JsonObject, type ProductFlowContract, type ProviderConfig, type RuntimeContext } from "./contracts.js";

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
}

export interface ReconcileResult {
  state: "applied" | "not_applied" | "conflict" | "unknown" | string;
  result?: unknown;
  detail?: string;
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

  async providerConfig(signal?: AbortSignal): Promise<ProviderConfig> {
    return this.json<ProviderConfig>("/api/internal/v1/agent-runtime/provider-config", { signal });
  }

  async productContext(conversationID: string, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/product-context", { signal });
  }

  async globalWorkflowContext(conversationID: string, productID: string, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/global-workflow-context?" + new URLSearchParams({ product_id: productID }), { signal });
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

  async listLegacyArchives(conversationID: string, args: { kind: string; query: string; after: string; limit: number }, signal?: AbortSignal): Promise<unknown> {
    return this.json(
      this.conversationPath(conversationID) +
        "/legacy-archives?" +
        new URLSearchParams({
          kind: args.kind,
          query: args.query,
          after: args.after,
          limit: String(args.limit),
        }),
      { signal },
    );
  }

  async inspectLegacyArchive(conversationID: string, args: { kind: string; archive_id: string; section: string; offset: number; limit: number }, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/legacy-archives/inspect", { method: "POST", body: args, signal });
  }

  async assetContent(conversationID: string, assetID: string, global: boolean, signal?: AbortSignal): Promise<AssetContent> {
    const path = global ? `${this.conversationPath(conversationID)}/media-library/${encodeURIComponent(assetID)}/content` : `${this.conversationPath(conversationID)}/assets/${encodeURIComponent(assetID)}/content`;
    const { response, body } = await this.request(path, { signal }, IMAGE_LIMIT);
    const data = body.toString("base64");
    return { data, mediaType: response.headers.get("content-type")?.split(";", 1)[0] || "application/octet-stream", sizeBytes: body.byteLength };
  }

  async validateWorkflowDraft(conversationID: string, value: unknown, signal?: AbortSignal): Promise<void> {
    await this.json(this.conversationPath(conversationID) + "/workflow-draft/validate", { method: "POST", body: { value }, signal });
  }

  async validateGlobalDraft(conversationID: string, value: unknown, signal?: AbortSignal): Promise<void> {
    await this.json(this.conversationPath(conversationID) + "/global-draft/validate", { method: "POST", body: { value }, signal });
  }

  async validateLibraryOrganizationDraft(conversationID: string, value: unknown, signal?: AbortSignal): Promise<void> {
    await this.json(this.conversationPath(conversationID) + "/library-organization-draft/validate", { method: "POST", body: { value }, signal });
  }

  async createProductWorkspace(conversationID: string, name: string, idempotencyKey: string, signal?: AbortSignal): Promise<unknown> {
    return this.json(this.conversationPath(conversationID) + "/product-workspaces", {
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
        try {
          const parsed = JSON.parse(raw) as { detail?: string; error?: { code?: string; message?: string } };
          if (typeof parsed.error?.code === "string" && parsed.error.code.trim()) code = parsed.error.code;
          if (typeof parsed.detail === "string" && parsed.detail.trim()) message = parsed.detail;
          else if (typeof parsed.error?.message === "string" && parsed.error.message.trim()) message = parsed.error.message;
        } catch {
          // Keep a bounded generic error when the backend did not return JSON.
        }
        throw new ProductFlowError(response.status, code, message.slice(0, 1000));
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
  };
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  idempotencyKey?: string;
  signal?: AbortSignal;
}
