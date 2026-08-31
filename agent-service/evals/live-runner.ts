/**
 * Opt-in 真实模型评测。对同一套 fixture 跑 Pi Turn，记录工具选择、调用次数和令牌。
 * 模型密钥来自 AGENT_PROVIDER_*；ProductFlow HTTP 用与 fixture.world 对齐的内存桩，
 * 避免改写开发库，同时让模型看到该场景的 intake / 图 / 失败 run。
 */

import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

import type { Config } from "../src/config.js";
import { ProductFlowError, resolvedToolContractVersion } from "../src/contracts.js";
import { PiRuntimeManager } from "../src/runtime-manager.js";
import type { ProductFlowClient } from "../src/productflow.js";
import { loadSkillCatalog } from "../src/skills.js";
import { TurnStore } from "../src/store.js";
import {
  EVAL_ASSET_ID,
  EVAL_FOLDER_ID,
  EVAL_NODE_ID,
  EVAL_PRODUCT_ID,
  EVAL_RUN_ID,
  EVAL_WORKFLOW_ID,
  SKILL_EVAL_FIXTURES,
  type SkillEvalFixture,
} from "./fixtures.js";
import { formatEvalReport, type EvalReport, type EvalReportRow } from "./harness.js";
import { loadGlobalDraftSchema } from "./json-schema.js";

const LIVE_REQUIRED_TOOLS = new Set([
  "ask_user",
  "finalize_product_intake_v1",
  "request_workflow_run_v1",
  "request_global_workflow_run_v1",
  "apply_graph_change_set_v1",
  "propose_graph_change_set_v1",
  "discard_workflow_proposal_v1",
  "propose_global_draft",
  "get_workflow_run_detail_v1",
]);

const PIXEL_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

export function requireLiveEvalEnv(): void {
  if (process.env.PRODUCTFLOW_RUN_AGENT_EVALS !== "1") {
    throw new Error("set PRODUCTFLOW_RUN_AGENT_EVALS=1 to run live agent evals");
  }
  if (!process.env.AGENT_PROVIDER_API_KEY?.trim()) {
    throw new Error("set AGENT_PROVIDER_API_KEY, configure AGENT_PROVIDER_MODEL, then run just agent-evals-live");
  }
}

export async function runLiveSkillEvals(): Promise<EvalReport> {
  requireLiveEvalEnv();
  const catalog = await loadSkillCatalog();
  const rows: EvalReportRow[] = [];
  for (const { fixture, index } of selectedLiveFixtures()) {
    const id = `${fixture.skillName}-${index + 1}`;
    try {
      const row = await runLiveFixture(fixture, id, catalog.hash);
      rows.push(row);
      process.stderr.write(`${row.ok ? "pass" : "fail"} ${id} calls=${row.callCount} tokens=${row.tokenCount ?? "unavailable"}${row.error ? ` error=${row.error}` : ""}\n`);
    } catch (error) {
      rows.push({
        id,
        skillName: fixture.skillName,
        ok: false,
        callCount: 0,
        tokenCount: null,
        error: error instanceof Error ? error.message : String(error),
      });
      process.stderr.write(`fail ${id} error=${rows[rows.length - 1].error}\n`);
    }
  }
  return { ok: rows.every((row) => row.ok), rows };
}

function selectedLiveFixtures(): Array<{ fixture: SkillEvalFixture; index: number }> {
  const all = SKILL_EVAL_FIXTURES.map((fixture, index) => ({ fixture, index }));
  const raw = process.env.PRODUCTFLOW_AGENT_EVAL_FILTER?.trim();
  if (!raw) return all;
  const wanted = new Set(raw.split(",").map((item) => item.trim()).filter(Boolean));
  const selected = all.filter(({ fixture, index }) =>
    wanted.has(fixture.skillName) || wanted.has(`${fixture.skillName}-${index + 1}`),
  );
  if (selected.length === 0) {
    throw new Error(`PRODUCTFLOW_AGENT_EVAL_FILTER matched no scenarios: ${raw}`);
  }
  return selected;
}

export { formatEvalReport };

