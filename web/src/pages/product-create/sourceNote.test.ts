import { describe, expect, it } from "vitest";

import {
  defaultCreateSourceNoteDraft,
  formatSourceNote,
  isCreateSourceNoteReady,
  newSourceNoteField,
  sourceNoteDraftFromGenerated,
} from "./sourceNote";

describe("create source note draft", () => {
  it("starts with prose only and treats empty specs as not ready", () => {
    const empty = defaultCreateSourceNoteDraft();
    expect(empty.fields).toEqual([]);
    expect(isCreateSourceNoteReady(empty)).toBe(false);
    expect(formatSourceNote(empty)).toBe("");
  });

  it("flattens returned specs and omits empty values so they do not invent facts", () => {
    const draft = defaultCreateSourceNoteDraft();
    draft.visible = "厚壁玻璃密封瓶";
    draft.fields = [
      newSourceNoteField("材质", "玻璃"),
      newSourceNoteField("容量", ""),
      newSourceNoteField("价格", ""),
    ];
    const text = formatSourceNote(draft);
    expect(text).toContain("厚壁玻璃密封瓶");
    expect(text).toContain("材质：玻璃");
    expect(text).not.toContain("容量：");
    expect(text).not.toContain("价格：");
    expect(text).not.toContain("认证");
  });

  it("maps generated JSON onto the editor without filling a fixed checklist", () => {
    const draft = sourceNoteDraftFromGenerated({
      visible: "玻璃密封瓶，球盖锁扣。",
      fields: [
        { label: "材质", value: "玻璃" },
        { label: "容量", value: "" },
      ],
    });
    expect(draft.visible).toBe("玻璃密封瓶，球盖锁扣。");
    expect(draft.fields.map((field) => field.label)).toEqual(["材质", "容量"]);
    expect(draft.fields[0]?.value).toBe("玻璃");
    expect(draft.fields[1]?.value).toBe("");
    expect(draft.fields.some((field) => field.label === "认证/资质")).toBe(false);
    expect(isCreateSourceNoteReady(draft)).toBe(true);
  });
});
