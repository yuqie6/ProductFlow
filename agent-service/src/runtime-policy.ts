/**
 * ProductFlow Pi 运行时政策。正文在 go/prompts/agent/runtime-policy.md。
 */

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export function runtimePolicyPath(fromFile = fileURLToPath(import.meta.url)): string {
  return join(dirname(fromFile), "../../go/prompts/agent/runtime-policy.md");
}

export function loadRuntimePolicy(fromFile?: string): string {
  const path = runtimePolicyPath(fromFile);
  const text = readFileSync(path, "utf8").trim();
  if (!text) {
    throw new Error(`ProductFlow runtime policy is empty: ${path}`);
  }
  return text;
}