async function runLiveFixture(fixture: SkillEvalFixture, id: string, skillHash: string): Promise<EvalReportRow> {
  void skillHash;
  const root = await mkdtemp(join(tmpdir(), `productflow-agent-eval-${id}-`));
  const conversationID = randomUUID();
  const runID = randomUUID();
  const draftSchema: Record<string, unknown> =
    fixture.contractScope === "global" ? (loadGlobalDraftSchema() as Record<string, unknown>) : {};
  const store = new TurnStore(root);
  await store.init();
  const writeAttempts: Record<string, number> = {};
  const catalog = await loadSkillCatalog();
  const manager = new PiRuntimeManager(
    liveConfig(root),
    store,
    stubProductFlow(fixture, conversationID, runID, draftSchema, writeAttempts),
    catalog,
  );
  try {
    const started = await manager.start({
      lookup: { conversationID },
      input: {
        input_text: fixture.userRequest,
        asset_ids: fixture.pageContext.selected_asset_ids,
        idempotency_key: `eval-${id}`,
        page_context: fixture.pageContext,
      },
    });
    const terminal = await waitForTerminal(store, runID, started.turn_id, 180_000);
    const tools = (terminal.tool_steps ?? [])
      .map((step) => step.tool_name)
      .filter((name): name is string => Boolean(name) && name !== "productflow_context_injection");
    const tokenCount = await tokenCountFromEvents(store, runID, started.turn_id);
    assertLiveOutcome(fixture, tools, terminal, writeAttempts);
    return {
      id,
      skillName: fixture.skillName,
      ok: true,
      callCount: tools.length,
      tokenCount,
    };
  } finally {
    await manager.close().catch(() => undefined);
    await rm(root, { recursive: true, force: true, maxRetries: 8, retryDelay: 50 }).catch(() => undefined);
  }
}

function assertLiveOutcome(
  fixture: SkillEvalFixture,
  tools: string[],
  terminal: import("../src/contracts.js").TurnState,
  writeAttempts: Record<string, number>,
): void {
  const expectsQuestion = fixture.scriptedCalls.some((call) => call.name === "ask_user");
  if (expectsQuestion) {
    if (terminal.status !== "requires_input") {
      throw new Error(
        `turn ended ${terminal.status}, expected requires_input for a clarifying question; tools=${tools.join(",") || "(none)"}; output=${diagnosticText(terminal.output)}`,
      );
    }
  } else if (terminal.status === "requires_input") {
    const question = terminal.question?.question?.trim() || terminal.question?.header?.trim() || "(question text unavailable)";
    throw new Error(
      `model asked the user instead of completing the skill loop: ${question}; tools=${tools.join(",") || "(none)"}`,
    );
  } else if (!["succeeded", "awaiting_confirmation"].includes(terminal.status)) {
    throw new Error(
      `turn ended ${terminal.status}; error=${diagnosticText(terminal.error)}; tools=${tools.join(",") || "(none)"}; output=${diagnosticText(terminal.output)}`,
    );
  }
  for (const banned of fixture.neverTools ?? []) {
    if (tools.includes(banned)) throw new Error(`Forbidden tool was selected: ${banned}`);
  }
  const ops = (terminal.tool_steps ?? []).flatMap((step) => step.details?.operation_summaries ?? []);
  for (const banned of fixture.neverOps ?? []) {
    if (ops.includes(banned)) throw new Error(`Forbidden Graph Command op was used: ${banned}`);
  }
  const required = fixture.scriptedCalls
    .map((call) => call.name)
    .filter((name, index, names) => names.indexOf(name) === index)
    .filter((name) => name === "load_productflow_skill" || LIVE_REQUIRED_TOOLS.has(name));
  for (const expected of required) {
    if (!tools.includes(expected)) {
      throw new Error(
        `expected tool ${expected} was not called; got ${tools.join(",") || "(none)"}; output=${diagnosticText(terminal.output)}`,
      );
    }
  }
  if (fixture.liveForceFirstWriteFailure) {
    const attempts = writeAttempts[fixture.liveForceFirstWriteFailure] ?? 0;
    if (attempts < 1 || attempts > 2) {
      throw new Error(
        `repair for ${fixture.liveForceFirstWriteFailure} used ${attempts} attempts; want 1–2`,
      );
    }
    if (attempts === 1) {
      throw new Error(`model did not retry ${fixture.liveForceFirstWriteFailure} after the injected 422`);
    }
  }
}

function diagnosticText(value: string | undefined): string {
  const compact = value?.replace(/\s+/gu, " ").trim() ?? "";
  return compact ? compact.slice(0, 500) : "(empty)";
}

