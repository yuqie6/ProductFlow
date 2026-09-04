import { createHash } from "node:crypto";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { harnessRoot, loadHarness } from "../src/harness.js";
import { RUNTIME_POLICY } from "../src/runtime-policy.generated.js";
import { canonicalJSON } from "../src/tool-manifest.js";

const roots: string[] = [];
async function fixture(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "productflow-harness-test-"));
  roots.push(root);
  await cp(harnessRoot(), root, { recursive: true });
  return root;
}
afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

describe("versioned ProductFlow harness", () => {
  it("hashes canonical artifact contents independently of checkout location or JSON key order", async () => {
    const root = await fixture();
    const original = loadHarness(root);
    const manifest = JSON.parse(await readFile(join(root, "manifest.json"), "utf8"));
    await writeFile(join(root, "manifest.json"), JSON.stringify(Object.fromEntries(Object.entries(manifest).reverse())));
    const reordered = loadHarness(root);
    expect(reordered.hash).toBe(original.hash);
    expect(reordered.hash).toBe(loadHarness().hash);
    expect(original.hash).toBe(createHash("sha256").update(canonicalJSON(original.artifact)).digest("hex"));
    expect(original.hash).toMatch(/^[a-f0-9]{64}$/u);
  });

  it.each(["bootstrap", "execution", "verification", "failure-recovery"])("hashes actual %s instructions and freezes the loaded snapshot", async (section) => {
    const root = await fixture();
    const original = loadHarness(root);
    await writeFile(join(root, "instructions", `${section}.md`), "Changed instruction.\n");
    const candidate = loadHarness(root);
    expect(candidate.hash).not.toBe(original.hash);
    expect(candidate.systemPrompt).toContain("Changed instruction.");
    expect(original.systemPrompt).not.toContain("Changed instruction.");
    expect(Object.isFrozen(original)).toBe(true);
    expect(Object.isFrozen(original.artifact)).toBe(true);
    expect(Object.isFrozen(original.artifact.instructions)).toBe(true);
    expect(Object.isFrozen(original.artifact.runtime_control)).toBe(true);
    expect(Object.isFrozen(original.artifact.skill_overlays)).toBe(true);
  });

  it("pins the frozen policy source and rejects a changed permission digest", async () => {
    const root = await fixture();
    const manifest = JSON.parse(await readFile(join(root, "manifest.json"), "utf8"));
    const source = (await readFile(new URL("../../go/prompts/agent/runtime-policy.md", import.meta.url), "utf8")).trim();
    expect(source).toBe(RUNTIME_POLICY);
    expect(createHash("sha256").update(source).digest("hex")).toBe(manifest.frozen_policy_sha256);
    expect(source).toContain("It is not authorization");
    expect(source).toContain("Never call a confirmation or materialization operation");
    manifest.frozen_policy_sha256 = "0".repeat(64);
    await writeFile(join(root, "manifest.json"), JSON.stringify(manifest));
    expect(() => loadHarness(root)).toThrow(/frozen policy/u);
  });

  it.each([
    { runtime_control: { token_budget: 100 } },
    { runtime_control: { acceptance_threshold: 0 } },
    { skill_overlays: { "graph-editing": "Unreviewed overlay" } },
    { controller_budget: 100 },
    { schema_version: 2 },
  ])("rejects undeclared fields or inactive editable surfaces: %j", async (change) => {
    const root = await fixture();
    const manifest = JSON.parse(await readFile(join(root, "manifest.json"), "utf8"));
    await writeFile(join(root, "manifest.json"), JSON.stringify({ ...manifest, ...change }));
    expect(() => loadHarness(root)).toThrow(/manifest/u);
  });

  it("fails on missing or empty instructions instead of falling back to the old policy", async () => {
    const root = await fixture();
    const path = join(root, "instructions", "execution.md");
    await writeFile(path, " \n");
    expect(() => loadHarness(root)).toThrow(/empty/u);
    await rm(path);
    expect(() => loadHarness(root)).toThrow();
  });

  it("preserves all existing behavioral and protocol guidance after the split", () => {
    const harness = loadHarness();
    expect(harness.systemPrompt).toContain("text enclosed in quotation marks as the user's literal value");
    expect(harness.systemPrompt).toContain("After a user answers a question, reread the current context");
    expect(harness.systemPrompt).toContain("Before any write, use the latest tool result");
    expect(harness.systemPrompt).toContain("use every returned issues[].path and issues[].message");
    expect(harness.systemPrompt).toContain("Never end a Turn with a question only in assistant prose");
    expect(harness.artifact.skill_overlays).toEqual({});
    expect(harness.artifact.runtime_control).toEqual({});
  });
});
