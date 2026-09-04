import { pathToFileURL } from "node:url";
import { join } from "node:path";

import {
  collectCoverage,
  diffReports,
  formatCoverageReport,
  formatReportDiff,
  formatRunReport,
  loadEvalHistory,
  loadRunReport,
  saturationWarnings,
} from "./report.js";
import { evalStorageRoot } from "./run-storage.js";

export interface CLIIO {
  stdout: (text: string) => void;
  stderr: (text: string) => void;
}

export interface CLILiveEvalOptions {
  trials?: number;
  filter?: string;
  suite?: string;
  concurrency?: number;
}

export interface CLIDeps {
  runLiveEvals?: (options?: CLILiveEvalOptions) => Promise<{
    runID?: string;
    runDir?: string;
    report: { ok: boolean };
  }>;
}

interface ParsedArgs {
  positionals: string[];
  options: Map<string, string>;
}

const DEFAULT_IO: CLIIO = {
  stdout: (text) => process.stdout.write(text),
  stderr: (text) => process.stderr.write(text),
};

export async function runCLI(args: readonly string[], io: CLIIO = DEFAULT_IO, deps: CLIDeps = {}): Promise<number> {
  try {
    const [command, ...rest] = args;
    if (!command || command === "help" || command === "--help" || command === "-h") {
      io.stdout(`${usage()}\n`);
      return command ? 0 : 1;
    }
    switch (command) {
      case "report":
        return await reportCommand(rest, io);
      case "diff":
        return await diffCommand(rest, io);
      case "coverage":
        return await coverageCommand(rest, io);
      case "run-live":
        return await runLiveCommand(rest, io, deps);
      case "run-sim":
        return await runSimCommand(rest, io);
      case "run-adversarial":
        return await runAdversarialCommand(rest, io);
      case "mutate":
        return await mutateCommand(rest, io);
      case "export-labels":
        return await exportLabelsCommand(rest, io);
      case "import-labels":
        return await importLabelsCommand(rest, io);
      case "judge":
        return await judgeCommand(rest, io);
      case "judge-calibrate":
        return await judgeCalibrateCommand(rest, io);
      default:
        throw new Error(`unknown command: ${command}\n${usage()}`);
    }
  } catch (error) {
    io.stderr(`agent-evals: ${error instanceof Error ? error.message : String(error)}\n`);
    return 1;
  }
}

async function mutateCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set());
  requirePositionals(parsed, 0, "mutate");
  process.env.PRODUCTFLOW_RUN_AGENT_EVALS = "1";
  const { runMutationEvals } = await import("./mutate.js");
  const report = await runMutationEvals();
  io.stdout(`${JSON.stringify(report, null, 2)}\n`);
  return report.scorable > 0 ? 0 : 1;
}

async function reportCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["storage-root", "k", "baseline"]));
  requirePositionals(parsed, 1, "report <run_id>");
  const storageRoot = parsed.options.get("storage-root");
  const passK = optionalPositiveInteger(parsed.options.get("k"), "--k");
  const report = await loadRunReport(parsed.positionals[0], { storageRoot, passK });
  io.stdout(`${formatRunReport(report)}\n`);
  const baselineRunId = parsed.options.get("baseline");
  if (baselineRunId) {
    const baseline = await loadRunReport(baselineRunId, { storageRoot, passK: report.passK });
    io.stdout(`\n${formatReportDiff(diffReports(baseline, report))}\n`);
  }
  const history = await loadEvalHistory({ storageRoot, limit: 8 });
  const warnings = saturationWarnings(history);
  if (warnings.length > 0) io.stdout(`\n${warnings.map((line) => `saturation: ${line}`).join("\n")}\n`);
  return 0;
}

async function diffCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["storage-root", "k"]));
  requirePositionals(parsed, 2, "diff <baseline_run_id> <candidate_run_id>");
  const storageRoot = parsed.options.get("storage-root");
  const requestedK = optionalPositiveInteger(parsed.options.get("k"), "--k");
  const candidate = await loadRunReport(parsed.positionals[1], { storageRoot, passK: requestedK });
  const baseline = await loadRunReport(parsed.positionals[0], { storageRoot, passK: candidate.passK });
  io.stdout(`${formatReportDiff(diffReports(baseline, candidate))}\n`);
  io.stdout(
    `\nModel compare: run the same task set against two AGENT_PROVIDER_MODEL values, then diff the two run_ids. Capability suites have no pass gate; regression still uses pass^1 >= 0.95 and pass^3 >= 0.90.\n`,
  );
  return 0;
}

