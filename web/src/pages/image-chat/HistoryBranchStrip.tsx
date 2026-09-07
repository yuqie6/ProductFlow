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
  variant?: "desktop" | "mobileDrawer";
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
  variant = "desktop",
  onSelectRound,
  onSelectPlaceholder,
  onPreviewPrompt,
  t,
}: HistoryBranchStripProps) {
  const depthOffset = Math.min(branch.depth, 4) * 18;
  const branchLabel = branch.base_asset_id ? t("chat.branch", { depth: branch.depth }) : t("chat.firstRound");
  const promptPreview = {
    title: branch.base_asset_id ? t("chat.branchPrompt") : t("chat.firstPrompt"),
    text: branch.prompt,
    meta: `${t("chat.imageCount", { count: branch.candidates.length })} · ${formatDateTime(branch.created_at, t.locale)}`,
  };

  if (variant === "mobileDrawer") {
    return (
      <div className="flex flex-col items-center gap-2">
        <div className="inline-flex min-h-7 max-w-[5.75rem] items-center gap-1 rounded-full border border-border-l1 bg-surface-raised px-2 text-[11px] font-semibold text-text-primary shadow-sm dark:border-border-l1 dark:bg-surface-panel dark:text-text-primary">
          {branch.depth > 0 ? <Layers3 size={12} /> : <History size={12} />}
          <span className="truncate">{branchLabel}</span>
        </div>
        <div className="flex flex-col gap-2">
          {branch.candidates.map((candidate) => (
            <HistoryCandidateCard
              key={candidate.id}
              candidate={candidate}
              selectedGeneratedAssetId={selectedGeneratedAssetId}
              selectedTaskPlaceholderId={selectedTaskPlaceholderId}
              branchBaseAssetId={branchBaseAssetId}
              variant="mobileDrawer"
              onSelectRound={onSelectRound}
              onSelectPlaceholder={onSelectPlaceholder}
              t={t}
            />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div
      className="relative flex w-max shrink-0 snap-start flex-col gap-1 rounded-2xl lg:ml-[var(--branch-depth-offset)] lg:h-full lg:flex-row lg:gap-2 lg:border lg:border-border-l1 lg:bg-surface-base/80 lg:p-2 lg:dark:border-border-l1/80 lg:dark:bg-surface-panel"
      style={{ "--branch-depth-offset": `${depthOffset}px` } as CSSProperties}
    >
      {branch.depth > 0 ? (
        <div className="pointer-events-none absolute -left-3 top-1/2 hidden h-px w-3 bg-surface-subtle dark:bg-surface-subtle lg:block" />
      ) : null}
      <div className="flex w-max items-center gap-1 px-0.5 text-[11px] text-text-muted dark:text-text-muted lg:hidden">
        <div className="flex min-w-0 items-center gap-1 rounded-full border border-border-l1 bg-surface-raised/86 px-1.5 py-0.5 shadow-sm dark:border-border-l1 dark:bg-surface-panel/88">
          <span className="inline-flex h-5 shrink-0 items-center gap-1 rounded-full border border-border-l1 bg-surface-raised px-1.5 font-semibold text-text-secondary shadow-sm dark:border-border-l1 dark:bg-surface-panel dark:text-text-primary">
            {branch.depth > 0 ? <Layers3 size={12} /> : <History size={12} />}
            {branchLabel}
          </span>
          <span className="pr-1">{t("chat.imageCount", { count: branch.candidates.length })}</span>
        </div>
      </div>
      <div className="hidden w-28 shrink-0 flex-col justify-between rounded-xl bg-surface-raised p-2 text-xs text-text-muted ring-1 ring-border-l1 dark:bg-surface-panel dark:text-text-muted dark:ring-border-l3/80 lg:flex">
        <div>
          <div className="flex items-center gap-1.5 font-semibold text-text-primary dark:text-text-primary">
            {branch.depth > 0 ? <Layers3 size={12} /> : <History size={12} />}
            {branchLabel}
          </div>
          <div className="mt-1">{t("chat.imageCount", { count: branch.candidates.length })}</div>
        </div>
        <button
          type="button"
          onClick={() => onPreviewPrompt(promptPreview)}
          className="hidden rounded-md text-left text-[11px] leading-4 text-text-muted transition-colors hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent dark:text-text-muted dark:hover:text-accent lg:line-clamp-3"
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
            variant={variant}
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
  variant?: "desktop" | "mobileDrawer";
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
  t: ImageChatTranslate;
}

function HistoryCandidateCard({
  candidate,
  selectedGeneratedAssetId,
  selectedTaskPlaceholderId,
  branchBaseAssetId,
  variant = "desktop",
  onSelectRound,
  onSelectPlaceholder,
  t,
}: HistoryCandidateCardProps) {
  const cardClassName = (active: boolean, asBase = false) =>
    `group/card relative shrink-0 overflow-hidden rounded-xl border bg-surface-raised transition-all dark:bg-surface-panel ${
      variant === "mobileDrawer"
        ? "aspect-square min-h-[5.75rem] w-full"
        : "h-[5.5rem] w-[5.5rem] lg:aspect-square lg:h-full lg:w-auto lg:min-w-[7rem] lg:rounded-2xl"
    } ${
      active
        ? "border-accent ring-2 ring-accent dark:border-accent dark:ring-accent/45"
        : "border-border-l1 hover:border-border-l3 dark:border-border-l1 dark:hover:border-accent/45"
    } ${asBase ? "shadow-md  " : ""}`;

  if (candidate.kind === "placeholder") {
    const active = candidate.id === selectedTaskPlaceholderId;
    const running = candidate.status === "queued" || candidate.status === "running";
    return (
      <div className={cardClassName(active)}>
        <button
          type="button"
          onClick={() => onSelectPlaceholder(candidate.id)}
          className="flex h-full w-full flex-col justify-between p-2 text-left"
        >
          <div className="flex items-center justify-between gap-2">
            <span className={`rounded-full border px-1.5 py-0.5 text-[10px] font-semibold ${placeholderStatusClass(candidate)}`}>
              {candidate.candidate_index}/{candidate.candidate_count}
            </span>
            {active ? <Check size={13} className="shrink-0 text-accent" /> : null}
          </div>
          <div className="flex flex-1 items-center justify-center">
            <div className="relative flex h-10 w-10 items-center justify-center rounded-2xl bg-surface-base text-accent ring-1 ring-border-l1 dark:bg-accent/12 dark:text-accent dark:ring-accent/30 lg:h-12 lg:w-12">
              {running ? <Loader2 size={19} className="animate-spin" /> : <Sparkles size={19} />}
            </div>
          </div>
          <div>
            <div className="truncate text-[11px] font-semibold text-text-secondary dark:text-text-primary">{placeholderStatusLabel(candidate, t)}</div>
            <div className="mt-0.5 hidden text-[10px] leading-3 text-text-muted dark:text-text-muted lg:line-clamp-2">{candidate.prompt}</div>
          </div>
        </button>
      </div>
    );
  }

  const round = candidate.round;
  const active = round.generated_asset.id === selectedGeneratedAssetId;
  const asBase = round.generated_asset.id === branchBaseAssetId;
  const candidateLabel =
    round.candidate_count > 1 ? `${round.candidate_index}/${round.candidate_count}` : imageRoundSizeLabel(round, t);
  return (
    <div className={`${cardClassName(active, asBase)} ${asBase ? "" : "shadow-sm  "}`}>
      <button type="button" onClick={() => onSelectRound(round.generated_asset.id)} className="block h-full w-full text-left">
        <img
          src={api.toApiUrl(round.generated_asset.thumbnail_url)}
          alt={variant === "mobileDrawer" ? candidateLabel : round.prompt}
          loading="lazy"
          decoding="async"
          className="h-full w-full object-cover"
        />
        <div className="absolute inset-x-0 bottom-0 bg-accent from-slate-950/80 via-slate-950/24 to-transparent px-1.5 pb-1 pt-5 text-accent-fg lg:p-1.5 lg:pt-8">
          <div className="flex items-center justify-between gap-2 text-[11px] font-medium">
            <span className="min-w-0 truncate">{candidateLabel}</span>
            {active ? <Check size={13} className="shrink-0" /> : null}
          </div>
        </div>
      </button>
      {asBase ? (
        <div className="absolute left-1.5 top-1.5 max-w-[calc(100%-2.75rem)] truncate rounded-full bg-accent px-1.5 py-0.5 text-[10px] font-semibold text-accent-fg shadow-sm dark:bg-accent/85 dark:ring-1 dark:ring-accent/30">
          {t("chat.baseImage")}
        </div>
      ) : null}
    </div>
  );
}
