import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createServer, type AddressInfo, type ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, it, vi } from "vitest";

import { API_VERSION, type TurnState } from "../src/contracts.js";
import { PiRuntimeManager } from "../src/runtime-manager.js";
import { TurnStore } from "../src/store.js";
import { runLiveEvals } from "./live-runner.js";
import * as goWorld from "./go-world.js";
import { loadEvalTaskSet } from "./loader.js";

vi.mock("./go-world.js", () => ({ openGoEvalHost: vi.fn() }));

const roots: string[] = [];

afterEach(async () => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true });
});

it("keeps the final Go observation before closing the host", async () => {
  const root = await mkdtemp(join(tmpdir(), "productflow-live-runner-observation-"));
  roots.push(root);
  vi.stubEnv("STORAGE_ROOT", root);
  vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
  vi.stubEnv("AGENT_PROVIDER_API_KEY", "not-used");
  vi.stubEnv("VITEST", "");

  const { tasks } = await loadEvalTaskSet();
  const task = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
  const terminal: TurnState = {
    api_version: API_VERSION,
    run_id: "fixture",
    turn_id: "fixture",
    status: "succeeded",
    input: {
      input_text: task.utterances[0],
      asset_ids: [],
      idempotency_key: "fixture",
      page_context: task.page_context,
    },
    output: "diagnostic output",
    error: "",
    tool_steps: [],
    created_at: "2026-09-05T00:00:00Z",
    updated_at: "2026-09-05T00:00:00Z",
    started_at: null,
    finished_at: null,
  };
  const order: string[] = [];
  const host = {
    observeFinal: vi.fn().mockImplementation(async () => {
      order.push("observe");
      return { errors: ["final graph content mismatch"], state: { graph: { revision: 4 } }, readback_errors: [] };
    }),
    observe: vi.fn(),
    close: vi.fn().mockImplementation(async () => {
      order.push("close");
    }),
  } as unknown as Awaited<ReturnType<typeof goWorld.openGoEvalHost>>;
  vi.mocked(goWorld.openGoEvalHost).mockResolvedValue(host);
  vi.spyOn(PiRuntimeManager.prototype, "start").mockResolvedValue(terminal);
  vi.spyOn(PiRuntimeManager.prototype, "close").mockImplementation(async () => {
    order.push("manager-close");
  });
  vi.spyOn(TurnStore.prototype, "getState").mockResolvedValue(terminal);
  vi.spyOn(TurnStore.prototype, "events").mockResolvedValue([]);

  const result = await runLiveEvals({ tasks: [task], trials: 1, concurrency: 1 });
  const record = JSON.parse((await readFile(join(result.runDir, "trials.jsonl"), "utf8")).trim());
  const transcript = JSON.parse(await readFile(join(result.runDir, record.transcript_path), "utf8"));

  expect(goWorld.openGoEvalHost).toHaveBeenCalledWith(task, expect.anything(), { layer: "l1", overlay: "full" });
  expect(order).toEqual(["manager-close", "observe", "close"]);
  expect(host.observeFinal).toHaveBeenCalledTimes(1);
  expect(record.passed).toBe(false);
  expect(record.status).toBe("succeeded");
  expect(record.errors).toContain("final graph content mismatch");
  expect(result.metrics.measurementEligible).toBe(true);
  expect(transcript.final_observation).toEqual({
    errors: ["final graph content mismatch"],
    state: { graph: { revision: 4 } },
    readback_errors: [],
  });
});