function liveConfig(dataRoot: string): Config {
  const apiKey = process.env.AGENT_PROVIDER_API_KEY?.trim() ?? "";
  return {
    listenAddress: "127.0.0.1:0",
    dataRoot,
    productFlowBaseURL: "http://127.0.0.1:9",
    internalToken: "0123456789abcdef0123456789abcdef",
    requestTimeoutMS: 5_000,
    providerRequestTimeoutMS: 180_000,
    maxBodyBytes: 96 << 20,
    maxIterations: 12,
    modelContextWindow: 128_000,
    autoCompactTokenLimit: 96_000,
    maxConcurrentTurns: 1,
    providerAPIKey: apiKey,
    providerBaseURL: process.env.AGENT_PROVIDER_BASE_URL?.trim() || null,
    providerModel: process.env.AGENT_PROVIDER_MODEL?.trim() || null,
    providerReasoningEffort: process.env.AGENT_PROVIDER_REASONING_EFFORT?.trim() || null,
    providerReasoningSummary: process.env.AGENT_PROVIDER_REASONING_SUMMARY?.trim() || null,
    providerTextVerbosity: process.env.AGENT_PROVIDER_TEXT_VERBOSITY?.trim() || null,
    providerServiceTier: process.env.AGENT_PROVIDER_SERVICE_TIER?.trim() || null,
    questionTimeoutMS: 8_000,
  };
}

