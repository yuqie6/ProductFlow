import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { ProductFlowError } from "../src/contracts.js";
import { workflowRunRequestPayload, type PreparedWorkflowRunRequest } from "../src/productflow.js";
import type { EvalCallRecord, EvalTask } from "./schema.js";
import type { StubWorld } from "./stub-world.js";

export interface DecisionEvidence { id: string; action: string; observed: boolean }

export interface GoEvalHostOptions {
  layer?: string;
  overlay?: "full" | "graph" | "intake";
}

const repoRoot = fileURLToPath(new URL("../../", import.meta.url));
let observationCheck: Promise<void> | undefined;

export function ensureEvalObservationFixtures(): Promise<void> {
  if (!observationCheck) {
    observationCheck = runGoTest("^TestEvalObservationFixtures$");
  }
  return observationCheck;
}

export async function openGoEvalHost(task: EvalTask, stub: StubWorld, options: GoEvalHostOptions = {}) {
  if (!process.env.DATABASE_URL) throw new Error("Go observation requires DATABASE_URL (isolated testdb only)");
  const layer = options.layer ?? "l3";
  const overlay = options.overlay ?? "full";
  if (overlay === "graph" || (overlay === "full" && layer === "l1")) await ensureEvalObservationFixtures();
  const childEnv = {
    ...process.env,
    PRODUCTFLOW_EVAL_HOST_TASK: task.id,
    PRODUCTFLOW_EVAL_HOST_LAYER: layer,
  };
  childEnv.PRODUCTFLOW_EVAL_HOST_DB_PREFIX = process.env.PRODUCTFLOW_EVAL_HOST_DB_PREFIX?.trim() || "eval_scope";
  const child = spawn("go", ["test", "-C", "go", "./internal/agent", "-run", "^TestEvalUserSimHost$", "-count=1", "-v", "-timeout", "30m"], {
    cwd: repoRoot,
    env: childEnv,
    stdio: ["ignore", "pipe", "pipe"],
    detached: true,
  });
  let logs = "";
  child.stdout.on("data", (chunk) => { logs = (logs + String(chunk)).slice(-8192); });
  child.stderr.on("data", (chunk) => { logs = (logs + String(chunk)).slice(-8192); });
  const exited = new Promise<number | null>((resolve) => { child.once("exit", resolve); child.once("error", () => resolve(null)); });
  const stop = (signal: NodeJS.Signals) => { if (child.pid) { try { process.kill(-child.pid, signal); } catch (error) { if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error; } } };
  const ready = new Promise<string>((resolve, reject) => {
    child.stdout.on("data", () => { const match = /EVAL_HOST_READY=(http:\/\/127\.0\.0\.1:\d+)/u.exec(logs); if (match) resolve(match[1]); });
    child.once("exit", () => reject(new Error(`Go eval host exited before ready: ${logs}`)));
    child.once("error", reject);
  });
  let baseURL: string;
  const timer = setTimeout(() => stop("SIGKILL"), 120_000);
  try { baseURL = await ready; } finally { clearTimeout(timer); }
  const recordCalls = overlay !== "graph";
  const writeAttempts = new Map<string, number>();
  const injectedWriteNames = new Set([
    "apply_graph_change_set_v1",
    "propose_graph_change_set_v1",
    "discard_workflow_proposal_v1",
    "cancel_workflow_run_v1",
    "focus_canvas_items_v1",
    "finalize_product_intake_v1",
    "create_product_workspace_v1",
    "propose_global_draft",
  ]);
  const injectedFailure = (name: string): ProductFlowError | undefined => {
    const readError = task.inject?.read_error;
    if (readError?.tool === name) {
      if (readError.status === "timeout") return new ProductFlowError(504, "timeout", "eval injected read timeout");
      return new ProductFlowError(500, "internal", "eval injected read error");
    }
    if (!injectedWriteNames.has(name)) return undefined;
    const attempt = (writeAttempts.get(name) ?? 0) + 1;
    writeAttempts.set(name, attempt);
    const count = task.inject?.write_409_count;
    if ((count && attempt <= count) || (task.inject?.first_write_409 === name && attempt === 1)) {
      return new ProductFlowError(409, "conflict", `eval injected revision conflict on ${name}`);
    }
    return undefined;
  };
  const observedInjections = (value: unknown): string[] | undefined => {
    const payload = task.inject?.payload;
    if (!payload || Object.keys(payload).length === 0) return undefined;
    const encoded = JSON.stringify(value) ?? "";
    return Object.entries(payload)
      .filter(([, text]) => typeof text === "string" && text.length > 0 && encoded.includes(text))
      .map(([point]) => point);
  };
  const syncGraphResult = (result: any): void => {
    if (typeof result?.revision === "number") stub.syncGraphRevision(result.revision);
    if (typeof result?.live_graph?.revision === "number") stub.syncGraphRevision(result.live_graph.revision);
  };
  const invoke = async (method: string, params: unknown, name?: string, key?: string): Promise<any> => {
    const record: EvalCallRecord = { name: name ?? method, params: structuredClone(params), ts: new Date().toISOString(), outcome: "unknown" };
    if (recordCalls && name) stub.calls.push(record);
    try {
      if (name && overlay !== "graph") {
        const failure = injectedFailure(name);
        if (failure) throw failure;
      }
      const response = await fetch(baseURL, {
        method: "POST",
        body: JSON.stringify({ method, params, idempotency_key: key ?? "" }),
        signal: AbortSignal.timeout(30_000),
      });
      const result = await response.json() as { detail?: unknown; code?: unknown; error?: unknown };
      if (!response.ok) throw hostError(response.status, result);
      record.outcome = "succeeded";
      const points = observedInjections(result);
      if (points) record.observed_injections = points;
      return result;
    } catch (error) {
      record.outcome = error instanceof ProductFlowError && error.status < 500 && error.code !== "eval_host" ? "failed" : "unknown";
      throw error;
    }
  };
  if (overlay === "graph") {
    stub.bindGraphAuthority({
      apply: (params, key) => invoke("apply", params, "apply_graph_change_set_v1", key),
      propose: (params, key) => invoke("propose", params, "propose_graph_change_set_v1", key),
      discard: (proposalID, key) => invoke("discard", proposalID ? { proposal_id: proposalID } : {}, "discard_workflow_proposal_v1", key),
      productContext: (format) => invoke("context", { response_format: format }, "get_product_workflow_context_v1"),
      getNodeDetail: (nodeID) => invoke("node", { node_id: nodeID }, "get_node_detail_v1"),
    });
    const snapshot = await invoke("context", { response_format: "concise" }) as { live_graph?: { revision?: number } };
    if (typeof snapshot.live_graph?.revision === "number") stub.syncGraphRevision(snapshot.live_graph.revision);
  } else if (overlay === "intake") {
    Object.assign(stub.client, {
      productContext: async (_c: string, _signal: AbortSignal | undefined, format: string) => {
        const result = await invoke("context", { response_format: format || "detailed" }, "get_product_workflow_context_v1");
        if (typeof result?.live_graph?.revision === "number") stub.syncGraphRevision(result.live_graph.revision);
        return result;
      },
      getNodeDetail: (_c: string, node_id: string) => invoke("node", { node_id }, "get_node_detail_v1"),
      workflowRuns: (_c: string, limit: number) => invoke("workflow_runs", { limit }, "inspect_workflow_runs_v1"),
      workflowRunDetail: (_c: string, run_id: string) => invoke("run_detail", { run_id }, "get_workflow_run_detail_v1"),
      listAssets: (_c: string, params: Record<string, unknown>) => invoke("product_assets", params, "list_product_image_assets_v2"),
      inspectAssets: (_c: string, asset_ids: string[]) => invoke("inspect_product_assets", { asset_ids }, "inspect_product_image_assets_v1"),
      assetContent: (_c: string, asset_id: string, global: boolean) => invoke(global ? "global_asset_content" : "product_asset_content", { asset_id }),
      listGlobalMediaAssets: (_c: string, query: string, cursor: string, limit: number, _signal?: AbortSignal,
        options: Record<string, unknown> = {}) => invoke("assets", { query, cursor, limit, ...options }, "list_global_media_library_assets_v1"),
      inspectGlobalMediaAssets: (_c: string, asset_ids: string[]) => invoke("inspect_assets", { asset_ids }, "inspect_global_media_library_assets_v1"),
      listProducts: (_c: string, query: string, cursor: string, limit: number) => invoke("products", { query, cursor, limit }, "list_products_v1"),
      inspectProducts: (_c: string, product_ids: string[]) => invoke("inspect_products", { product_ids }, "inspect_products_v1"),
      globalWorkflowContext: async (_c: string, product_id: string, _signal: AbortSignal | undefined, format: string) => {
        const result = await invoke("global_context", { product_id, response_format: format || "detailed" }, "inspect_global_workflow_context_v1");
        syncGraphResult(result);
        return result;
      },
      inspectGlobalWorkflowRuns: (_c: string, workflow_ids: string[], limit: number) =>
        invoke("global_runs", { workflow_ids, limit }, "inspect_global_workflow_runs_v1"),
      createProductWorkspace: (_c: string, name: string, key: string) => invoke("workspace", { name }, "create_product_workspace_v1", key),
      applyGraphChangeSet: (_c: string, params: Record<string, unknown>, key: string) => {
        return invoke("apply", params, "apply_graph_change_set_v1", key).then((result) => {
          if (typeof result?.revision === "number") stub.syncGraphRevision(result.revision);
          return result;
        });
      },
      proposeGraphChangeSet: (_c: string, params: Record<string, unknown>, key: string) =>
        invoke("propose", params, "propose_graph_change_set_v1", key),
      discardGraphProposal: (_c: string, proposalID: string | null, key: string) =>
        invoke("discard", proposalID ? { proposal_id: proposalID } : {}, "discard_workflow_proposal_v1", key),
      cancelWorkflowRun: (_c: string, run_id: string, key: string) =>
        invoke("cancel", { run_id }, "cancel_workflow_run_v1", key),
      focusCanvasItems: (_c: string, params: Record<string, unknown>, key: string) =>
        invoke("focus", params, "focus_canvas_items_v1", key),
      finalizeProductIntake: async (_c: string, params: unknown, key: string) => {
        const result = await invoke("intake", params, "finalize_product_intake_v1", key);
        if (typeof result?.revision === "number") stub.syncGraphRevision(result.revision);
        return result;
      },
      validateGlobalDraft: (_c: string, params: unknown) => invoke("draft", params, "propose_global_draft"),
      prepareWorkflowRunRequest: (_c: string, params: unknown) => invoke("prepare", params),
      executeWorkflowRunRequest: (_c: string, prepared: PreparedWorkflowRunRequest, sourceStepID: string, key: string) =>
        invoke("run", workflowRunRequestPayload(prepared, sourceStepID), "request_workflow_run_v1", key),
      prepareGlobalWorkflowRunRequest: (_c: string, params: unknown) => invoke("global_prepare", params),
      executeGlobalWorkflowRunRequest: (_c: string, prepared: PreparedWorkflowRunRequest, sourceStepID: string, key: string) =>
        invoke("global_run", { ...workflowRunRequestPayload(prepared, sourceStepID), product_id: prepared.product_id }, "request_global_workflow_run_v1", key),
    });
  } else {
    Object.assign(stub.client, {
      productContext: async (_c: string, _signal: AbortSignal | undefined, format: string) => {
        const result = await invoke("context", { response_format: format || "detailed" }, "get_product_workflow_context_v1");
        syncGraphResult(result);
        return result;
      },
      globalWorkflowContext: async (_c: string, product_id: string, _signal: AbortSignal | undefined, format: string) => {
        const result = await invoke("global_context", { product_id, response_format: format || "detailed" }, "inspect_global_workflow_context_v1");
        syncGraphResult(result);
        return result;
      },
      workflowRuns: (_c: string, limit: number) => invoke("workflow_runs", { limit }, "inspect_workflow_runs_v1"),
      workflowRunDetail: (_c: string, run_id: string) => invoke("run_detail", { run_id }, "get_workflow_run_detail_v1"),
      listAssets: (_c: string, params: Record<string, unknown>) => invoke("product_assets", params, "list_product_image_assets_v2"),
      inspectAssets: (_c: string, asset_ids: string[]) => invoke("inspect_product_assets", { asset_ids }, "inspect_product_image_assets_v1"),
      assetContent: (_c: string, asset_id: string, global: boolean) => invoke(global ? "global_asset_content" : "product_asset_content", { asset_id }),
      listGlobalMediaAssets: (_c: string, query: string, cursor: string, limit: number, _signal?: AbortSignal,
        options: Record<string, unknown> = {}) => invoke("assets", { query, cursor, limit, ...options }, "list_global_media_library_assets_v1"),
      inspectGlobalMediaAssets: (_c: string, asset_ids: string[]) => invoke("inspect_assets", { asset_ids }, "inspect_global_media_library_assets_v1"),
      listProducts: (_c: string, query: string, cursor: string, limit: number) => invoke("products", { query, cursor, limit }, "list_products_v1"),
      inspectProducts: (_c: string, product_ids: string[]) => invoke("inspect_products", { product_ids }, "inspect_products_v1"),
      getNodeDetail: (_c: string, node_id: string) => invoke("node", { node_id }, "get_node_detail_v1"),
      applyGraphChangeSet: (_c: string, params: unknown, key?: string) =>
        invoke("apply", params, "apply_graph_change_set_v1", key).then((result) => { syncGraphResult(result); return result; }),
      proposeGraphChangeSet: (_c: string, params: unknown, key?: string) => invoke("propose", params, "propose_graph_change_set_v1", key),
      discardGraphProposal: (_c: string, proposalID: string | null, key?: string) =>
        invoke("discard", proposalID ? { proposal_id: proposalID } : {}, "discard_workflow_proposal_v1", key),
      cancelWorkflowRun: (_c: string, run_id: string, key: string) =>
        invoke("cancel", { run_id }, "cancel_workflow_run_v1", key),
      focusCanvasItems: (_c: string, params: Record<string, unknown>, key: string) =>
        invoke("focus", params, "focus_canvas_items_v1", key),
      finalizeProductIntake: (_c: string, params: unknown, key: string) =>
        invoke("intake", params, "finalize_product_intake_v1", key).then((result) => { syncGraphResult(result); return result; }),
      createProductWorkspace: (_c: string, name: string, key: string) =>
        invoke("workspace", { name }, "create_product_workspace_v1", key),
      validateGlobalDraft: (_c: string, params: unknown) => invoke("draft", params, "propose_global_draft"),
      prepareWorkflowRunRequest: (_c: string, params: unknown) => invoke("prepare", params),
      executeWorkflowRunRequest: (_c: string, prepared: PreparedWorkflowRunRequest, sourceStepID: string, key: string) =>
        invoke("run", workflowRunRequestPayload(prepared, sourceStepID), "request_workflow_run_v1", key),
      prepareGlobalWorkflowRunRequest: (_c: string, params: unknown) => invoke("global_prepare", params),
      executeGlobalWorkflowRunRequest: (_c: string, prepared: PreparedWorkflowRunRequest, sourceStepID: string, key: string) =>
        invoke("global_run", { ...workflowRunRequestPayload(prepared, sourceStepID), product_id: prepared.product_id }, "request_global_workflow_run_v1", key),
    });
  }
  return {
    async observeFinal(): Promise<{ errors: string[]; state: Record<string, unknown>; readback_errors: string[] }> {
      const result = await invoke("observe", {});
      if (!Array.isArray(result.errors) || !result.state || typeof result.state !== "object" || !Array.isArray(result.readback_errors)) {
        throw new Error("missing Go final-state observation");
      }
      return {
        errors: result.errors,
        state: result.state as Record<string, unknown>,
        readback_errors: result.readback_errors,
      };
    },
    async observe(): Promise<string[]> {
      const final = await this.observeFinal();
      if (final.readback_errors.length > 0) throw new Error(`final readback failed: ${final.readback_errors.join("; ")}`);
      return final.errors;
    },
    async observeGraph(): Promise<Record<string, unknown>> {
      return invoke("graph_state", {});
    },
    async decide(kind: string, action: "confirm" | "discard"): Promise<DecisionEvidence> {
      const evidence = await invoke("decision", { kind, action });
      if (!evidence?.observed || !evidence.id || evidence.action !== action) throw new Error("unobserved user decision");
      return evidence;
    },
    async close() {
      try { await fetch(`${baseURL}/close`, { method: "POST", signal: AbortSignal.timeout(5000) }); }
      catch { stop("SIGTERM"); }
      const kill = setTimeout(() => stop("SIGKILL"), 5000);
      try { const code = await exited; if (code !== 0) throw new Error(`Go eval host exited with ${code}: ${logs}`); }
      finally { clearTimeout(kill); }
    },
  };
}

function hostError(status: number, result: { detail?: unknown; code?: unknown; error?: unknown }): ProductFlowError {
  const detail = typeof result.detail === "string" && result.detail.trim()
    ? result.detail
    : typeof result.error === "string" && result.error.trim() ? result.error : "Go observation failed";
  const code = typeof result.code === "string" && result.code.trim()
    ? result.code
    : status >= 500 ? "eval_host" : "eval_backend";
  return new ProductFlowError(status, code, detail);
}

function runGoTest(run: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const env = { ...process.env };
    delete env.PRODUCTFLOW_UPDATE_EVAL_FIXTURES;
    const child = spawn("go", ["test", "-C", "go", "./internal/agent", "-run", run, "-count=1", "-timeout", "10m"], {
      cwd: repoRoot,
      env,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let logs = "";
    child.stdout.on("data", (chunk) => { logs = (logs + String(chunk)).slice(-8000); });
    child.stderr.on("data", (chunk) => { logs = (logs + String(chunk)).slice(-8000); });
    child.once("exit", (code) => {
      if (code === 0) resolve();
      else reject(new Error(`eval observation fixtures failed: ${logs}`));
    });
    child.once("error", reject);
  });
}
