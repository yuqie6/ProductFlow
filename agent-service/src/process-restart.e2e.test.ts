import { spawn, type ChildProcess } from "node:child_process";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { resolvedToolContractVersion } from "./contracts.js";

const conversationID = "11111111-1111-4111-8111-111111111111";
const runID = "44444444-4444-4444-8444-444444444444";
const token = "0123456789abcdef0123456789abcdef";

describe("ProductFlow Pi Agent process recovery", () => {
  it("preserves a live ask_user Turn as requires_input on SIGTERM", async () => {
    const dataRoot = await mkdtemp(join(tmpdir(), "productflow-pi-process-question-sigterm-"));
    const provider = await createQuestionProviderServer();
    const productFlow = await createFakeProductFlowServer(provider.baseURL);
    let agent: AgentProcess | undefined;
    const turnID = "process-question-sigterm-turn-id";
    try {
      agent = await spawnAgentOnFreePort(dataRoot, productFlow.baseURL, provider.baseURL);
      const response = await fetch(`http://127.0.0.1:${agent.port}/internal/v1/conversations/${conversationID}/turns`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify({
          input_text: "需要时向我提问",
          asset_ids: [],
          idempotency_key: "process-question-sigterm-turn-1",
          turn_id: turnID,
        }),
      });
      expect(response.status).toBe(202);
      await within(productFlow.firstBatchCommitted, 5_000, "first journal batch was not committed");
      productFlow.releaseFirstBatchResponse();
      await within(provider.received, 5_000, "provider request was not started");
      await waitForJSON<{ status: string }>(
        `http://127.0.0.1:${agent.port}/internal/v1/conversations/${conversationID}/turns/${turnID}`,
        { headers: { Authorization: `Bearer ${token}` } },
        (value) => value.status === "requires_input",
      );

      agent.child.kill("SIGTERM");
      await waitForExit(agent.child);
      expect(agent.child.exitCode).toBe(0);
      agent = undefined;
      const state = JSON.parse(await readFile(join(dataRoot, "runs", runID, "turns", `${turnID}.json`), "utf8")) as { status: string };
      expect(state.status).toBe("requires_input");
      expect(productFlow.events.map((event) => event.kind)).toContain("question/requested");
      expect(productFlow.events.map((event) => event.kind)).not.toContain("turn/end");
    } finally {
      productFlow.releaseFirstBatchResponse();
      if (agent) {
        agent.child.kill("SIGKILL");
        await waitForExit(agent.child).catch(() => undefined);
      }
      await productFlow.close();
      await provider.close();
      await rm(dataRoot, { recursive: true, force: true });
    }
  }, 30_000);

  it("flushes an active Turn as unknown on SIGTERM instead of reporting user cancellation", async () => {
    const dataRoot = await mkdtemp(join(tmpdir(), "productflow-pi-process-sigterm-"));
    const provider = await createHangingProviderServer();
    const productFlow = await createFakeProductFlowServer(provider.baseURL);
    let agent: AgentProcess | undefined;
    try {
      agent = await spawnAgentOnFreePort(dataRoot, productFlow.baseURL, provider.baseURL);
      const response = await fetch(`http://127.0.0.1:${agent.port}/internal/v1/conversations/${conversationID}/turns`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify({
          input_text: "保持模型请求进行中",
          asset_ids: [],
          idempotency_key: "process-sigterm-turn-1",
          turn_id: "process-sigterm-turn-id",
        }),
      });
      expect(response.status).toBe(202);
      await within(productFlow.firstBatchCommitted, 5_000, "first journal batch was not committed");
      productFlow.releaseFirstBatchResponse();
      await within(provider.received, 5_000, "provider request was not started");

      agent.child.kill("SIGTERM");
      await waitForExit(agent.child);
      expect(agent.child.exitCode).toBe(0);
      agent = undefined;
      const terminal = productFlow.events.find((event) => event.kind === "turn/end");
      expect(terminal?.payload).toMatchObject({
        status: "unknown",
        reason: "unknown",
        reason_code: "execution_interrupted",
      });
      expect(terminal?.payload.error).toContain("stopped");
    } finally {
      productFlow.releaseFirstBatchResponse();
      if (agent) {
        agent.child.kill("SIGKILL");
        await waitForExit(agent.child).catch(() => undefined);
      }
      await productFlow.close();
      await provider.close();
      await rm(dataRoot, { recursive: true, force: true });
    }
  }, 30_000);

  it("does not replay the model after SIGKILL once the provider request has started", async () => {
    const dataRoot = await mkdtemp(join(tmpdir(), "productflow-pi-model-sigkill-"));
    const provider = await createHangingProviderServer();
    const productFlow = await createFakeProductFlowServer(provider.baseURL);
    let first: AgentProcess | undefined;
    let second: AgentProcess | undefined;
    try {
      first = await spawnAgentOnFreePort(dataRoot, productFlow.baseURL, provider.baseURL);
      const response = await fetch(`http://127.0.0.1:${first.port}/internal/v1/conversations/${conversationID}/turns`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify({
          input_text: "模型开始后中断",
          asset_ids: [],
          idempotency_key: "process-model-sigkill-1",
          turn_id: "process-model-sigkill-turn-id",
        }),
      });
      expect(response.status).toBe(202);
      await within(productFlow.firstBatchCommitted, 5_000, "first journal batch was not committed");
      productFlow.releaseFirstBatchResponse();
      await within(provider.received, 5_000, "provider request was not started");
      expect(provider.requestCount).toBe(1);

      first.child.kill("SIGKILL");
      await waitForExit(first.child);
      first = undefined;
      second = await spawnAgentOnFreePort(dataRoot, productFlow.baseURL, provider.baseURL);
      const recovered = await waitForJSON<{ status: string }>(
        `http://127.0.0.1:${second.port}/internal/v1/conversations/${conversationID}/turns/process-model-sigkill-turn-id`,
        { headers: { Authorization: `Bearer ${token}` } },
        (value) => value.status === "running" && productFlow.confirmRequests.length > 0,
      );
      expect(recovered.status).toBe("running");
      expect(provider.requestCount).toBe(1);
      expect(productFlow.checkpoints.map((checkpoint) => checkpoint.kind)).toContain("before_model_request");
      expect(productFlow.events.some((event) => event.kind === "turn/end")).toBe(false);
    } finally {
      productFlow.releaseFirstBatchResponse();
      if (first) {
        first.child.kill("SIGKILL");
        await waitForExit(first.child).catch(() => undefined);
      }
      if (second) {
        second.child.kill("SIGTERM");
        await waitForExit(second.child).catch(() => undefined);
      }
      await productFlow.close();
      await provider.close();
      await rm(dataRoot, { recursive: true, force: true });
    }
  }, 30_000);

  it("confirms a server-committed batch after SIGKILL before the local ACK", async () => {
    const dataRoot = await mkdtemp(join(tmpdir(), "productflow-pi-process-restart-"));
    const provider = await createHangingProviderServer();
    const productFlow = await createFakeProductFlowServer(provider.baseURL, true);
    let first: AgentProcess | undefined;
    let second: AgentProcess | undefined;

    try {
      first = await spawnAgentOnFreePort(dataRoot, productFlow.baseURL, provider.baseURL);
      const start = await fetch(
        `http://127.0.0.1:${first.port}/internal/v1/conversations/${conversationID}/turns`,
        {
          method: "POST",
          headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
          body: JSON.stringify({
            input_text: "保持模型请求进行中",
            asset_ids: [],
            idempotency_key: "process-restart-turn-1",
            turn_id: "process-restart-turn-id",
          }),
        },
      );
      expect(start.status).toBe(202);
      const started = (await start.json()) as { turn_id: string };
      expect(started.turn_id).toBe("process-restart-turn-id");
      await within(productFlow.firstBatchCommitted, 5_000, "first journal batch was not committed");

      const committedBeforeACK = structuredClone(productFlow.events);
      const committedSequences = committedBeforeACK.map((event) => event.sequence);
      expect(committedBeforeACK[0]).toMatchObject({ sequence: 1, kind: "turn/start" });
      expect(productFlow.batchRequests).toEqual([committedSequences]);

      first.child.kill("SIGKILL");
      await waitForExit(first.child);
      first = undefined;
      productFlow.releaseFirstBatchResponse();

      second = await spawnAgentOnFreePort(dataRoot, productFlow.baseURL, provider.baseURL);
      const recovered = await waitForJSON<{ status: string; error: string }>(
        `http://127.0.0.1:${second.port}/internal/v1/conversations/${conversationID}/turns/${started.turn_id}`,
        { headers: { Authorization: `Bearer ${token}` } },
        (value) => value.status === "running" && productFlow.confirmRequests.some((sequences) => sequences.includes(1)),
      );

      expect(recovered).toMatchObject({ status: "running", error: "" });
      expect(provider.requestCount).toBe(0);
      const health = await fetch(`http://127.0.0.1:${second.port}/healthz`);
      expect(await health.json()).toMatchObject({ active_turns: 0, queued_turns: 0 });
      expect(productFlow.checkpoints).toEqual([]);
      expect(productFlow.claimOwnerIDs.length).toBeGreaterThanOrEqual(2);
      expect(new Set(productFlow.claimOwnerIDs).size).toBe(1);
      expect(productFlow.batchRequests.filter((sequences) => sequences.includes(1))).toHaveLength(1);
      expect(productFlow.confirmRequests).toEqual([[], committedSequences]);
      expect(productFlow.batchRequests).toEqual([committedSequences]);
      expect(productFlow.events).toEqual(committedBeforeACK);
    } finally {
      productFlow.releaseFirstBatchResponse();
      if (first) {
        first.child.kill("SIGKILL");
        await waitForExit(first.child).catch(() => undefined);
      }
      if (second) {
        second.child.kill("SIGTERM");
        await waitForExit(second.child).catch(() => undefined);
      }
      await productFlow.close();
      await provider.close();
      await rm(dataRoot, { recursive: true, force: true });
    }
  }, 30_000);
});

