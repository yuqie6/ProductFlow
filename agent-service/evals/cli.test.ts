import { describe, expect, it } from "vitest";

import { runCLI, type CLIIO } from "./cli.js";

describe("agent eval CLI", () => {
  it("prints help without importing the live runner", async () => {
    const capture = capturedIO();
    expect(await runCLI(["help"], capture.io)).toBe(0);
    expect(capture.stdout()).toContain("evals/cli.ts report");
    expect(capture.stdout()).toContain("run-sim");
    expect(capture.stdout()).toContain("run-adversarial");
    expect(capture.stdout()).toContain("judge-calibrate");
    expect(capture.stdout()).toContain("freeze-collection");
    expect(capture.stdout()).toContain("export-development");
    expect(capture.stderr()).toBe("");
  });

  it("passes explicit collection identity to the existing live runner", async () => {
    const capture = capturedIO();
    let options: unknown;
    expect(await runCLI(["run-live", "--collection", "/fixture/manifest.json", "--purpose", "development"], capture.io, {
      runLiveEvals: async (input) => { options = input; return { report: { ok: true } }; },
    })).toBe(0);
    expect(options).toMatchObject({ collectionPath: "/fixture/manifest.json", collectionPurpose: "development" });
    expect(await runCLI(["export-development", "run"], capture.io)).toBe(1);
    expect(capture.stderr()).toContain("--collection is required");
  });

  it("rejects missing arguments and invalid numeric options", async () => {
    const missing = capturedIO();
    expect(await runCLI(["report"], missing.io)).toBe(1);
    expect(missing.stderr()).toContain("report <run_id>");

    const invalid = capturedIO();
    expect(await runCLI(["diff", "a", "b", "--k", "0"], invalid.io)).toBe(1);
    expect(invalid.stderr()).toContain("--k must be a positive integer");
  });

  it("returns 0 from run-live only when the stubbed report is ok", async () => {
    const capture = capturedIO();
    expect(await runCLI(["run-live"], capture.io, {
      runLiveEvals: async () => ({ report: { ok: true, rows: [] } }),
    })).toBe(0);
    expect(capture.stdout()).toContain('"ok": true');
    expect(capture.stderr()).toBe("");
  });

  it("returns 1 from run-live when the stubbed report is not ok", async () => {
    const capture = capturedIO();
    expect(await runCLI(["run-live"], capture.io, {
      runLiveEvals: async () => ({ report: { ok: false, rows: [{ id: "task#1", ok: false }] } }),
    })).toBe(1);
    expect(capture.stdout()).toContain('"ok": false');
  });

  it("returns 1 from run-live when the stubbed runner throws", async () => {
    const capture = capturedIO();
    expect(await runCLI(["run-live"], capture.io, {
      runLiveEvals: async () => {
        throw new Error("model unavailable");
      },
    })).toBe(1);
    expect(capture.stderr()).toContain("model unavailable");
  });

  it("returns 1 from run-live when storage finalization fails", async () => {
    const capture = capturedIO();
    expect(await runCLI(["run-live"], capture.io, {
      runLiveEvals: async () => {
        throw new Error("cannot publish latest.json for an unsuccessful Agent eval run");
      },
    })).toBe(1);
    expect(capture.stderr()).toContain("cannot publish latest.json");
  });
});

function capturedIO(): { io: CLIIO; stdout: () => string; stderr: () => string } {
  const stdout: string[] = [];
  const stderr: string[] = [];
  return {
    io: { stdout: (text) => stdout.push(text), stderr: (text) => stderr.push(text) },
    stdout: () => stdout.join(""),
    stderr: () => stderr.join(""),
  };
}
