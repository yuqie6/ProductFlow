import { outputCount, outputText } from "./utils";

export function NodeOutputPreview({ output }: { output: Record<string, unknown> }) {
  const copyReady = Boolean(outputText(output, "copy_set_id"));
  const posterCount = outputCount(output, "poster_variant_ids");
  const filledCount = outputCount(output, "filled_source_asset_ids");
  const imageCount = Math.max(
    outputCount(output, "source_asset_ids"),
    outputCount(output, "image_asset_ids"),
  );
  const targetCount =
    typeof output.target_count === "number" ? output.target_count : null;
  const size = outputText(output, "size");
  const facts = [
    copyReady ? "文案 已生成" : null,
    posterCount ? `图片 ${posterCount}` : null,
    filledCount
      ? `参考图 ${filledCount}`
      : imageCount
        ? `参考图 ${imageCount}`
        : null,
    targetCount ? `槽位 ${targetCount}` : null,
    size,
  ].filter((item): item is string => Boolean(item));

  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 p-3">
      <div className="mb-2 text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
        输出
      </div>
      {typeof output.summary === "string" ? (
        <div className="mb-2 text-xs text-zinc-700">{output.summary}</div>
      ) : null}
      {facts.length ? (
        <div className="flex flex-wrap gap-1.5">
          {facts.map((item) => (
            <span
              key={item}
              className="rounded-full border border-zinc-200 bg-white px-2 py-0.5 text-[10px] text-zinc-500"
            >
              {item}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}
