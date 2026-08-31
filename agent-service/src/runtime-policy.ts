/**
 * ProductFlow Pi 运行时政策。构建时从 go/prompts/agent/runtime-policy.md 打包。
 */

import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { RUNTIME_POLICY } from "./runtime-policy.generated.js";

export function runtimePolicyPath(fromFile = fileURLToPath(import.meta.url)): string {
  return join(dirname(fromFile), "../../go/prompts/agent/runtime-policy.md");
}

export function loadRuntimePolicy(fromFile?: string): string {
  void fromFile;
  if (!RUNTIME_POLICY) throw new Error("ProductFlow runtime policy is empty");
  return RUNTIME_POLICY;
}
