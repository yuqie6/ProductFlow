import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const RUBRICS_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "rubrics");
const LABELS_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "labels");
const SKILL_RUBRIC_FILES = new Set([
  "product-intake.md",
  "graph-editing.md",
  "workflow-run-request.md",
  "media-library-organization.md",
  "run-diagnosis.md",
]);

export interface RubricDimension {
  skill: string;
  id: string;
  title: string;
  body: string;
}

export interface JudgeScore {
  skill: string;
  dimension: string;
  score: number;
  reason: string;
  unknown: boolean;
}

export interface HumanLabel {
  task_id: string;
  trial: number;
  skill: string;
  dimension: string;
  score: number;
}

export function parseRubric(skill: string, markdown: string): RubricDimension[] {
  const sections = markdown.split(/^## /mu).slice(1);
  if (sections.length < 3 || sections.length > 5) {
    throw new Error(`${skill} rubric must have 3 to 5 dimensions`);
  }
  return sections.map((section) => {
    const [titleLine, ...rest] = section.trim().split("\n");
    const title = titleLine.trim();
    const id = title.toLowerCase().replace(/[^a-z0-9\u4e00-\u9fff]+/gu, "-").replace(/^-|-$/gu, "");
    return { skill, id, title, body: rest.join("\n").trim() };
  });
}

export async function loadRubrics(): Promise<Map<string, RubricDimension[]>> {
  const { readdir } = await import("node:fs/promises");
  const files = (await readdir(RUBRICS_ROOT)).filter((name) => SKILL_RUBRIC_FILES.has(name)).sort();
  const rubrics = new Map<string, RubricDimension[]>();
  for (const file of files) {
    const skill = file.replace(/\.md$/u, "");
    rubrics.set(skill, parseRubric(skill, await readFile(join(RUBRICS_ROOT, file), "utf8")));
  }
  return rubrics;
}

export function parseJudgeJSON(raw: string): Pick<JudgeScore, "score" | "reason" | "unknown"> {
  const match = /\{[\s\S]*\}/u.exec(raw);
  if (!match) throw new Error("judge output did not contain JSON");
  const value = JSON.parse(match[0]) as { score?: unknown; reason?: unknown; unknown?: unknown };
  if (typeof value.score !== "number" || value.score < 0 || value.score > 1) {
    throw new Error("judge score must be a number between 0 and 1");
  }
  return {
    score: value.score,
    reason: typeof value.reason === "string" ? value.reason : "",
    unknown: value.unknown === true,
  };
}

export function cohenKappa(left: readonly number[], right: readonly number[]): number {
  if (left.length !== right.length || left.length === 0) {
    throw new Error("kappa requires equal non-empty label series");
  }
  const categories = [...new Set([...left, ...right])].sort((a, b) => a - b);
  const n = left.length;
  const matrix = new Map<string, number>();
  const row = new Map<number, number>();
  const col = new Map<number, number>();
  for (let index = 0; index < n; index += 1) {
    const key = `${left[index]}\u0000${right[index]}`;
    matrix.set(key, (matrix.get(key) ?? 0) + 1);
    row.set(left[index], (row.get(left[index]) ?? 0) + 1);
    col.set(right[index], (col.get(right[index]) ?? 0) + 1);
  }
  let observed = 0;
  for (const category of categories) observed += (matrix.get(`${category}\u0000${category}`) ?? 0) / n;
  let expected = 0;
  for (const category of categories) {
    expected += ((row.get(category) ?? 0) / n) * ((col.get(category) ?? 0) / n);
  }
  if (expected === 1) return 1;
  return (observed - expected) / (1 - expected);
}

export async function exportLabelTemplate(runDir: string, n: number, destDir: string): Promise<string> {
  const transcriptsDir = join(runDir, "transcripts");
  const { readdir } = await import("node:fs/promises");
  const files = (await readdir(transcriptsDir)).filter((name) => name.endsWith(".json")).sort().slice(0, n);
  if (files.length < n) throw new Error(`run has ${files.length} transcripts, need ${n}`);
  const rubrics = await loadRubrics();
  const rows: Array<Record<string, unknown>> = [];
  for (const file of files) {
    const transcript = JSON.parse(await readFile(join(transcriptsDir, file), "utf8")) as {
      task_id: string;
      trial: number;
      output?: string;
    };
    const skill = skillFromTaskID(transcript.task_id, rubrics);
    const dimensions = rubrics.get(skill) ?? [...rubrics.values()][0];
    for (const dimension of dimensions) {
      rows.push({
        task_id: transcript.task_id,
        trial: transcript.trial,
        skill,
        dimension: dimension.id,
        output_excerpt: String(transcript.output ?? "").slice(0, 400),
        score: null,
      });
    }
  }
  await mkdir(destDir, { recursive: true });
  const path = join(destDir, "labels.template.jsonl");
  await writeFile(path, `${rows.map((row) => JSON.stringify(row)).join("\n")}\n`);
  return path;
}

export async function importLabels(source: string, dest = join(LABELS_ROOT, "imported.jsonl")): Promise<string> {
  const lines = (await readFile(source, "utf8")).trim().split("\n").filter(Boolean);
  const labels: HumanLabel[] = [];
  for (const [index, line] of lines.entries()) {
    const value = JSON.parse(line) as HumanLabel;
    if (!value.task_id || !value.dimension || typeof value.score !== "number") {
      throw new Error(`label line ${index + 1} is missing task_id, dimension, or numeric score`);
    }
    labels.push(value);
  }
  await mkdir(dirname(dest), { recursive: true });
  await writeFile(dest, `${labels.map((label) => JSON.stringify(label)).join("\n")}\n`);
  return dest;
}

export async function loadJudgeScores(path: string): Promise<JudgeScore[]> {
  return (await readFile(path, "utf8")).trim().split("\n").filter(Boolean).map((line) => JSON.parse(line) as JudgeScore);
}

export async function calibrateJudge(humanPath: string, judgeScores: readonly JudgeScore[]): Promise<Record<string, number>> {
  const humans = (await readFile(humanPath, "utf8")).trim().split("\n").filter(Boolean).map((line) => JSON.parse(line) as HumanLabel);
  const byDimension = new Map<string, { human: number[]; judge: number[] }>();
  for (const human of humans) {
    const judge = judgeScores.find((score) =>
      score.skill === human.skill && score.dimension === human.dimension
    );
    if (!judge || judge.unknown) continue;
    const bucket = byDimension.get(human.dimension) ?? { human: [], judge: [] };
    bucket.human.push(human.score >= 0.5 ? 1 : 0);
    bucket.judge.push(judge.score >= 0.5 ? 1 : 0);
    byDimension.set(human.dimension, bucket);
  }
  const report: Record<string, number> = {};
  for (const [dimension, series] of byDimension) {
    if (series.human.length < 2) continue;
    report[dimension] = cohenKappa(series.human, series.judge);
  }
  return report;
}

export function skillFromTaskID(taskID: string, rubrics: ReadonlyMap<string, RubricDimension[]>): string {
  const skills = [...rubrics.keys()].sort((left, right) => right.length - left.length);
  return skills.find((skill) => taskID === skill || taskID.startsWith(`${skill}-`)) ?? [...rubrics.keys()][0] ?? taskID;
}

export function judgeScoresIncludePass(kappas: Record<string, number>, minKappa = 0.7): boolean {
  const values = Object.values(kappas);
  return values.length > 0 && values.every((value) => value >= minKappa);
}

export async function scoreTranscriptsWithJudge(runDir: string): Promise<JudgeScore[]> {
  const transcriptsDir = join(runDir, "transcripts");
  const { readdir } = await import("node:fs/promises");
  const files = (await readdir(transcriptsDir)).filter((name) => name.endsWith(".json")).sort();
  const rubrics = await loadRubrics();
  const scores: JudgeScore[] = [];
  for (const file of files) {
    const transcript = JSON.parse(await readFile(join(transcriptsDir, file), "utf8")) as {
      task_id: string;
      output?: string;
    };
    const skill = skillFromTaskID(transcript.task_id, rubrics);
    const dimensions = rubrics.get(skill);
    if (!dimensions) continue;
    for (const dimension of dimensions) {
      scores.push(await scoreDimension(dimension, String(transcript.output ?? "")));
    }
  }
  await mkdir(runDir, { recursive: true });
  await writeFile(join(runDir, "judge-scores.jsonl"), `${scores.map((score) => JSON.stringify(score)).join("\n")}\n`);
  return scores;
}

export async function scoreDimension(dimension: RubricDimension, output: string): Promise<JudgeScore> {
  try {
    const raw = await callJudgeModel(judgePrompt(dimension, output));
    const parsed = parseJudgeJSON(raw);
    return { skill: dimension.skill, dimension: dimension.id, ...parsed };
  } catch (error) {
    return {
      skill: dimension.skill,
      dimension: dimension.id,
      score: 0,
      reason: error instanceof Error ? error.message : String(error),
      unknown: true,
    };
  }
}

async function callJudgeModel(prompt: string): Promise<string> {
  const apiKey = process.env.AGENT_PROVIDER_API_KEY?.trim();
  if (!apiKey) throw new Error("AGENT_PROVIDER_API_KEY is required for L4 judge");
  const model = process.env.AGENT_EVAL_JUDGE_MODEL?.trim() || process.env.AGENT_PROVIDER_MODEL?.trim();
  if (!model) throw new Error("set AGENT_EVAL_JUDGE_MODEL or AGENT_PROVIDER_MODEL");
  const base = (process.env.AGENT_PROVIDER_BASE_URL?.trim() || "https://api.openai.com/v1").replace(/\/$/u, "");
  const response = await fetch(`${base}/chat/completions`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${apiKey}`,
      "content-type": "application/json",
    },
    body: JSON.stringify({
      model,
      temperature: 0,
      messages: [{ role: "user", content: prompt }],
    }),
  });
  if (!response.ok) {
    throw new Error(`judge HTTP ${response.status}`);
  }
  const payload = await response.json() as { choices?: Array<{ message?: { content?: string } }> };
  const content = payload.choices?.[0]?.message?.content;
  if (!content) throw new Error("judge response missing content");
  return content;
}

export function judgePrompt(dimension: RubricDimension, output: string): string {
  return [
    `你是 ProductFlow Agent 评测的独立评审。只判断维度「${dimension.title}」。`,
    dimension.body,
    "输出唯一 JSON：{\"score\":0到1的数字,\"reason\":\"简体中文理由\",\"unknown\":false}。信息不足时 unknown=true 且 score=0。",
    "待评文本：",
    output.slice(0, 8000),
  ].join("\n");
}
