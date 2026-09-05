import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { ProductFlowError } from "../src/contracts.js";
import type { EvalCallRecord, EvalTask } from "./schema.js";
import type { StubWorld } from "./stub-world.js";

export interface DecisionEvidence { id: string; action: string; observed: boolean }

export async function openGoEvalHost(task: EvalTask, stub: StubWorld) {
  if (!process.env.DATABASE_URL) throw new Error("L3 Go observation requires DATABASE_URL (isolated testdb only)");
  const child = spawn("go", ["test", "-C", "go", "./internal/agent", "-run", "^TestEvalUserSimHost$", "-count=1", "-v", "-timeout", "30m"], {
    cwd: fileURLToPath(new URL("../../", import.meta.url)),
    env: { ...process.env, PRODUCTFLOW_EVAL_HOST_TASK: task.id }, stdio: ["ignore", "pipe", "pipe"], detached: true,
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
  const invoke = async (method: string, params: unknown, name?: string): Promise<any> => {
    const record: EvalCallRecord = { name: name ?? method, params: structuredClone(params), ts: new Date().toISOString(), outcome: "unknown" };
    if (name) stub.calls.push(record);
    try {
      const response = await fetch(baseURL, { method: "POST", body: JSON.stringify({ method, params }), signal: AbortSignal.timeout(30_000) });
      const result = await response.json();
      if (!response.ok) throw new ProductFlowError(response.status, "eval_backend", result.error ?? "Go observation failed");
      record.outcome = "succeeded";
      return result;
    } catch (error) {
      record.outcome = error instanceof ProductFlowError ? "failed" : "unknown";
      throw error;
    }
  };
  Object.assign(stub.client, {
    listGlobalMediaAssets: () => invoke("assets", {}, "list_global_media_library_assets_v1"),
    inspectGlobalMediaAssets: (_c: string, asset_ids: string[]) => invoke("inspect_assets", { asset_ids }, "inspect_global_media_library_assets_v1"),
    productContext: () => invoke("context", {}, "get_product_workflow_context_v1"),
    getNodeDetail: (_c: string, node_id: string) => invoke("node", { node_id }, "get_node_detail_v1"),
    applyGraphChangeSet: (_c: string, params: unknown) => invoke("apply", params, "apply_graph_change_set_v1"),
    proposeGraphChangeSet: (_c: string, params: unknown) => invoke("propose", params, "propose_graph_change_set_v1"),
    finalizeProductIntake: (_c: string, params: unknown) => invoke("intake", params, "finalize_product_intake_v1"),
    validateGlobalDraft: (_c: string, params: unknown) => invoke("draft", params, "propose_global_draft"),
    prepareWorkflowRunRequest: (_c: string, params: unknown) => invoke("prepare", params),
    executeWorkflowRunRequest: (_c: string, prepared: Record<string, unknown>) => invoke("run", {
      expected_workflow_revision: prepared.workflow_revision,
      scope: prepared.scope ?? "graph", node_id: prepared.node_id, node_ids: prepared.node_ids,
      force: prepared.force, document_action: prepared.document_action,
    }, "request_workflow_run_v1"),
  });
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