async function coverageCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["tasks"]));
  requirePositionals(parsed, 0, "coverage");
  const report = await collectCoverage(parsed.options.get("tasks"));
  io.stdout(`${formatCoverageReport(report)}\n`);
  return report.complete ? 0 : 1;
}

async function runLiveCommand(args: readonly string[], io: CLIIO, deps: CLIDeps): Promise<number> {
  const parsed = parseArgs(args, new Set(["trials", "filter", "suite", "concurrency"]));
  requirePositionals(parsed, 0, "run-live");
  const trials = optionalPositiveInteger(parsed.options.get("trials"), "--trials");
  const concurrency = optionalPositiveInteger(parsed.options.get("concurrency"), "--concurrency");
  if (trials !== undefined) process.env.PRODUCTFLOW_AGENT_EVAL_TRIALS = String(trials);
  setOptionalEnv("PRODUCTFLOW_AGENT_EVAL_FILTER", parsed.options.get("filter"));
  setOptionalEnv("PRODUCTFLOW_AGENT_EVAL_SUITE", parsed.options.get("suite"));
  process.env.PRODUCTFLOW_RUN_AGENT_EVALS = "1";

  const run = deps.runLiveEvals ?? (await import("./live-runner.js")).runLiveEvals;
  const result = await run({
    trials,
    filter: parsed.options.get("filter"),
    suite: parsed.options.get("suite"),
    concurrency,
  });
  io.stdout(`${JSON.stringify({ run_id: result.runID, run_dir: result.runDir, ok: result.report.ok }, null, 2)}\n`);
  return result.report.ok ? 0 : 1;
}

async function runSimCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["trials", "filter"]));
  requirePositionals(parsed, 0, "run-sim");
  process.env.PRODUCTFLOW_RUN_AGENT_EVALS = "1";
  const { runUserSimEvals } = await import("./user-sim.js");
  const result = await runUserSimEvals({
    trials: optionalPositiveInteger(parsed.options.get("trials"), "--trials"),
    filter: parsed.options.get("filter"),
  });
  io.stdout(`${JSON.stringify({ run_id: result.runID, run_dir: result.runDir, ok: result.report.ok }, null, 2)}\n`);
  return result.report.ok ? 0 : 1;
}

async function runAdversarialCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["trials", "filter", "concurrency"]));
  requirePositionals(parsed, 0, "run-adversarial");
  process.env.PRODUCTFLOW_RUN_AGENT_EVALS = "1";
  const { runAdversarialEvals } = await import("./injections.js");
  const result = await runAdversarialEvals({
    trials: optionalPositiveInteger(parsed.options.get("trials"), "--trials") ?? 1,
    filter: parsed.options.get("filter"),
    concurrency: optionalPositiveInteger(parsed.options.get("concurrency"), "--concurrency"),
  });
  io.stdout(`${JSON.stringify(result, null, 2)}\n`);
  return result.metrics.passed_gates ? 0 : 1;
}

async function exportLabelsCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["n", "storage-root"]));
  requirePositionals(parsed, 1, "export-labels <run_id>");
  const n = optionalPositiveInteger(parsed.options.get("n"), "--n") ?? 50;
  const runDir = join(evalStorageRootFrom(parsed.options.get("storage-root")), "agent-evals", parsed.positionals[0]);
  const dest = join(evalStorageRootFrom(parsed.options.get("storage-root")), "agent-evals", "labeling");
  const { exportLabelTemplate } = await import("./graders/judge.js");
  const path = await exportLabelTemplate(runDir, n, dest);
  io.stdout(`${path}\n`);
  return 0;
}

async function importLabelsCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set());
  requirePositionals(parsed, 1, "import-labels <file>");
  const { importLabels } = await import("./graders/judge.js");
  const path = await importLabels(parsed.positionals[0]);
  io.stdout(`${path}\n`);
  return 0;
}