interface AgentProcess {
  child: ChildProcess;
  port: number;
  ready: Promise<void>;
}

async function spawnAgentOnFreePort(
  dataRoot: string,
  productFlowBaseURL: string,
  providerBaseURL: string,
): Promise<AgentProcess> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 10; attempt += 1) {
    const port = await freePort();
    const started = spawnAgentProcess(dataRoot, productFlowBaseURL, providerBaseURL, port);
    try {
      await started.ready;
      return started;
    } catch (error) {
      lastError = error;
      if (started.child.exitCode == null && started.child.signalCode == null) {
        started.child.kill("SIGKILL");
        await waitForExit(started.child).catch(() => undefined);
      }
      const message = error instanceof Error ? error.message : String(error);
      if (!message.includes("EADDRINUSE")) throw error;
    }
  }
  throw lastError instanceof Error ? lastError : new Error("Agent process did not bind a free port");
}

function spawnAgentProcess(
  dataRoot: string,
  productFlowBaseURL: string,
  providerBaseURL: string,
  port: number,
): AgentProcess {
  const child = spawn(process.execPath, ["--import", "tsx/esm", "src/main.ts"], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      AGENT_LISTEN_ADDRESS: `127.0.0.1:${port}`,
      AGENT_DATA_ROOT: dataRoot,
      PRODUCTFLOW_INTERNAL_BASE_URL: productFlowBaseURL,
      AGENT_SERVICE_INTERNAL_TOKEN: token,
      AGENT_PROVIDER_API_KEY: "fake-provider-key",
      AGENT_PROVIDER_BASE_URL: providerBaseURL,
      AGENT_PROVIDER_MODEL: "fake-model",
      PRODUCTFLOW_REQUEST_TIMEOUT: "30s",
      AGENT_MAX_CONCURRENT_TURNS: "1",
      AGENT_MODEL_CONTEXT_WINDOW: "128000",
      AGENT_AUTO_COMPACT_TOKEN_LIMIT: "96000",
      AGENT_MAX_ITERATIONS: "4",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const output: string[] = [];
  const collect = (chunk: Buffer) => output.push(chunk.toString("utf8"));
  child.stdout.on("data", collect);
  child.stderr.on("data", collect);
  const ready = new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`Agent process did not start: ${output.join("")}`)), 10_000);
    const onData = () => {
      if (!output.join("").includes("ProductFlow Pi Agent listening")) return;
      clearTimeout(timer);
      child.stdout.off("data", onData);
      child.stderr.off("data", onData);
      resolve();
    };
    child.stdout.on("data", onData);
    child.stderr.on("data", onData);
    child.once("exit", (code, signal) => {
      clearTimeout(timer);
      reject(new Error(`Agent process exited before startup: code=${code} signal=${signal} output=${output.join("")}`));
    });
  });
  return { child, port, ready };
}