it("marks an observation failure unobservable after preserving the terminal outcome", async () => {
  const root = await mkdtemp(join(tmpdir(), "productflow-live-runner-observation-error-"));
  roots.push(root);
  vi.stubEnv("STORAGE_ROOT", root);
  vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
  vi.stubEnv("AGENT_PROVIDER_API_KEY", "not-used");
  vi.stubEnv("VITEST", "");

  const { tasks } = await loadEvalTaskSet();
  const task = tasks.find((candidate) => candidate.id === "graph-editing-rename-node")!;
  const terminal: TurnState = {
    api_version: API_VERSION,
    run_id: "fixture",
    turn_id: "fixture",
    status: "succeeded",
    input: {
      input_text: task.utterances[0],
      asset_ids: [],
      idempotency_key: "fixture",
      page_context: task.page_context,
    },
    output: "diagnostic output",
    error: "",
    tool_steps: [],
    created_at: "2026-09-05T00:00:00Z",
    updated_at: "2026-09-05T00:00:00Z",
    started_at: null,
    finished_at: null,
  };
  const order: string[] = [];
  const host = {
    observeFinal: vi.fn().mockImplementation(async () => {
      order.push("observe");
      throw new Error("database readback unavailable");
    }),
    observe: vi.fn(),
    close: vi.fn().mockImplementation(async () => {
      order.push("close");
    }),
  } as unknown as Awaited<ReturnType<typeof goWorld.openGoEvalHost>>;
  vi.mocked(goWorld.openGoEvalHost).mockResolvedValue(host);
  vi.spyOn(PiRuntimeManager.prototype, "start").mockResolvedValue(terminal);
  vi.spyOn(PiRuntimeManager.prototype, "close").mockImplementation(async () => {
    order.push("manager-close");
  });
  vi.spyOn(TurnStore.prototype, "getState").mockResolvedValue(terminal);
  vi.spyOn(TurnStore.prototype, "events").mockResolvedValue([]);

  const result = await runLiveEvals({ tasks: [task], trials: 1, concurrency: 1 });
  const record = JSON.parse((await readFile(join(result.runDir, "trials.jsonl"), "utf8")).trim());
  const transcript = JSON.parse(await readFile(join(result.runDir, record.transcript_path), "utf8"));

  expect(order).toEqual(["manager-close", "observe", "close"]);
  expect(record.terminal).toBe("succeeded");
  expect(record.status).toBe("unobservable");
  expect(record.passed).toBe(false);
  expect(record.errors).toContain("final observation failed: database readback unavailable");
  expect(result.metrics.measurementEligible).toBe(false);
  expect(transcript.final_observation).toEqual({
    errors: ["database readback unavailable"],
    state: {},
    readback_errors: ["database readback unavailable"],
  });
});

it("keeps global page selection out of image preload while product turns retain attachments", async () => {
  const root = await mkdtemp(join(tmpdir(), "productflow-live-runner-asset-input-"));
  roots.push(root);
  let provider: EvalProvider | undefined;
  try {
    vi.stubEnv("STORAGE_ROOT", root);
    vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
    vi.stubEnv("AGENT_PROVIDER_API_KEY", "fake-key");
    vi.stubEnv("AGENT_PROVIDER_KIND", "openai");
    vi.stubEnv("AGENT_PROVIDER_MODEL", "gpt-4.1");
    vi.stubEnv("VITEST", "1");

    const { tasks } = await loadEvalTaskSet();
    const restore = tasks.find((task) => task.id === "media-library-organization-restore-asset")!;
    const productSource = tasks.find((task) => task.id === "product-intake-inspect-and-clarify-missing-types")!;
    provider = await createEvalProvider(restore.reference.scripted_calls.at(-1)!.params as Record<string, unknown>);
    vi.stubEnv("AGENT_PROVIDER_BASE_URL", provider.baseURL);
    const product = structuredClone(productSource);
    product.expect = {
      terminal: ["succeeded"],
      tools: { required: [], forbidden: [], reads: [] },
      ops: { required: [], forbidden: [] },
      writes: [],
    };

    const result = await runLiveEvals({ tasks: [restore, product], trials: 1, concurrency: 1 });
    const rows = (await readFile(join(result.runDir, "trials.jsonl"), "utf8"))
      .trim().split("\n").map((line) => JSON.parse(line) as { task_id: string; terminal: string; passed: boolean; transcript_path: string });
    expect(rows.map((row) => [row.task_id, row.terminal, row.passed])).toEqual([
      [restore.id, "awaiting_confirmation", true],
      [product.id, "succeeded", true],
    ]);

    const restoreTranscript = JSON.parse(await readFile(join(result.runDir, rows[0]!.transcript_path), "utf8")) as {
      terminal_status: string;
    };
    expect(restoreTranscript.terminal_status).toBe("awaiting_confirmation");
    expect(provider.requestBodies).toHaveLength(4);
    const restoreRequests = provider.requestBodies.slice(0, 3).map((body) => JSON.stringify(body));
    expect(restoreRequests.every((body) => !body.includes("iVBORw0KGgo"))).toBe(true);
    expect(JSON.stringify(provider.requestBodies[3])).toContain("iVBORw0KGgo");
  } finally {
    await provider?.close();
  }
});

