import { describe, expect, it } from "vitest";
import { ApiError, api } from "./api";
import { normalizeApiErrorDetail } from "./apiErrors";
import { vi } from "vitest";

describe("API 校验错误回显", () => {
  it("把 FastAPI 校验对象数组转换成文本并忽略原始输入", () => {
    const detail = [{ type: "string_too_long", loc: ["body", "prompt"], msg: "最多 4000 字符", input: "原始输入" }];
    const error = new ApiError(422, detail);
    expect(error.detail).toBe("提示词：最多 4000 字符");
    expect(error.message).toBe(error.detail);
    expect(error.detail).not.toContain("原始输入");
  });

  it("合并多个字段错误，兼容纯文本及异常结构", () => {
    expect(normalizeApiErrorDetail([{ loc: ["body", "size"], msg: "尺寸不合法" }, { field: "n", message: "数量不合法" }]))
      .toBe("size：尺寸不合法；n：数量不合法");
    expect(normalizeApiErrorDetail("  请求参数不合法  ")).toBe("请求参数不合法");
    for (const detail of [null, undefined, 123, {}, [], [{ input: "隐藏输入" }]]) {
      expect(normalizeApiErrorDetail(detail)).toBe("请求失败");
    }
  });

  it("真实 fetch 错误路径始终抛出可渲染的字符串", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      detail: [{ loc: ["body", "prompt"], msg: "提示词过长", ctx: { max_length: 4000 } }],
    }), { status: 422 })));
    try {
      await expect(api.getRuntimeConfig()).rejects.toMatchObject({ status: 422, detail: "提示词：提示词过长" });
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
