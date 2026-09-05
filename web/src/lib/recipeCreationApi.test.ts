import { afterEach, expect, it, vi } from "vitest";
import { api } from "./api";

afterEach(() => vi.unstubAllGlobals());

it("previews without a target and confirms with a stable key and exact recipe version/digest", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
  vi.stubGlobal("fetch", fetchMock);
  await api.previewRecipeCreation("recipe/1", 3);
  const image = new File(["reference"], "product.png", { type: "image/png" });
  const input = {
    name: "new product",
    images: [image],
    sourceNote: "target facts",
    recipeId: "recipe/1",
    expectedRecipeVersion: 3,
    previewDigest: "digest",
    idempotencyKey: "confirmation-key",
  };
  await api.createProductFromRecipe(input);
  await api.createProductFromRecipe(input);
  expect(fetchMock.mock.calls[0][0]).toBe(
    "/api/v3/workflow-recipes/recipe%2F1/creation-preview",
  );
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    expected_recipe_version: 3,
  });
  for (const [url, init] of fetchMock.mock.calls.slice(1)) {
    expect(url).toBe("/api/v3/products/from-recipe");
    expect(init.headers).toEqual({ "Idempotency-Key": "confirmation-key" });
    expect(init.credentials).toBe("include");
    expect(Object.fromEntries(init.body.entries())).toEqual({
      name: "new product",
      images: image,
      source_note: "target facts",
      recipe_id: "recipe/1",
      expected_recipe_version: "3",
      preview_digest: "digest",
    });
  }
});
