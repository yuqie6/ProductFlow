import type { CSSProperties } from "react";
import { Check, History, Layers3, Loader2, Sparkles } from "lucide-react";

import { api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { PromptPreview } from "../../components/PromptPreviewDialog";
import type { ImageHistoryBranch, ImageHistoryCandidate } from "./branching";
import type { ImageChatTranslate } from "./display";
import { imageRoundSizeLabel, placeholderStatusClass, placeholderStatusLabel } from "./display";

interface HistoryBranchStripProps {
  branch: ImageHistoryBranch;
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
  onPreviewPrompt: (preview: PromptPreview) => void;
  t: ImageChatTranslate;
}

export function HistoryBranchStrip({
  branch,
  selectedGeneratedAssetId,
  selectedTaskPlaceholderId,
  branchBaseAssetId,
  onSelectRound,
  onSelectPlaceholder,
  onPreviewPrompt,
  t,
}: HistoryBranchStripProps) {
  const depthOffset = Math.min(branch.depth, 4) * 18;
  const branchLabel = branch.base_asset_id ? t("chat.branch", { depth: branch.depth }) : t("chat.firstRound");

  return (
    <div
      className="relative flex w-max shrink-0 snap-start flex-col gap-1 rounded-2xl lg:ml-[var(--branch-depth-offset)] lg:h-full lg:flex-row lg:gap-2 lg:border lg:border-slate-200 lg:bg-slate-50/80 lg:p-2 lg:dark:border-slate-700/80 lg:dark:bg-[#151f33]"
      style={{ "--branch-depth-offset": `${depthOffset}px` } as CSSProperties}
    >
      {branch.depth > 0 ? (
        <div className="pointer-events-none absolute -left-3 top-1/2 hidden h-px w-3 bg-slate-300 dark:bg-slate-700 lg:block" />
      ) : null}
      <div className="flex w-max items-center gap-1 px-0.5 text-[11px] text-slate-500 dark:text-slate-400 lg:hidden">
        <div className="flex min-w-0 items-center gap-1 rounded-full border border-slate-200 bg-white/86 px-1.5 py-0.5 shadow-sm dark:border-slate-700 dark:bg-[#0b1220]/88">
          <span className="inline-flex h-5 shrink-0 items-center gap-1 rounded-full border border-slate-200 bg-white px-1.5 font-semibold text-slate-700 shadow-sm dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100">
            {branch.depth > 0 ? <Layers3 size={12} /> : <History size={12} />}
            {branchLabel}
          </span>
          <span className="pr-1">{t("chat.imageCount", { count: branch.candidates.length })}</span>
        </div>
      </div>
      <div className="hidden w-28 shrink-0 flex-col justify-between rounded-xl bg-white p-2 text-xs text-slate-500 ring-1 ring-slate-200 dark:bg-[#0b1220] dark:text-slate-400 dark:ring-slate-600/80 lg:flex">
        <div>
          <div className="flex items-center gap-1.5 font-semibold text-slate-800 dark:text-slate-100">
            {branch.depth > 0 ? <Layers3 size={12} /> : <History size={12} />}
            {branchLabel}
          </div>
          <div className="mt-1">{t("chat.imageCount", { count: branch.candidates.length })}</div>
        </div>
        <button
          type="button"
          onClick={() =>
            onPreviewPrompt({
              title: branch.base_asset_id ? t("chat.branchPrompt") : t("chat.firstPrompt"),
              text: branch.prompt,
              meta: `${t("chat.imageCount", { count: branch.candidates.length })} · ${formatDateTime(branch.created_at)}`,
            })
          }
          className="hidden rounded-md text-left text-[11px] leading-4 text-slate-400 transition-colors hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:text-slate-500 dark:hover:text-violet-200 lg:line-clamp-3"
        >
          {branch.prompt}
        </button>
      </div>
      <div className="flex w-max shrink-0 gap-2 lg:min-h-0 lg:flex-1">
        {branch.candidates.map((candidate) => (
          <HistoryCandidateCard
            key={candidate.id}
            candidate={candidate}
            selectedGeneratedAssetId={selectedGeneratedAssetId}
            selectedTaskPlaceholderId={selectedTaskPlaceholderId}
            branchBaseAssetId={branchBaseAssetId}
            onSelectRound={onSelectRound}
            onSelectPlaceholder={onSelectPlaceholder}
            t={t}
          />
        ))}
      </div>
    </div>
  );
}

interface HistoryCandidateCardProps {
  candidate: ImageHistoryCandidate;
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
  t: ImageChatTranslate;
}

function HistoryCandidateCard({
  candidate,
  selectedGeneratedAssetId,
  selectedTaskPlaceholderId,
  branchBaseAssetId,
  onSelectRound,
  onSelectPlaceholder,
  t,
}: HistoryCandidateCardProps) {
  if (candidate.kind === "placeholder") {
    const active = candidate.id === selectedTaskPlaceholderId;
    const running = candidate.status === "queued" || candidate.status === "running";
    return (
      <div
        className={`group/card relative h-[5.5rem] w-[5.5rem] shrink-0 overflow-hidden rounded-xl border bg-white transition-all dark:bg-[#0b1220] lg:aspect-square lg:h-full lg:w-auto lg:min-w-[7rem] lg:rounded-2xl ${
          active
            ? "border-indigo-400 ring-2 ring-indigo-200 dark:border-violet-400 dark:ring-violet-400/45"
            : "border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-violet-400/45"
        }`}
      >
        <button
          type="button"
          onClick={() => onSelectPlaceholder(candidate.id)}
          className="flex h-full w-full flex-col justify-between p-2 text-left"
        >
          <div className="flex items-center justify-between gap-2">
            <span className={`rounded-full border px-1.5 py-0.5 text-[10px] font-semibold ${placeholderStatusClass(candidate)}`}>
              {candidate.candidate_index}/{candidate.candidate_count}
            </span>
            {active ? <Check size={13} className="shrink-0 text-indigo-600" /> : null}
          </div>
          <div className="flex flex-1 items-center justify-center">
            <div className="relative flex h-10 w-10 items-center justify-center rounded-2xl bg-slate-50 text-indigo-600 ring-1 ring-slate-200 dark:bg-violet-500/12 dark:text-violet-200 dark:ring-violet-400/30 lg:h-12 lg:w-12">
              {running ? <Loader2 size={19} className="animate-spin" /> : <Sparkles size={19} />}
            </div>
          </div>
          <div>
            <div className="truncate text-[11px] font-semibold text-slate-700 dark:text-slate-100">{placeholderStatusLabel(candidate, t)}</div>
            <div className="mt-0.5 hidden text-[10px] leading-3 text-slate-400 dark:text-slate-500 lg:line-clamp-2">{candidate.prompt}</div>
          </div>
        </button>
      </div>
    );
  }

  const round = candidate.round;
  const active = round.generated_asset.id === selectedGeneratedAssetId;
  const asBase = round.generated_asset.id === branchBaseAssetId;
  return (
    <div
      className={`group/card relative h-[5.5rem] w-[5.5rem] shrink-0 overflow-hidden rounded-xl border bg-white transition-all dark:bg-[#0b1220] lg:aspect-square lg:h-full lg:w-auto lg:min-w-[7rem] lg:rounded-2xl ${
        active
          ? "border-indigo-400 ring-2 ring-indigo-200 dark:border-violet-400 dark:ring-violet-400/45"
          : "border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-violet-400/45"
      } ${asBase ? "shadow-md shadow-indigo-200/70 dark:shadow-violet-950/40" : "shadow-sm shadow-slate-200/60 dark:shadow-slate-950/30"}`}
    >
      <button type="button" onClick={() => onSelectRound(round.generated_asset.id)} className="block h-full w-full text-left">
        <img
          src={api.toApiUrl(round.generated_asset.thumbnail_url)}
          alt={round.prompt}
          loading="lazy"
          decoding="async"
          className="h-full w-full object-cover"
        />
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-slate-950/80 via-slate-950/24 to-transparent px-1.5 pb-1 pt-5 text-white lg:p-1.5 lg:pt-8">
          <div className="flex items-center justify-between gap-2 text-[11px] font-medium">
            <span className="min-w-0 truncate">
              {round.candidate_count > 1 ? `${round.candidate_index}/${round.candidate_count}` : imageRoundSizeLabel(round, t)}
            </span>
            {active ? <Check size={13} className="shrink-0" /> : null}
          </div>
        </div>
      </button>
      {asBase ? (
        <div className="absolute left-1.5 top-1.5 max-w-[calc(100%-2.75rem)] truncate rounded-full bg-indigo-600 px-1.5 py-0.5 text-[10px] font-semibold text-white shadow-sm dark:bg-violet-500/85 dark:ring-1 dark:ring-violet-200/30">
          {t("chat.baseImage")}
        </div>
      ) : null}
    </div>
  );
}