function stubProductFlow(
  fixture: SkillEvalFixture,
  conversationID: string,
  runID: string,
  draftSchema: Record<string, unknown>,
  writeAttempts: Record<string, number>,
): ProductFlowClient {
  const productID = EVAL_PRODUCT_ID;
  const workflowID = EVAL_WORKFLOW_ID;
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
  const prepared = {
    product_id: productID,
    workflow_id: workflowID,
    workflow_title: "评测工作流",
    workflow_revision: fixture.world.liveGraph.revision,
    runnable_node_count: 2,
    task_id: null,
    source_run_id: fixture.world.failedRun?.id ?? null,
  };
  const failedRun = {
    id: fixture.world.failedRun?.id ?? EVAL_RUN_ID,
    status: fixture.world.failedRun?.status ?? "failed",
  };
  const bumpWrite = (name: string, onFirst?: () => never): void => {
    writeAttempts[name] = (writeAttempts[name] ?? 0) + 1;
    if (fixture.liveForceFirstWriteFailure === name && writeAttempts[name] === 1 && onFirst) onFirst();
  };
  return {
    conversationContract: async () => ({
      schema_version: 1,
      scope_type: fixture.contractScope,
      conversation_id: conversationID,
      task_id: null,
      task_goal: null,
      product_id: fixture.contractScope === "product_workflow" ? productID : null,
      harness_run_id: runID,
      current_draft_version: 1,
      system_prompt: "ProductFlow",
      draft_kind: fixture.contractScope === "global" ? "global" : "workflow",
      draft_schema: draftSchema,
      tool_contract_version: resolvedToolContractVersion(draftSchema),
      has_live_graph: fixture.contractScope === "product_workflow",
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
      reasoning_effort: null,
      reasoning_summary: null,
      text_verbosity: null,
      service_tier: null,
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
      created_at: "2026-08-31T00:00:00.000Z",
    }),
    appendTurnEvents: async (_c: string, _e: string, args: { events: Array<{ sequence: number; kind: string }> }) =>
      args.events.map((event) => ({
        id: `event-${event.sequence}`,
        projection_id: lease.projection_id,
        execution_id: lease.execution_id,
        sequence: event.sequence,
        schema_version: 1 as const,
        kind: event.kind,
        created_at: "2026-08-31T00:00:00.000Z",
      })),
    productContext: async () => {
      const conflictAdvancedRevision = (writeAttempts.apply_graph_change_set_v1 ?? 0) > 0
        ? fixture.world.liveGraph.revision + 1
        : fixture.world.liveGraph.revision;
      return {
        schema_version: 1,
        product: { id: productID, name: "评测商品" },
        intake: fixture.world.intake,
        birth_expandable: fixture.world.birthExpandable,
        live_graph: { ...fixture.world.liveGraph, revision: conflictAdvancedRevision },
        node_catalog: { nodes: [] },
        response_format: "concise",
      };
    },
    globalWorkflowContext: async () => ({
      schema_version: 1,
      product: { id: productID, name: "评测商品" },
      intake: fixture.world.intake,
      live_graph: fixture.world.liveGraph,
      response_format: "concise",
    }),
    workflowRuns: async () => ({ items: [failedRun], total_count: 1 }),
    inspectGlobalWorkflowRuns: async () => ({ items: [failedRun], total_count: 1 }),
    workflowRunDetail: async () => ({
      run_id: failedRun.id,
      status: failedRun.status,
      nodes: [
        {
          id: fixture.world.failedRun?.failed_node_id ?? EVAL_NODE_ID,
          status: "failed",
          failure_reason: "provider_error",
        },
      ],
    }),
    listAssets: async () => ({ items: fixture.world.listedAssets ?? [], total_count: fixture.world.listedAssets?.length ?? 0 }),
    inspectAssets: async () => ({ items: fixture.world.listedAssets ?? [{ id: EVAL_ASSET_ID }] }),
    listGlobalMediaAssets: async () => ({
      items: fixture.world.listedAssets ?? [{ id: EVAL_ASSET_ID, display_name: "商品图", revision: 1, folder_id: null, tag_names: [], is_archived: false }],
      folders: fixture.world.listedFolders ?? [{ id: EVAL_FOLDER_ID, title: "季节" }],
      total_count: fixture.world.listedAssets?.length ?? 1,
    }),
    inspectGlobalMediaAssets: async () => ({
      items: fixture.world.listedAssets ?? [{ id: EVAL_ASSET_ID, display_name: "商品图", revision: 1, folder_id: null, tag_names: [], is_archived: false }],
    }),
    listProducts: async () => ({
      items: [{ id: productID, name: "评测商品", workflow_id: workflowID }],
      total_count: 1,
    }),
    inspectProducts: async () => ({ items: [{ id: productID, name: "评测商品" }] }),
    assetContent: async () => ({ data: PIXEL_PNG.toString("base64"), mediaType: "image/png", sizeBytes: PIXEL_PNG.length }),
    getNodeDetail: async () => ({
      id: EVAL_NODE_ID,
      node_type: "image_prompt",
      title: "主图提示词",
      config_status: "ready",
    }),
    applyGraphChangeSet: async () => {
      bumpWrite("apply_graph_change_set_v1", () => {
        const currentRevision = fixture.world.liveGraph.revision + 1;
        throw new ProductFlowError(409, "conflict", `live graph revision conflict: current revision is ${currentRevision}`, {
          issues: [{ path: "base_graph_revision", message: `current revision is ${currentRevision}; reread context and apply again` }],
        });
      });
      return { accepted: true, applied: true, revision: fixture.world.liveGraph.revision + 2 };
    },
    reconcileApplyGraphChangeSet: async () => ({ state: "applied", result: { accepted: true } }),
    proposeGraphChangeSet: async () => ({
      accepted: true,
      applied: false,
      pending_confirmation: true,
      proposal_id: "p1",
    }),
    reconcileProposeGraphChangeSet: async () => ({ state: "applied", result: { accepted: true } }),
    discardGraphProposal: async () => ({ discarded: true }),
    reconcileDiscardGraphProposal: async () => ({ state: "applied", result: { discarded: true } }),
    cancelWorkflowRun: async () => ({ canceled: true }),
    reconcileCancelWorkflowRun: async () => ({ state: "applied", result: { canceled: true } }),
    focusCanvasItems: async () => ({ accepted: true }),
    finalizeProductIntake: async () => ({ accepted: true, node_count: 19, group_count: 4 }),
    reconcileProductIntake: async () => ({ state: "applied", result: { accepted: true } }),
    validateGlobalDraft: async () => undefined,
    createProductWorkspace: async () => ({ product_id: productID }),
    reconcileProductWorkspace: async () => ({ state: "applied", result: { product_id: productID } }),
    prepareWorkflowRunRequest: async () => prepared,
    prepareGlobalWorkflowRunRequest: async () => prepared,
    executeWorkflowRunRequest: async () => ({ request_id: "req-1", status: "awaiting_confirmation" }),
    executeGlobalWorkflowRunRequest: async () => ({ request_id: "req-1", status: "awaiting_confirmation" }),
    reconcileWorkflowRunRequest: async () => ({ state: "applied", result: { request_id: "req-1" } }),
  } as unknown as ProductFlowClient;
}

async function waitForTerminal(
  store: TurnStore,
  runID: string,
  turnID: string,
  timeoutMS: number,
): Promise<import("../src/contracts.js").TurnState> {
  const deadline = Date.now() + timeoutMS;
  while (Date.now() < deadline) {
    const state = await store.getState(runID, turnID);
    if (["awaiting_confirmation", "succeeded", "failed", "canceled", "unknown", "requires_input"].includes(state.status)) {
      return state;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error("live eval turn did not reach a terminal state");
}

async function tokenCountFromEvents(store: TurnStore, runID: string, turnID: string): Promise<number | null> {
  const events = await store.events(runID, turnID, 0);
  let total = 0;
  for (const event of events) {
    const usage = event.payload && typeof event.payload === "object" ? (event.payload as { usage?: { total_tokens?: unknown } }).usage : undefined;
    if (usage && typeof usage.total_tokens === "number" && Number.isFinite(usage.total_tokens)) {
      total += usage.total_tokens;
    }
  }
  return total > 0 ? total : null;
}
