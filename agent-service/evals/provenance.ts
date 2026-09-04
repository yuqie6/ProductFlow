import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const MAX_GIT_OUTPUT_BYTES = 64 << 20;

export interface GitProvenance {
  commit: string;
  worktree_dirty: boolean;
  worktree_hash: string;
}

export function canonicalJSONStringify(value: unknown): string {
  return JSON.stringify(canonicalValue(value));
}

export function hashCanonicalJSON(value: unknown): string {
  return createHash("sha256").update(canonicalJSONStringify(value)).digest("hex");
}

export async function currentGitProvenance(): Promise<GitProvenance> {
  let commit = "unknown";
  try {
    commit = (await git(["rev-parse", "HEAD"])).trim();
  } catch {
    return { commit: "unknown", worktree_dirty: true, worktree_hash: "unknown" };
  }
  try {
    const repoRoot = (await git(["rev-parse", "--show-toplevel"])).trim();
    const status = await git(["status", "--porcelain=v1", "-z", "--untracked-files=all"]);
    const diffNames = await git(["diff", "--name-only", "HEAD", "--"]);
    const untracked = (await git(["ls-files", "--others", "--exclude-standard", "-z"]))
      .split("\0")
      .filter(Boolean)
      .sort();
    const untrackedHashes: Array<[string, string]> = [];
    for (const path of untracked) {
      const content = await readFile(join(repoRoot, path));
      untrackedHashes.push([path, createHash("sha256").update(content).digest("hex")]);
    }
    return {
      commit,
      worktree_dirty: status.length > 0,
      worktree_hash: hashCanonicalJSON({
        commit,
        status,
        diff_name_only: diffNames,
        untracked: untrackedHashes,
      }),
    };
  } catch {
    return { commit, worktree_dirty: true, worktree_hash: "unknown" };
  }
}

function canonicalValue(value: unknown): unknown {
  if (value === null || typeof value === "string" || typeof value === "boolean") return value;
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new Error("canonical JSON does not support non-finite numbers");
    return value;
  }
  if (Array.isArray(value)) return value.map(canonicalValue);
  if (typeof value === "object") {
    const record = value as Record<string, unknown>;
    const result: Record<string, unknown> = {};
    for (const key of Object.keys(record).sort()) {
      if (record[key] === undefined) throw new Error(`canonical JSON does not support undefined at ${key}`);
      result[key] = canonicalValue(record[key]);
    }
    return result;
  }
  throw new Error(`canonical JSON does not support ${typeof value}`);
}

async function git(args: string[]): Promise<string> {
  const result = await execFileAsync("git", args, { maxBuffer: MAX_GIT_OUTPUT_BYTES });
  return result.stdout;
}
