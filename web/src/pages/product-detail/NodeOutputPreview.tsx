import { CheckCircle2, Image as ImageIcon, Sparkles } from "lucide-react";

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
    <div className="overflow-hidden rounded-xl border border-zinc-200 bg-white shadow-sm">
      <div className="flex items-center justify-between border-b border-zinc-100 bg-zinc-50/80 px-3 py-2">
        <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-widest text-zinc-500">
          <Sparkles size={13} className="text-zinc-400" />
          输出摘要
        </div>
        <span className="inline-flex items-center rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-medium text-emerald-700">
          <CheckCircle2 size={11} className="mr-1" />
          已生成
        </span>
      </div>
      <div className="space-y-3 p-3">
        {typeof output.summary === "string" ? (
          <div className="rounded-lg border border-zinc-100 bg-zinc-50 px-3 py-2 text-xs leading-relaxed text-zinc-700">
            {output.summary}
          </div>
        ) : null}
        {facts.length ? (
          <div className="flex flex-wrap gap-1.5">
            {facts.map((item) => (
              <span
                key={item}
                className="inline-flex items-center rounded-full border border-zinc-200 bg-white px-2.5 py-1 text-[10px] font-medium text-zinc-600 shadow-sm"
              >
                <ImageIcon size={11} className="mr-1 text-zinc-400" />
                {item}
              </span>
            ))}
          </div>
        ) : (
          <div className="text-xs text-zinc-500">
            已有输出，可在相关节点和底部图片区查看可下载素材。
          </div>
        )}
      </div>
    </div>
  );
}