async function createHangingProviderServer(): Promise<{
  baseURL: string;
  requestCount: number;
  received: Promise<void>;
  close: () => Promise<void>;
}> {
  let requestCount = 0;
  let resolveReceived!: () => void;
  const received = new Promise<void>((resolve) => {
    resolveReceived = resolve;
  });
  const server = createServer((request, response) => {
    requestCount += 1;
    resolveReceived();
    response.writeHead(200, {
      "content-type": "text/event-stream",
      connection: "keep-alive",
      "cache-control": "no-cache",
    });
    request.once("close", () => response.destroy());
  });
  const baseURL = await listen(server, "/v1");
  return {
    baseURL,
    get requestCount() {
      return requestCount;
    },
    received,
    close: () => closeServer(server),
  };
}

async function createQuestionProviderServer(): Promise<{
  baseURL: string;
  received: Promise<void>;
  close: () => Promise<void>;
}> {
  let resolveReceived!: () => void;
  const received = new Promise<void>((resolve) => {
    resolveReceived = resolve;
  });
  const server = createServer((request, response) => {
    request.resume();
    request.on("end", () => {
      resolveReceived();
      response.writeHead(200, {
        "content-type": "text/event-stream",
        connection: "keep-alive",
        "cache-control": "no-cache",
      });
      const toolCall = {
        type: "function_call",
        id: "fc-question",
        call_id: "call-question",
        name: "ask_user",
        arguments: JSON.stringify({
          header: "确认",
          question: "是否继续？",
          options: [{ label: "继续" }, { label: "停止" }],
        }),
        status: "completed",
      };
      const responseBody = {
        id: "resp-question",
        object: "response",
        status: "completed",
        output: [toolCall],
        usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 },
      };
      writeProviderSSE(response, {
        type: "response.created",
        response: { id: responseBody.id, object: "response", status: "in_progress", output: [] },
      });
      writeProviderSSE(response, {
        type: "response.output_item.added",
        output_index: 0,
        item: { ...toolCall, arguments: "" },
      });
      writeProviderSSE(response, {
        type: "response.function_call_arguments.delta",
        output_index: 0,
        delta: toolCall.arguments,
      });
      writeProviderSSE(response, {
        type: "response.function_call_arguments.done",
        output_index: 0,
        arguments: toolCall.arguments,
      });
      writeProviderSSE(response, { type: "response.output_item.done", output_index: 0, item: toolCall });
      writeProviderSSE(response, { type: "response.completed", response: responseBody });
      response.end();
    });
  });
  return {
    baseURL: await listen(server, "/v1"),
    received,
    close: () => closeServer(server),
  };
}

