/**
 * Frozen held-in / held-out assignment for Agent eval tasks.
 *
 * Split is derived from a fixed seed and the task id. The held-out id list hash
 * is recorded so a later reshuffle is visible.
 */

import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const EVAL_SPLIT_SEED = "productflow-eval-split-v1";
export type EvalSplit = "held_in" | "held_out";

export interface SplitRegistry {
  schema_version: 1;
  seed: typeof EVAL_SPLIT_SEED;
  held_out_ids: string[];
  held_out_ids_sha256: string;
}

export function assignedEvalSplit(taskID: string): EvalSplit {
  const digest = createHash("sha256").update(`${EVAL_SPLIT_SEED}:${taskID}`).digest();
  return digest[0] % 3 === 0 ? "held_out" : "held_in";
}

export const assignedSplit = assignedEvalSplit;

export function taskSplit(task: { id: string; split?: EvalSplit }): EvalSplit {
  return task.split ?? assignedEvalSplit(task.id);
}

export function hashHeldOutIDs(ids: readonly string[]): string {
  return createHash("sha256").update([...ids].sort().join("\n")).digest("hex");
}

export function applyEvalSplit<T extends { id: string; split?: EvalSplit }>(task: T): T & { split: EvalSplit } {
  return { ...task, split: task.split ?? assignedEvalSplit(task.id) };
}

export function defaultSplitRegistryPath(): string {
  return join(dirname(fileURLToPath(import.meta.url)), "split-registry.json");
}

export async function loadSplitRegistry(path = defaultSplitRegistryPath()): Promise<SplitRegistry> {
  const parsed = JSON.parse(await readFile(path, "utf8")) as SplitRegistry;
  if (parsed.schema_version !== 1 || parsed.seed !== EVAL_SPLIT_SEED) {
    throw new Error("Agent eval split registry schema or seed mismatch");
  }
  const expected = hashHeldOutIDs(parsed.held_out_ids);
  if (expected !== parsed.held_out_ids_sha256) {
    throw new Error("Agent eval split registry held-out hash mismatch");
  }
  return parsed;
}

export function buildSplitRegistry(taskIDs: readonly string[]): SplitRegistry {
  const heldOut = taskIDs.filter((id) => assignedEvalSplit(id) === "held_out").sort();
  return {
    schema_version: 1,
    seed: EVAL_SPLIT_SEED,
    held_out_ids: heldOut,
    held_out_ids_sha256: hashHeldOutIDs(heldOut),
  };
}