interface EvalProvider {
  baseURL: string;
  requestBodies: Record<string, unknown>[];
  close(): Promise<void>;
}

async function createEvalProvider(proposalParams: Record<string, unknown>): Promise<EvalProvider> {
  const requestBodies: Record<string, unknown>[] = [];
  let globalStep = 0;
  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => {
      const body = JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown>;
      requestBodies.push(body);
      if (globalStep === 0) {
        globalStep += 1;
        writeEvalFunctionCall(response, "load_productflow_skill", { skill_name: "media-library-organization" }, "load-skill");
      } else if (globalStep === 1) {
        globalStep += 1;
        writeEvalFunctionCall(response, "list_global_media_library_assets_v1", { limit: 20, include_archived: true }, "list-assets");
      } else if (globalStep === 2) {
        globalStep += 1;
        writeEvalFunctionCall(response, "propose_global_draft", proposalParams, "propose-draft");
      } else {
        writeEvalText(response, "fake product turn");
      }
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
  const address = server.address() as AddressInfo;
  return {
    baseURL: `http://127.0.0.1:${address.port}/v1`,
    requestBodies,
    close: async () => {
      if (!server.listening) return;
      await new Promise<void>((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
    },
  };
}

function writeEvalFunctionCall(
  response: ServerResponse,
  name: string,
  params: Record<string, unknown>,
  id: string,
): void {
  const call = {
    type: "function_call",
    id: `fc-${id}`,
    call_id: `call-${id}`,
    name,
    arguments: JSON.stringify(params),
    status: "completed",
  };
  const body = { id: `response-${id}`, object: "response", status: "completed", output: [call], usage: { input_tokens: 1, output_tokens: 2, total_tokens: 3 } };
  response.writeHead(200, { "content-type": "text/event-stream", connection: "keep-alive", "cache-control": "no-cache" });
  writeEvalSSE(response, { type: "response.created", response: { ...body, status: "in_progress", output: [] } });
  writeEvalSSE(response, { type: "response.output_item.added", output_index: 0, item: { ...call, arguments: "" } });
  writeEvalSSE(response, { type: "response.function_call_arguments.delta", output_index: 0, delta: call.arguments });
  writeEvalSSE(response, { type: "response.function_call_arguments.done", output_index: 0, arguments: call.arguments });
  writeEvalSSE(response, { type: "response.output_item.done", output_index: 0, item: call });
  writeEvalSSE(response, { type: "response.completed", response: body });
  response.end();
}

function writeEvalText(response: ServerResponse, text: string): void {
  const message = {
    type: "message",
    id: "message-product",
    role: "assistant",
    status: "completed",
    phase: "final_answer",
    content: [{ type: "output_text", text, annotations: [] }],
  };
  const body = { id: "response-product", object: "response", status: "completed", output: [message], usage: { input_tokens: 1, output_tokens: 2, total_tokens: 3 } };
  response.writeHead(200, { "content-type": "text/event-stream", connection: "keep-alive", "cache-control": "no-cache" });
  writeEvalSSE(response, { type: "response.created", response: { ...body, status: "in_progress", output: [] } });
  writeEvalSSE(response, { type: "response.output_item.added", output_index: 0, item: { ...message, content: [] } });
  writeEvalSSE(response, { type: "response.output_text.delta", output_index: 0, delta: text });
  writeEvalSSE(response, { type: "response.output_text.done", output_index: 0, text });
  writeEvalSSE(response, { type: "response.output_item.done", output_index: 0, item: message });
  writeEvalSSE(response, { type: "response.completed", response: body });
  response.end();
}

function writeEvalSSE(response: ServerResponse, event: Record<string, unknown>): void {
  response.write(`event: ${String(event.type)}\ndata: ${JSON.stringify(event)}\n\n`);
}