function writeProviderSSE(response: ServerResponse, event: Record<string, unknown>): void {
  response.write(`event: ${String(event.type)}\ndata: ${JSON.stringify(event)}\n\n`);
}

async function createFakeProductFlowServer(providerBaseURL: string, holdRuntimeContextUntilACK = false): Promise<{
  baseURL: string;
  checkpoints: Array<{ kind: string; payload: Record<string, unknown> }>;
  events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }>;
  batchRequests: number[][];
  confirmRequests: number[][];
  claimOwnerIDs: string[];
  firstBatchCommitted: Promise<void>;
  releaseFirstBatchResponse: () => void;
  close: () => Promise<void>;
}> {
  const checkpoints: Array<{ kind: string; payload: Record<string, unknown> }> = [];
  const events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }> = [];
  const batchRequests: number[][] = [];
  const confirmRequests: number[][] = [];
  const claimOwnerIDs: string[] = [];
  let resolveFirstBatchCommitted!: () => void;
  const firstBatchCommitted = new Promise<void>((resolve) => {
    resolveFirstBatchCommitted = resolve;
  });
  let releaseFirstBatchResponse!: () => void;
  const firstBatchResponse = new Promise<void>((resolve) => {
    releaseFirstBatchResponse = resolve;
  });
  const handoff = { firstBatchBlocked: false, resolveFirstBatchCommitted, firstBatchResponse, holdRuntimeContextUntilACK };
  const lease = {
    harnessTurnID: "",
    ownerID: "",
    leaseToken: "lease-process-restart",
  };
  const server = createServer((request, response) => {
    void handleFakeProductFlowRequest(
      request,
      response,
      providerBaseURL,
      checkpoints,
      events,
      batchRequests,
      confirmRequests,
      claimOwnerIDs,
      lease,
      handoff,
    );
  });
  return {
    baseURL: await listen(server),
    checkpoints,
    events,
    batchRequests,
    confirmRequests,
    claimOwnerIDs,
    firstBatchCommitted,
    releaseFirstBatchResponse,
    close: () => closeServer(server),
  };
}

