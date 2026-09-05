import { isDeepStrictEqual } from "node:util";
import { toolManifestEntry } from "../../src/tool-manifest.js";

import { result, type EvalCallRecord, type GradeResult, type WriteExpectation } from "./types.js";

const PATH_PART = /(?:^|\.)([^.[\]]+)|\[(\d+)\]/gu;

export function valueAtPath(value: unknown, path: string): unknown {
  let current = value;
  let consumed = "";
  for (const match of path.matchAll(PATH_PART)) {
    consumed += match[0];
    const key = match[1] ?? Number(match[2]);
    if (current === null || typeof current !== "object") return undefined;
    current = (current as Record<string | number, unknown>)[key];
  }
  return consumed.replace(/^\./u, "") === path ? current : undefined;
}

export function gradeWrites(
  expectations: readonly WriteExpectation[] | undefined,
  calls: readonly EvalCallRecord[],
): GradeResult {
  if (!expectations) return result([]);
  const errors: string[] = [];
  for (const expectation of expectations) {
    const candidates = calls.filter((call) => call.name === expectation.tool && call.outcome === "succeeded");
    if (candidates.length === 0) {
      errors.push(`expected write tool was not called: ${expectation.tool}`);
      continue;
    }
    const matched = candidates.some((call) => matchesWrite(expectation, call.params));
    if (!matched) {
      errors.push(`no ${expectation.tool} call matched ${JSON.stringify(expectation.match)}`);
    }
  }
  for (const call of calls) {
    if (!isBusinessWrite(call.name) || call.outcome === "failed") continue;
    if (call.outcome !== "succeeded" || !expectations.some((expectation) =>
      expectation.tool === call.name && matchesWrite(expectation, call.params))) {
      errors.push(`unexpected or unobserved write: ${call.name}`);
    }
  }
  return result(errors);
}

export function isBusinessWrite(name: string): boolean {
  const effect = toolManifestEntry(name)?.effect;
  return effect === "mutate" || (effect === "approval" && name !== "ask_user");
}

// Operations form a set of constrained effects; their array positions are not business identities.
export function matchesWrite(expectation: WriteExpectation, params: unknown): boolean {
  if (expectation.tool === "request_workflow_run_v1" || expectation.tool === "request_global_workflow_run_v1") {
    params = { force: false, document_action: "", source_run_id: null, ...(params as object) };
  }
  const operationPaths = new Map<string, Map<number, Record<string, unknown>>>();
  for (const [path, expected] of Object.entries(expectation.match)) {
    const op = /^(operations|library_payload\.operations)\[(\d+)\]\.(.+)$/u.exec(path);
    if (op) {
      const items = operationPaths.get(op[1]) ?? new Map<number, Record<string, unknown>>();
      const constraints = items.get(Number(op[2])) ?? {};
      constraints[op[3]] = expected;
      items.set(Number(op[2]), constraints);
      operationPaths.set(op[1], items);
    } else if (!equalBusinessValue(path, valueAtPath(params, path), expected)) return false;
  }
  for (const [path, items] of operationPaths) {
    const actual = valueAtPath(params, path);
    if (!Array.isArray(actual) || actual.length !== items.size) return false;
    const constraints = [...items.values()];
    const assign = (index: number, remaining: unknown[], bindings: Map<string, unknown>): boolean => index === constraints.length
      || remaining.some((item, candidate) => {
        const next = new Map(bindings);
        return Object.entries(constraints[index]).every(([key, expected]) => {
          const value = valueAtPath(item, key);
          if (typeof expected === "string" && expected.startsWith("$")) {
            if (typeof value !== "string" || !value) return false;
            if (next.has(expected)) return next.get(expected) === value;
            if ([...next.values()].includes(value)) return false;
            next.set(expected, value);
            return true;
          }
          return equalBusinessValue(key, value, expected);
        }) && assign(index + 1, remaining.filter((_, other) => other !== candidate), next);
      });
    if (!assign(0, actual, new Map())) return false;
  }
  return true;
}

function equalBusinessValue(path: string, actual: unknown, expected: unknown): boolean {
  if (Array.isArray(actual) && Array.isArray(expected) && /(?:^|\.)(?:node_ids|node_refs|workflow_ids|product_ids|asset_ids|reference_asset_ids|tag_names|nodes)$/u.test(path)) {
    return isDeepStrictEqual([...actual].sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b))),
      [...expected].sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b))));
  }
  return isDeepStrictEqual(actual, expected);
}
