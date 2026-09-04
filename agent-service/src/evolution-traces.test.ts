import * as fs from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DEPLOYED_HARNESS } from "./harness.js";
import { EvolutionTraceStore, TRACE_LIMITS, type TraceIdentity } from "./evolution-traces.js";

vi.mock("node:fs/promises", async (importOriginal) => ({ ...await importOriginal<typeof import("node:fs/promises")>() }));

const roots: string[] = [];
const identity: TraceIdentity = {
  runID: "private-run", turnID: "private-turn", attempt: 1, fencingToken: 2,
  harnessHash: DEPLOYED_HARNESS.hash, skillHash: "a".repeat(64),
};
async function root(): Promise<string> {
  const path = await fs.mkdtemp(join(tmpdir(), "productflow-traces-"));
  roots.push(path);
  return join(path, "traces");
}
async function rows(path: string) {
  const names = await fs.readdir(path);
  return Promise.all(names.filter((name) => name.endsWith(".jsonl")).map(async (name) => {
    const text = await fs.readFile(join(path, name), "utf8");
    return { name, text, records: text.trim().split("\n").map((line) => JSON.parse(line)) };
  }));
}
afterEach(async () => {
  vi.restoreAllMocks();
  for (const path of roots.splice(0)) await fs.rm(path, { recursive: true, force: true });
});