async function handleFakeProductFlowRequest(
  request: IncomingMessage,
  response: ServerResponse,
  providerBaseURL: string,
  checkpoints: Array<{ kind: string; payload: Record<string, unknown> }>,
  events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }>,
  batchRequests: number[][],
  confirmRequests: number[][],
  claimOwnerIDs: string[],
  lease: { harnessTurnID: string; ownerID: string; leaseToken: string },
  handoff: { firstBatchBlocked: boolean; resolveFirstBatchCommitted: () => void; firstBatchResponse: Promise<void>; holdRuntimeContextUntilACK: boolean },
): Promise<void> {
  const url = new URL(request.url ?? "/", "http://127.0.0.1");
  const body = request.method === "POST" ? await readJSON(request) : {};
  if (url.pathname.endsWith("/contract")) {
    sendJSON(response, 200, {
      schema_version: 1,
      scope_type: "global",
      conversation_id: conversationID,
      task_id: null,
      task_goal: null,
      product_id: null,
      harness_run_id: runID,
      current_draft_version: 1,
      system_prompt: "ProductFlow test contract",
      draft_kind: "global",
      draft_schema: { type: "object" },
      tool_contract_version: resolvedToolContractVersion({ type: "object" }),
    });
    return;
  }
  if (url.pathname === "/api/internal/v1/agent-runtime/provider-config") {
    sendJSON(response, 200, {
      schema_version: 1,
      provider_kind: "openai",
      api_key: "fake-provider-key",
      base_url: providerBaseURL,
      model: "fake-model",
      reasoning_effort: null,
      reasoning_summary: null,
      text_verbosity: null,
      service_tier: null,
      background_resumable: false,
    });
    return;
  }
  if (url.pathname.endsWith("/runtime-context")) {
    // Keep the ACK-loss scenario from racing with additional locally queued context events.
    if (handoff.holdRuntimeContextUntilACK) {
      await handoff.firstBatchResponse;
      if (response.destroyed) return;
    }
    sendJSON(response, 200, {
      schema_version: 1,
      session_id: "session-process-restart",
      conversation_id: conversationID,
      task_id: null,
      session_summary: null,
      task_summary: null,
    });
    return;
  }
  if (url.pathname.endsWith("/turn-executions/claim")) {
    const value = body as { harness_turn_id: string; owner_id: string };
    claimOwnerIDs.push(value.owner_id);
    lease.harnessTurnID = value.harness_turn_id;
    lease.ownerID = value.owner_id;
    sendJSON(response, 200, {
      execution_id: "execution-process-restart",
      projection_id: "projection-process-restart",
      harness_turn_id: value.harness_turn_id,
      owner_id: value.owner_id,
      lease_token: lease.leaseToken,
      attempt: 1,
      fencing_token: 1,
      phase: "claimed",
      lease_expires_at: "2099-01-01T00:00:00.000Z",
    });
    return;
  }
  if (url.pathname.endsWith("/heartbeat")) {
    sendJSON(response, 200, {
      execution_id: "execution-process-restart",
      projection_id: "projection-process-restart",
      harness_turn_id: lease.harnessTurnID,
      owner_id: lease.ownerID,
      lease_token: lease.leaseToken,
      attempt: 1,
      fencing_token: 1,
      phase: (body as { phase: string }).phase,
      lease_expires_at: "2099-01-01T00:00:00.000Z",
    });
    return;
  }
  if (url.pathname.endsWith("/checkpoints")) {
    const value = body as { kind: string; payload: Record<string, unknown> };
    checkpoints.push({ kind: value.kind, payload: value.payload });
    sendJSON(response, 200, {
      id: `checkpoint-${checkpoints.length}`,
      projection_id: "projection-process-restart",
      execution_id: "execution-process-restart",
      attempt: 1,
      fencing_token: 1,
      sequence: checkpoints.length,
      kind: value.kind,
      created_at: new Date().toISOString(),
    });
    return;
  }
  if (url.pathname.endsWith("/events/confirm")) {
    const value = body as { events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }> };
    confirmRequests.push(value.events.map((event) => event.sequence));
    const confirmed = [];
    for (const event of value.events) {
      const existing = events.find((candidate) => candidate.sequence === event.sequence);
      if (!existing) break;
      if (JSON.stringify(existing) !== JSON.stringify(event)) {
        sendJSON(response, 409, { detail: "Agent event sequence is bound to different content" });
        return;
      }
      confirmed.push(event);
    }
    sendJSON(response, 200, {
      status: confirmed.length === value.events.length ? "confirmed" : "missing",
      confirmed_through: confirmed.at(-1)?.sequence ?? (value.events[0]?.sequence ?? 1) - 1,
      persisted_through: events.at(-1)?.sequence ?? 0,
      items: confirmed.map((event) => ({
        id: `event-${event.sequence}`,
        projection_id: "projection-process-restart",
        execution_id: "execution-process-restart",
        sequence: event.sequence,
        schema_version: 1,
        kind: event.kind,
        ignorable: false,
        created_at: new Date().toISOString(),
      })),
    });
    return;
  }
  if (url.pathname.endsWith("/events/batch")) {
    const value = body as { events: Array<{ kind: string; sequence: number; payload: Record<string, unknown> }> };
    batchRequests.push(value.events.map((event) => event.sequence));
    for (const event of value.events) {
      const existing = events.find((candidate) => candidate.sequence === event.sequence);
      if (existing) {
        if (JSON.stringify(existing) !== JSON.stringify(event)) {
          sendJSON(response, 409, { detail: "Agent event sequence is bound to different content" });
          return;
        }
        continue;
      }
      events.push(event);
    }
    events.sort((left, right) => left.sequence - right.sequence);
    if (!handoff.firstBatchBlocked) {
      handoff.firstBatchBlocked = true;
      handoff.resolveFirstBatchCommitted();
      await handoff.firstBatchResponse;
      if (response.destroyed) return;
    }
    sendJSON(response, 200, {
      items: value.events.map((event) => ({
        id: `event-${event.sequence}`,
        projection_id: "projection-process-restart",
        execution_id: "execution-process-restart",
        sequence: event.sequence,
        schema_version: 1,
        kind: event.kind,
        created_at: new Date().toISOString(),
      })),
    });
    return;
  }
  if (url.pathname.endsWith("/release")) {
    sendJSON(response, 200, { released: true });
    return;
  }
  sendJSON(response, 404, { error: { code: "not_found", message: "fake ProductFlow route not found" } });
}

