import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { ProductFlowError } from "../src/contracts.js";
import type { EvalCallRecord, EvalTask } from "./schema.js";
import type { StubWorld } from "./stub-world.js";

export interface DecisionEvidence { id: string; action: string; observed: boolean }

export interface GoEvalHostOptions {
  layer?: string;
  overlay?: "full" | "graph";
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
  if (overlay === "graph" || layer === "l1") await ensureEvalObservationFixtures();
  const child = spawn("go", ["test", "-C", "go", "./internal/agent", "-run", "^TestEvalUserSimHost$", "-count=1", "-v", "-timeout", "30m"], {
    cwd: repoRoot,
    env: { ...process.env, PRODUCTFLOW_EVAL_HOST_TASK: task.id, PRODUCTFLOW_EVAL_HOST_LAYER: layer },
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
  const invoke = async (method: string, params: unknown, name?: string, key?: string): Promise<any> => {
    const record: EvalCallRecord = { name: name ?? method, params: structuredClone(params), ts: new Date().toISOString(), outcome: "unknown" };
    if (recordCalls && name) stub.calls.push(record);
    try {
      const response = await fetch(baseURL, {
        method: "POST",
        body: JSON.stringify({ method, params, idempotency_key: key ?? "" }),
        signal: AbortSignal.timeout(30_000),
      });
      const result = await response.json() as { detail?: unknown; code?: unknown; error?: unknown };
      if (!response.ok) throw hostError(response.status, result);
      record.outcome = "succeeded";
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
  } else {
    Object.assign(stub.client, {
      listGlobalMediaAssets: (_c: string, query: string, cursor: string, limit: number, _signal?: AbortSignal,
        options: Record<string, unknown> = {}) => invoke("assets", { query, cursor, limit, ...options }, "list_global_media_library_assets_v1"),
      inspectGlobalMediaAssets: (_c: string, asset_ids: string[]) => invoke("inspect_assets", { asset_ids }, "inspect_global_media_library_assets_v1"),
      productContext: (_c: string, _signal: AbortSignal | undefined, format: string) =>
        invoke("context", { response_format: format || "detailed" }, "get_product_workflow_context_v1"),
      getNodeDetail: (_c: string, node_id: string) => invoke("node", { node_id }, "get_node_detail_v1"),
      applyGraphChangeSet: (_c: string, params: unknown, key?: string) => invoke("apply", params, "apply_graph_change_set_v1", key),
      proposeGraphChangeSet: (_c: string, params: unknown, key?: string) => invoke("propose", params, "propose_graph_change_set_v1", key),
      discardGraphProposal: (_c: string, proposalID: string | null, key?: string) =>
        invoke("discard", proposalID ? { proposal_id: proposalID } : {}, "discard_workflow_proposal_v1", key),
      finalizeProductIntake: (_c: string, params: unknown) => invoke("intake", params, "finalize_product_intake_v1"),
      validateGlobalDraft: (_c: string, params: unknown) => invoke("draft", params, "propose_global_draft"),
      prepareWorkflowRunRequest: (_c: string, params: unknown) => invoke("prepare", params),
      executeWorkflowRunRequest: (_c: string, prepared: Record<string, unknown>) => invoke("run", {
        expected_workflow_revision: prepared.workflow_revision,
        scope: prepared.scope ?? "graph", node_id: prepared.node_id, node_ids: prepared.node_ids,
        force: prepared.force, document_action: prepared.document_action,
      }, "request_workflow_run_v1"),
    });
  }
  return {
    async observe(): Promise<string[]> {
      const result = await invoke("observe", {});
      if (!Array.isArray(result.errors)) throw new Error("missing Go final-state observation");
      return result.errors;
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
