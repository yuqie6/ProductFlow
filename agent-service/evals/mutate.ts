import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { evalStorageRoot, newEvalRunID } from "./run-storage.js";
import { runLiveEvals } from "./live-runner.js";

const SKILLS_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "../.pi/skills");

interface Mutation {
  id: string;
  skill: string;
  task: string;
  file: string;
  apply: (source: string) => string;
}

export interface MutationResult {
  id: string;
  skill: string;
  task_id: string;
  status: "killed" | "survived" | "unscorable";
  baseline_run_id: string;
  mutant_run_id: string;
  baseline_passed: boolean;
  mutant_passed: boolean;
}

export interface MutationReport {
  schema_version: 1;
  run_id: string;
  created_at: string;
  killed: number;
  scorable: number;
  kill_rate: number | null;
  results: MutationResult[];
  artifact: string;
}

const MUTATIONS: readonly Mutation[] = [
  {
    id: "remove-unconfirmed-finalize-guard",
    skill: "product-intake",
    task: "product-intake-clarify-image-types",
    file: "product-intake/SKILL.md",
    apply: (source) => replaceRequired(source, "不要在用户未确认时直接 finalize 这一套。", ""),
  },
  {
    id: "rename-node-example-to-add-node",
    skill: "graph-editing",
    task: "graph-editing-rename-node",
    file: "graph-editing/SKILL.md",
    apply: (source) => replaceRequired(source, "`rename_node`", "`add_node`"),
  },
  {
    id: "remove-read-context-first",
    skill: "graph-editing",
    task: "graph-editing-rename-node",
    file: "graph-editing/SKILL.md",
    apply: (source) => replaceRequired(source, "先读 `get_product_workflow_context_v1`。", ""),
  },
  {
    id: "swap-apply-propose-guidance",
    skill: "graph-editing",
    task: "graph-editing-propose-scene-shot",
    file: "graph-editing/SKILL.md",
    apply: swapApplyAndPropose,
  },
];

export async function runMutationEvals(): Promise<MutationReport> {
  const baselineByTask = new Map<string, Awaited<ReturnType<typeof runLiveEvals>>>();
  const results: MutationResult[] = [];
  for (const mutation of MUTATIONS) {
    let baseline = baselineByTask.get(mutation.task);
    if (!baseline) {
      baseline = await runLiveEvals({ trials: 1, filter: mutation.task, concurrency: 1 });
      baselineByTask.set(mutation.task, baseline);
    }
    const root = await mkdtemp(join(tmpdir(), `productflow-agent-mutation-${mutation.id}-`));
    const skillRoot = join(root, "skills");
    try {
      await cp(SKILLS_ROOT, skillRoot, { recursive: true });
      const target = join(skillRoot, mutation.file);
      const original = await readFile(target, "utf8");
      await writeFile(target, mutation.apply(original), "utf8");
      const mutant = await runLiveEvals({ trials: 1, filter: mutation.task, concurrency: 1, skillRoot });
      const baselinePassed = baseline.report.ok;
      const mutantPassed = mutant.report.ok;
      results.push({
        id: mutation.id,
        skill: mutation.skill,
        task_id: mutation.task,
        status: !baselinePassed ? "unscorable" : mutantPassed ? "survived" : "killed",
        baseline_run_id: baseline.runID,
        mutant_run_id: mutant.runID,
        baseline_passed: baselinePassed,
        mutant_passed: mutantPassed,
      });
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  }
  const killed = results.filter((row) => row.status === "killed").length;
  const scorable = results.filter((row) => row.status !== "unscorable").length;
  const runID = `mutate-${newEvalRunID()}`;
  const artifact = join(evalStorageRoot(), "agent-evals", "mutations", `${runID}.json`);
  const report: MutationReport = {
    schema_version: 1,
    run_id: runID,
    created_at: new Date().toISOString(),
    killed,
    scorable,
    kill_rate: scorable === 0 ? null : killed / scorable,
    results,
    artifact,
  };
  await writeFileEnsuringParent(artifact, report);
  return report;
}

function replaceRequired(source: string, from: string, to: string): string {
  if (!source.includes(from)) throw new Error(`mutation anchor is missing: ${from}`);
  return source.replace(from, to);
}

function swapApplyAndPropose(source: string): string {
  const apply = "一次可逆编辑（一个节点配置、一条边、一次改名）：`apply_graph_change_set_v1`";
  const propose = "多节点重建、加一组镜头、批量删除：`propose_graph_change_set_v1`";
  if (!source.includes(apply) || !source.includes(propose)) throw new Error("apply/propose guidance anchors are missing");
  return source.replace(apply, "__EVAL_APPLY_GUIDANCE__")
    .replace(propose, apply)
    .replace("__EVAL_APPLY_GUIDANCE__", propose);
}

async function writeFileEnsuringParent(path: string, value: unknown): Promise<void> {
  const { mkdir } = await import("node:fs/promises");
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

export const mutationDefinitions = MUTATIONS.map(({ id, skill, task, file }) => ({ id, skill, task, file }));
