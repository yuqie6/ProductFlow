import { describe, expect, it, vi } from "vitest";

import { submitAfterSuccessfulFlush, withGraphRunSubmit } from "./graphRunLock";

describe("submitAfterSuccessfulFlush", () => {
  it("does not create a run when inspector flush fails", async () => {
    const submit = vi.fn();
    const submitted = await submitAfterSuccessfulFlush(
      async () => {
        throw new Error("草稿冲突");
      },
      submit,
    );
    expect(submitted).toBe(false);
    expect(submit).not.toHaveBeenCalled();
  });

  it("submits only after flush succeeds", async () => {
    const submit = vi.fn(async () => undefined);
    const submitted = await submitAfterSuccessfulFlush(async () => undefined, submit);
    expect(submitted).toBe(true);
    expect(submit).toHaveBeenCalledOnce();
  });
});

describe("withGraphRunSubmit", () => {
  it("rejects a second in-flight submit", async () => {
    let release!: () => void;
    const first = new Promise<void>((resolve) => {
      release = resolve;
    });
    const firstSubmit = withGraphRunSubmit(() => first);
    const second = await withGraphRunSubmit(async () => "nope");
    expect(second).toBeUndefined();
    release();
    await firstSubmit;
  });
});