async function judgeCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["storage-root"]));
  requirePositionals(parsed, 1, "judge <run_id>");
  const runDir = join(evalStorageRootFrom(parsed.options.get("storage-root")), "agent-evals", parsed.positionals[0]);
  const { scoreTranscriptsWithJudge, judgeScoresIncludePass } = await import("./graders/judge.js");
  const scores = await scoreTranscriptsWithJudge(runDir);
  const unknown = scores.filter((score) => score.unknown).length;
  io.stdout(`${JSON.stringify({ count: scores.length, unknown, calibrated: false, note: "trend only until kappa >= 0.7" }, null, 2)}\n`);
  return unknown === scores.length ? 1 : 0;
}

async function judgeCalibrateCommand(args: readonly string[], io: CLIIO): Promise<number> {
  const parsed = parseArgs(args, new Set(["human", "judge"]));
  requirePositionals(parsed, 0, "judge-calibrate");
  const human = parsed.options.get("human");
  const judgePath = parsed.options.get("judge");
  if (!human || !judgePath) throw new Error("usage: judge-calibrate --human <labels.jsonl> --judge <judge-scores.jsonl>");
  const { calibrateJudge, loadJudgeScores, judgeScoresIncludePass } = await import("./graders/judge.js");
  const scores = await loadJudgeScores(judgePath);
  const report = await calibrateJudge(human, scores);
  const calibrated = judgeScoresIncludePass(report);
  io.stdout(`${JSON.stringify({ kappa: report, calibrated, gate: 0.7 }, null, 2)}\n`);
  return calibrated ? 0 : 1;
}

function evalStorageRootFrom(storageRoot?: string): string {
  if (storageRoot) return storageRoot;
  return evalStorageRoot();
}

function parseArgs(args: readonly string[], allowed: ReadonlySet<string>): ParsedArgs {
  const positionals: string[] = [];
  const options = new Map<string, string>();
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    if (!arg.startsWith("--")) {
      positionals.push(arg);
      continue;
    }
    const name = arg.slice(2);
    if (!allowed.has(name)) throw new Error(`unknown option --${name}`);
    if (options.has(name)) throw new Error(`duplicate option --${name}`);
    const value = args[index + 1];
    if (!value || value.startsWith("--")) throw new Error(`option --${name} requires a value`);
    options.set(name, value);
    index += 1;
  }
  return { positionals, options };
}

function requirePositionals(parsed: ParsedArgs, count: number, command: string): void {
  if (parsed.positionals.length !== count) {
    throw new Error(`usage: pnpm exec tsx evals/cli.ts ${command}`);
  }
}

function optionalPositiveInteger(raw: string | undefined, name: string): number | undefined {
  if (raw === undefined) return undefined;
  const value = Number(raw);
  if (!Number.isInteger(value) || value < 1) throw new Error(`${name} must be a positive integer`);
  return value;
}

function setOptionalEnv(name: string, value: string | undefined): void {
  if (value === undefined) delete process.env[name];
  else process.env[name] = value;
}

function usage(): string {
  return [
    "Usage:",
    "  pnpm exec tsx evals/cli.ts report <run_id> [--baseline <run_id>] [--k <n>] [--storage-root <path>]",
    "  pnpm exec tsx evals/cli.ts diff <baseline_run_id> <candidate_run_id> [--k <n>] [--storage-root <path>]",
    "  pnpm exec tsx evals/cli.ts coverage [--tasks <path>]",
    "  pnpm exec tsx evals/cli.ts run-live [--trials <n>] [--filter <task_or_skill>] [--suite <suite>] [--concurrency <n>]",
    "  pnpm exec tsx evals/cli.ts run-sim [--trials <n>] [--filter <task_or_skill>]",
    "  pnpm exec tsx evals/cli.ts run-adversarial [--trials <n>] [--filter <task_or_skill>]",
    "  pnpm exec tsx evals/cli.ts mutate",
    "  pnpm exec tsx evals/cli.ts export-labels <run_id> [--n 50]",
    "  pnpm exec tsx evals/cli.ts import-labels <file>",
    "  pnpm exec tsx evals/cli.ts judge <run_id>",
    "  pnpm exec tsx evals/cli.ts judge-calibrate --human <labels.jsonl> --judge <judge-scores.jsonl>",
  ].join("\n");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  process.exitCode = await runCLI(process.argv.slice(2));
}