describe("bounded evolution traces", () => {
  it("does no filesystem work or content inspection when disabled", async () => {
    const mkdir = vi.spyOn(fs, "mkdir");
    const readdir = vi.spyOn(fs, "readdir");
    const store = new EvolutionTraceStore();
    const unread = new Proxy(identity, { get: () => { throw new Error("identity was inspected"); } });
    expect(store.start(unread, [])).toBeUndefined();
    await store.close();
    expect(mkdir).not.toHaveBeenCalled();
    expect(readdir).not.toHaveBeenCalled();
    expect(store.health()).toMatchObject({ enabled: false, pending_records: 0, io_errors: 0 });
  });

  it("preserves causal metadata while excluding raw text, IDs, paths and arbitrary objects", async () => {
    const path = await root();
    const store = new EvolutionTraceStore(path);
    const trace = store.start(identity, ["graph-editing"])!;
    trace.modelStart("secret-request-id", "openai", "test-model");
    const secret = "api-key-secret https://example.com/image?token=SECRET /private/path";
    trace.toolStart("apply_graph_change_set_v1", "secret-tool-id", {
      base_graph_revision: 7, operations: [{ op: "rename_node", title: secret, node_ref: secret }, { op: secret }],
      input_text: secret, api_key: secret, image: "base64-media", nested: { arbitrary: secret },
    });
    trace.toolEnd("apply_graph_change_set_v1", "secret-tool-id", true);
    trace.toolStart("load_productflow_skill", "skill-call", { skill_name: "graph-editing", resource_path: secret });
    trace.modelEnd("secret-request-id", "error", 23, { input: 10, output: 2, total_tokens: 12 });
    trace.confirmTerminal("failed");
    trace.finish();
    await store.close();
    const [file] = await rows(path);
    expect(file.records[0]).toMatchObject({ kind: "attempt_start", harness_hash: DEPLOYED_HARNESS.hash, skill_catalog_hash: identity.skillHash, attempt: 1, fencing_token: 2 });
    expect(file.records[2]).toMatchObject({ base_graph_revision: 7, op_count: 2, ops: ["rename_node", "unknown"] });
    expect(file.records[2].call_key).toBe(file.records[3].call_key);
    expect(file.records[4].skill).toBe("graph-editing");
    expect(file.records[5]).toMatchObject({ total_tokens: 12, reason: "error" });
    expect(file.records.at(-1)).toMatchObject({ kind: "attempt_end", complete: true, terminal: "failed", dropped_records: 0 });
    for (const value of [secret, "private-run", "private-turn", "secret-tool-id", "secret-request-id", "base64-media", "api_key", "resource_path"]) {
      expect(file.text).not.toContain(value);
    }
    expect((await fs.stat(join(path, file.name))).mode & 0o777).toBe(0o600);
  });

  it("bounds each attempt and reserves room to report truncation", async () => {
    const path = await root();
    const limits = { ...TRACE_LIMITS, traceBytes: 8192 };
    const store = new EvolutionTraceStore(path, limits);
    const trace = store.start(identity, [])!;
    await store.drain();
    for (let n = 0; n < 100; n += 1) {
      trace.toolStart("ask_user", `call-${n}`, { options: [{ label: "private" }, { label: "private" }] });
      await store.drain();
    }
    trace.confirmTerminal("succeeded");
    trace.finish();
    await store.close();
    const [file] = await rows(path);
    expect(Buffer.byteLength(file.text)).toBeLessThanOrEqual(limits.traceBytes);
    expect(file.text.split("\n").every((line) => Buffer.byteLength(line) < limits.recordBytes)).toBe(true);
    expect(file.records.at(-1)).toMatchObject({ kind: "attempt_end", complete: false });
    expect(file.records.at(-1).dropped_records).toBeGreaterThan(0);
    expect(store.health().dropped_records).toBeGreaterThan(0);
  });

  it("bounds pending writes without waiting for a slow disk", async () => {
    const path = await root();
    let release!: () => void;
    const blocked = new Promise<void>((resolve) => { release = resolve; });
    const original = fs.mkdir;
    vi.spyOn(fs, "mkdir").mockImplementationOnce(async (...args) => { await blocked; return original(...args); });
    const store = new EvolutionTraceStore(path, { ...TRACE_LIMITS, pending: 2 });
    const trace = store.start(identity, [])!;
    trace.modelStart("request", "openai", "model");
    trace.toolStart("ask_user", "call", {});
    expect(store.health()).toMatchObject({ pending_records: 2, dropped_records: 1 });
    release();
    await store.drain();
    trace.confirmTerminal("failed");
    await store.drain();
    trace.finish();
    await store.close();
    const [file] = await rows(path);
    expect(file.records.at(-1)).toMatchObject({ complete: false, dropped_records: 1 });
  });

  it("drops an oversized projected record and reports the gap", async () => {
    const path = await root();
    const store = new EvolutionTraceStore(path, { ...TRACE_LIMITS, recordBytes: 512 });
    const trace = store.start(identity, [])!;
    await store.drain();
    trace.toolStart("propose_graph_change_set_v1", "call", {
      operations: Array.from({ length: 128 }, () => ({ op: "update_node_config", config: { private: "content" } })),
    });
    trace.confirmTerminal("failed");
    trace.finish();
    await store.close();
    const [file] = await rows(path);
    expect(file.records.map((record) => record.kind)).toEqual(["attempt_start", "terminal", "attempt_end"]);
    expect(file.records.at(-1)).toMatchObject({ complete: false, dropped_records: 1 });
    expect(file.text.split("\n").every((line) => Buffer.byteLength(line) <= 512)).toBe(true);
  });

  it("keeps crashed or I/O-failed attempts observably incomplete", async () => {
    const path = await root();
    const store = new EvolutionTraceStore(path);
    const trace = store.start(identity, [])!;
    await store.drain();
    vi.spyOn(fs, "appendFile").mockRejectedValueOnce(new Error("SECRET filesystem path"));
    trace.modelStart("request", "openai", "model");
    trace.confirmTerminal("succeeded");
    trace.finish();
    await store.close();
    const [file] = await rows(path);
    expect(file.records).toHaveLength(1);
    expect(file.records[0].kind).toBe("attempt_start");
    expect(store.health().io_errors).toBe(1);
    expect(JSON.stringify(store.health())).not.toContain("SECRET");
  });

  it("rotates only owned inactive traces and protects concurrent attempts", async () => {
    const path = await root();
    const store = new EvolutionTraceStore(path, { ...TRACE_LIMITS, files: 2 });
    const first = store.start(identity, [])!;
    const second = store.start({ ...identity, attempt: 2 }, [])!;
    await store.drain();
    store.start({ ...identity, turnID: "third" }, []);
    await store.drain();
    expect((await rows(path))).toHaveLength(2);
    expect(store.health().dropped_records).toBe(1);
    first.finish();
    await store.drain();
    const fourth = store.start({ ...identity, turnID: "fourth" }, [])!;
    second.finish();
    fourth.finish();
    await store.close();
    expect((await rows(path))).toHaveLength(2);
    expect(store.health().evicted_traces).toBe(1);
    const resumed = new EvolutionTraceStore(path, { ...TRACE_LIMITS, files: 2 });
    const fifth = resumed.start({ ...identity, turnID: "fifth" }, [])!;
    fifth.finish();
    await resumed.close();
    const files = await rows(path);
    expect(files).toHaveLength(2);
    expect(files.reduce((sum, file) => sum + Buffer.byteLength(file.text), 0)).toBeLessThanOrEqual(2 * TRACE_LIMITS.traceBytes);
  });

  it("counts partial first writes against retention capacity", async () => {
    const path = await root();
    const original = fs.writeFile;
    vi.spyOn(fs, "writeFile").mockImplementationOnce(async (path, _data, options) => {
      await original(path, "partial", options);
      throw new Error("partial write failed");
    });
    const store = new EvolutionTraceStore(path, { ...TRACE_LIMITS, files: 1 });
    const first = store.start(identity, [])!;
    await store.drain();
    first.finish();
    const second = store.start({ ...identity, attempt: 2 }, [])!;
    second.confirmTerminal("succeeded");
    second.finish();
    await store.close();
    const files = await rows(path);
    expect(files).toHaveLength(1);
    expect(files[0].records[0].attempt).toBe(2);
    expect(store.health()).toMatchObject({ io_errors: 1, evicted_traces: 1 });
  });
});
