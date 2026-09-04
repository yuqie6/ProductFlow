import { describe, expect, it } from "vitest";

import { gradeUserSim, scriptedTurnAnswer, unconfirmedWriteCount } from "./user-sim.js";
import { loadEvalTaskSet } from "./loader.js";

describe("L3 user simulator graders", () => {
  it("counts finalize/apply before the user agrees as unconfirmed writes", () => {
    const calls = [
      { name: "get_product_workflow_context_v1", params: {}, ts: "t0" },
      { name: "finalize_product_intake_v1", params: {}, ts: "t1" },
    ];
    expect(unconfirmedWriteCount(calls, false)).toBe(1);
    expect(unconfirmedWriteCount(calls, true)).toBe(0);
  });

  it("maps scripted selected_option answers onto TurnAnswer", () => {
    expect(scriptedTurnAnswer({
      when: "question",
      action: "answer",
      answer: { selected_option: 1 },
    })).toEqual({ option: 1 });
  });

  it("loads five L3 flows with user_sim scripts", async () => {
    const { tasks } = await loadEvalTaskSet();
    const sim = tasks.filter((task) => task.layers.includes("l3"));
    expect(sim.length).toBeGreaterThanOrEqual(5);
    expect(sim.every((task) => task.user_sim && task.user_sim.scripted_answers.length > 0)).toBe(true);
    const intake = sim.find((task) => task.id === "product-intake-sim-two-round-clarify")!;
    const grade = gradeUserSim(intake, {
      terminal: "succeeded",
      calls: intake.reference.scripted_calls.map((call) => ({ ...call, ts: "ref" })),
      turns: 2,
      userAgreed: true,
    });
    expect(grade.unconfirmed_writes).toBe(0);
  });
});
