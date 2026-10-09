function errorText(value: unknown): string {
  if (typeof value === "string") return value.trim();
  if (!value || typeof value !== "object") return "";
  const error = value as Record<string, unknown>;
  const message = typeof error.msg === "string" ? error.msg : typeof error.message === "string" ? error.message : "";
  if (!message) return "";
  const field = Array.isArray(error.loc)
    ? error.loc.filter((part) => typeof part === "string" || typeof part === "number")
        .filter((part) => !["body", "query", "path"].includes(String(part))).join(".")
    : typeof error.field === "string" ? error.field : "";
  return field ? `${field === "prompt" ? "提示词" : field}：${message}` : message;
}

/** 将各种 API 错误转换为文本，禁止把校验对象直接交给 React。 */
export function normalizeApiErrorDetail(value: unknown, fallback = "请求失败"): string {
  const message = Array.isArray(value)
    ? value.map(errorText).filter(Boolean).join("；")
    : errorText(value);
  return message || fallback;
}