async function waitFor<T>(predicate: () => T | false | Promise<T | false>): Promise<T> {
  for (let attempt = 0; attempt < 300; attempt += 1) {
    const value = await predicate();
    if (value !== false && value) return value as T;
    await new Promise<void>((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("condition was not reached");
}

async function waitForJSON<T>(url: string, init: RequestInit, predicate: (value: T) => boolean): Promise<T> {
  return waitFor(async () => {
    const response = await fetch(url, init);
    if (!response.ok) return false;
    const value = (await response.json()) as T;
    return predicate(value) ? value : false;
  });
}

async function freePort(): Promise<number> {
  const server = createServer();
  const address = await listen(server);
  const port = Number(new URL(address).port);
  await closeServer(server);
  return port;
}

async function listen(server: Server, path = ""): Promise<string> {
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
  const address = server.address() as AddressInfo;
  return `http://127.0.0.1:${address.port}${path}`;
}

async function closeServer(server: Server): Promise<void> {
  if (!server.listening) return;
  await new Promise<void>((resolve) => server.close(() => resolve()));
}

async function readJSON(request: IncomingMessage): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown>;
}

function sendJSON(response: ServerResponse, status: number, body: unknown): void {
  const encoded = JSON.stringify(body);
  response.writeHead(status, { "content-type": "application/json", "content-length": Buffer.byteLength(encoded) });
  response.end(encoded);
}

function waitForExit(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return Promise.resolve();
  return new Promise<void>((resolve) => child.once("exit", () => resolve()));
}

async function within<T>(promise: Promise<T>, timeoutMS: number, message: string): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_resolve, reject) => {
        timer = setTimeout(() => reject(new Error(message)), timeoutMS);
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}
