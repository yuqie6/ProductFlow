import type React from "react";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft, CheckCircle2, ClipboardList, Download, FileText, Loader2, Store } from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api } from "../lib/api";
import { formatDateTime } from "../lib/format";
import type { LaunchKitDetail, LaunchKitStatus } from "../lib/types";

const statusTone: Record<LaunchKitStatus, string> = {
  draft: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-200 dark:ring-amber-400/30",
  generating: "bg-blue-50 text-blue-700 ring-blue-200 dark:bg-blue-500/10 dark:text-blue-200 dark:ring-blue-400/30",
  ready: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-200 dark:ring-emerald-400/30",
  archived: "bg-slate-100 text-slate-700 ring-slate-200 dark:bg-slate-800 dark:text-slate-200 dark:ring-slate-700",
  failed: "bg-red-50 text-red-700 ring-red-200 dark:bg-red-500/10 dark:text-red-200 dark:ring-red-400/30",
};

const progressStageLabels: Record<string, string> = {
  extracting_facts: "Extracting product facts",
  applying_playbook: "Applying category playbook",
  applying_store_profile: "Applying store profile",
  generating_angles: "Selecting buyer angle",
  generating_copy: "Writing platform copy",
  planning_images: "Planning image proof",
  scoring: "Scoring readiness",
  exporting_optional_snapshot: "Preparing export snapshot",
};

const progressStageOrder = Object.keys(progressStageLabels);

function generationButtonLabel(status: LaunchKitStatus, pending: boolean) {
  if (pending) {
    return "Submitting…";
  }
  if (status === "failed") {
    return "Retry generation";
  }
  if (status === "ready") {
    return "Regenerate";
  }
  return "Generate";
}

function platformLabel(platform: string) {
  return platform === "tiktok_shop" ? "TikTok Shop" : "Shopee";
}

function JsonPreview({ value }: { value: unknown }) {
  if (!value || (typeof value === "object" && Object.keys(value as Record<string, unknown>).length === 0)) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">No data yet.</p>;
  }
  return (
    <pre className="max-h-80 overflow-auto rounded-xl bg-slate-950 p-4 text-xs leading-5 text-slate-100">
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}


function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value : null;
}

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function booleanOrNull(value: unknown): boolean | null {
  return typeof value === "boolean" ? value : null;
}

async function copyToClipboard(text: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.append(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

function downloadTextFile(filename: string, content: string, mimeType: string) {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

function slugifyFilename(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9_-]+/gi, "-").replace(/^-+|-+$/g, "") || "launch-kit";
}


function QualityScoreCard({ value }: { value: Record<string, unknown> | null }) {
  if (!value) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">Generate the kit to calculate readiness.</p>;
  }
  const overall = typeof value.overall === "number" ? value.overall : 0;
  const warnings = stringList(value.warnings);
  return (
    <div>
      <div className="flex items-end gap-3">
        <div className="text-4xl font-semibold text-slate-950 dark:text-white">{overall}</div>
        <div className="pb-1 text-sm font-semibold text-slate-500 dark:text-slate-400">/ 100 readiness</div>
      </div>
      <div className="mt-3 h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
        <div className="h-full rounded-full bg-emerald-600" style={{ width: `${Math.min(100, Math.max(0, overall))}%` }} />
      </div>
      {warnings.length ? (
        <ul className="mt-4 space-y-2 text-sm text-amber-700 dark:text-amber-200">
          {warnings.map((warning) => <li key={warning}>• {warning}</li>)}
        </ul>
      ) : <p className="mt-4 text-sm text-emerald-700 dark:text-emerald-200">No blocking readiness warnings.</p>}
    </div>
  );
}

function AngleCard({ value }: { value: Record<string, unknown> | null }) {
  if (!value) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">No angle selected yet.</p>;
  }
  return (
    <div className="space-y-3 text-sm leading-6 text-slate-600 dark:text-slate-300">
      <h3 className="text-lg font-semibold text-slate-950 dark:text-white">{stringValue(value.label) ?? "Selected angle"}</h3>
      <p>{stringValue(value.why_it_might_work) ?? "—"}</p>
      <p><span className="font-semibold text-slate-800 dark:text-slate-100">Buyer emotion:</span> {stringValue(value.buyer_emotion) ?? "—"}</p>
      <p><span className="font-semibold text-slate-800 dark:text-slate-100">Risk:</span> {stringValue(value.risk) ?? "—"}</p>
    </div>
  );
}

function FeedbackPanel({ kit }: { kit: LaunchKitDetail }) {
  const queryClient = useQueryClient();
  const [used, setUsed] = useState(Boolean(recordValue(kit.seller_feedback)?.used));
  const [edited, setEdited] = useState(Boolean(recordValue(kit.seller_feedback)?.edited));
  const [wouldReuse, setWouldReuse] = useState(Boolean(recordValue(kit.seller_feedback)?.would_reuse));
  const [wouldPay, setWouldPay] = useState(Boolean(recordValue(kit.seller_feedback)?.would_pay));
  const [notes, setNotes] = useState(stringValue(recordValue(kit.seller_feedback)?.notes) ?? "");
  const [saved, setSaved] = useState(false);
  const mutation = useMutation({
    mutationFn: () => api.saveLaunchKitFeedback(kit.id, {
      used,
      edited,
      would_reuse: wouldReuse,
      would_pay: wouldPay,
      notes: notes.trim() || null,
      metrics: {},
    }),
    onSuccess: async (updated) => {
      setSaved(true);
      queryClient.setQueryData(["launch-kit", kit.id], updated);
      await queryClient.invalidateQueries({ queryKey: ["launch-kits"] });
    },
  });

  return (
    <div className="space-y-4">
      <div className="grid gap-2 sm:grid-cols-2">
        <TogglePill label="Used in listing" checked={used} onChange={setUsed} />
        <TogglePill label="Edited before use" checked={edited} onChange={setEdited} />
        <TogglePill label="Would reuse" checked={wouldReuse} onChange={setWouldReuse} />
        <TogglePill label="Would pay" checked={wouldPay} onChange={setWouldPay} />
      </div>
      <label className="block">
        <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">Notes</span>
        <textarea
          value={notes}
          onChange={(event) => { setNotes(event.target.value); setSaved(false); }}
          rows={3}
          placeholder="What did you edit? Did this help you publish faster?"
          className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-3 text-sm text-slate-950 outline-none transition focus:border-emerald-400 focus:ring-2 focus:ring-emerald-100 dark:border-slate-700 dark:bg-slate-950 dark:text-white dark:focus:ring-emerald-400/20"
        />
      </label>
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-slate-500 dark:text-slate-400">Lightweight feedback helps tune playbooks without marketplace APIs.</p>
        <button
          type="button"
          onClick={() => mutation.mutate()}
          disabled={mutation.isPending}
          className="rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-emerald-600/20 transition hover:bg-emerald-500 disabled:opacity-50"
        >
          {mutation.isPending ? "Saving…" : "Save feedback"}
        </button>
      </div>
      {saved ? <p className="text-sm font-semibold text-emerald-700 dark:text-emerald-300">Feedback saved.</p> : null}
      {mutation.isError ? <p className="text-sm font-semibold text-red-600 dark:text-red-300">Could not save feedback.</p> : null}
    </div>
  );
}

function TogglePill({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className={`rounded-xl border px-3 py-2.5 text-left text-sm font-semibold transition ${checked
        ? "border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-400/45 dark:bg-emerald-500/10 dark:text-emerald-100"
        : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300"}`}
      aria-pressed={checked}
    >
      {checked ? "✓ " : ""}{label}
    </button>
  );
}

function ManualExportCard({ kit }: { kit: LaunchKitDetail }) {
  const queryClient = useQueryClient();
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [copyError, setCopyError] = useState("");
  const snapshot = recordValue(kit.export_snapshot);
  const manualExport = recordValue(snapshot?.manual_export);
  const platformBlocks = Array.isArray(manualExport?.platform_blocks)
    ? manualExport.platform_blocks.map(recordValue).filter((item): item is Record<string, unknown> => Boolean(item))
    : [];
  const checklist = stringList(manualExport?.checklist ?? (snapshot?.checklist_items));
  const feedback = recordValue(kit.seller_feedback);
  const feedbackMetrics = recordValue(feedback?.metrics);
  const metricMutation = useMutation({
    mutationFn: (metricKey: string) => api.saveLaunchKitFeedback(kit.id, {
      used: booleanOrNull(feedback?.used),
      edited: booleanOrNull(feedback?.edited),
      would_reuse: booleanOrNull(feedback?.would_reuse),
      would_pay: booleanOrNull(feedback?.would_pay),
      notes: stringValue(feedback?.notes),
      metrics: {
        ...feedbackMetrics,
        [metricKey]: Number(feedbackMetrics?.[metricKey] ?? 0) + 1,
        last_copy_action_at: new Date().toISOString(),
      },
    }),
    onSuccess: (updated) => {
      queryClient.setQueryData(["launch-kit", kit.id], updated);
    },
  });

  const handleCopy = async (key: string, text: string, metricKey: string) => {
    if (!text.trim()) {
      return;
    }
    try {
      await copyToClipboard(text);
      setCopiedKey(key);
      setCopyError("");
      metricMutation.mutate(metricKey);
      window.setTimeout(() => setCopiedKey((current) => (current === key ? null : current)), 1800);
    } catch {
      setCopyError("Copy failed. Select the text manually and copy it.");
    }
  };

  if (!manualExport && platformBlocks.length === 0) {
    return <JsonPreview value={kit.export_snapshot ?? kit.exports} />;
  }
  return (
    <div className="space-y-4" aria-live="polite">
      {platformBlocks.map((block) => {
        const platform = stringValue(block.platform) ?? "platform";
        const title = stringValue(block.title) ?? "";
        const hook = stringValue(block.hook) ?? "";
        const description = stringValue(block.description) ?? "";
        const hashtags = stringList(block.hashtags);
        const allText = [title, hook, description, hashtags.join(" ")].filter(Boolean).join("\n\n");
        const blockKey = `${platform}-${title}`;
        return (
          <article key={blockKey} className="rounded-2xl border border-slate-200 p-4 dark:border-slate-700">
            <div className="mb-3 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <div className="mb-2 text-xs font-bold uppercase tracking-[0.18em] text-emerald-700 dark:text-emerald-300">
                  {platformLabel(platform)} copy block
                </div>
                <h3 className="text-base font-semibold text-slate-950 dark:text-white">{title || "Untitled"}</h3>
              </div>
              <div className="flex flex-wrap gap-2">
                <CopyButton label="Copy title" copied={copiedKey === `${blockKey}-title`} onClick={() => handleCopy(`${blockKey}-title`, title, `copied_${platform}_title`)} disabled={!title} />
                <CopyButton label="Copy description" copied={copiedKey === `${blockKey}-description`} onClick={() => handleCopy(`${blockKey}-description`, description, `copied_${platform}_description`)} disabled={!description} />
                <CopyButton label="Copy all" copied={copiedKey === `${blockKey}-all`} onClick={() => handleCopy(`${blockKey}-all`, allText, `copied_${platform}_all`)} disabled={!allText} />
              </div>
            </div>
            {hook ? <p className="mt-2 text-sm font-medium text-slate-700 dark:text-slate-200">{hook}</p> : null}
            <pre className="mt-3 whitespace-pre-wrap rounded-xl bg-slate-50 p-3 text-sm leading-6 text-slate-700 dark:bg-slate-900 dark:text-slate-200">{description}</pre>
            {hashtags.length ? (
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <p className="text-sm text-emerald-700 dark:text-emerald-300">{hashtags.join(" ")}</p>
                <CopyButton label="Copy hashtags" copied={copiedKey === `${blockKey}-hashtags`} onClick={() => handleCopy(`${blockKey}-hashtags`, hashtags.join(" "), `copied_${platform}_hashtags`)} />
              </div>
            ) : null}
          </article>
        );
      })}
      {copyError ? <p className="text-sm font-semibold text-red-600 dark:text-red-300">{copyError}</p> : null}
      {metricMutation.isError ? <p className="text-sm text-amber-700 dark:text-amber-200">Copied, but usage metric was not saved.</p> : null}
      {checklist.length ? (
        <div className="rounded-2xl bg-amber-50 p-4 text-sm text-amber-800 dark:bg-amber-500/10 dark:text-amber-100">
          <div className="mb-2 font-semibold">Manual export checklist</div>
          <ul className="space-y-1">{checklist.map((item) => <li key={item}>• {item}</li>)}</ul>
        </div>
      ) : null}
    </div>
  );
}

function CopyButton({ label, copied, disabled, onClick }: { label: string; copied: boolean; disabled?: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-semibold text-slate-600 transition hover:border-emerald-200 hover:bg-emerald-50 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-45 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300 dark:hover:bg-emerald-500/10 dark:hover:text-emerald-200"
    >
      {copied ? "Copied" : label}
    </button>
  );
}

function GenerationProgress({ kit }: { kit: LaunchKitDetail }) {
  const task = kit.latest_task;
  if (!task) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">No generation task yet.</p>;
  }
  const stage = task.progress_stage ?? "";
  const index = progressStageOrder.indexOf(stage);
  const percent = task.status === "succeeded" ? 100 : index >= 0 ? Math.round(((index + 1) / progressStageOrder.length) * 100) : 8;
  return (
    <div className="space-y-3 text-sm text-slate-600 dark:text-slate-300">
      <p><span className="font-semibold">Status:</span> {task.status}</p>
      <p><span className="font-semibold">Stage:</span> {progressStageLabels[stage] ?? (stage || "—")}</p>
      <div className="h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800" aria-label="Generation progress">
        <div className="h-full rounded-full bg-blue-600 transition-all" style={{ width: `${percent}%` }} />
      </div>
      {task.failure_detail ? <p className="text-red-600 dark:text-red-300">{task.failure_detail}</p> : null}
    </div>
  );
}

function ReadinessRail({ kit }: { kit: LaunchKitDetail }) {
  const steps = [
    { label: "Brief captured", done: true },
    { label: "Angle selected", done: Boolean(kit.selected_angle) },
    { label: "Copy variants", done: kit.variants.length > 0 },
    { label: "Quality scored", done: Boolean(kit.quality_score_summary) },
    { label: "Export snapshot", done: Boolean(kit.export_snapshot) || kit.exports.length > 0 },
  ];
  return (
    <div className="space-y-3">
      {steps.map((step, index) => (
        <div key={step.label} className="flex items-center gap-3">
          <span className={`inline-flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold ${step.done ? "bg-emerald-600 text-white" : "bg-slate-100 text-slate-400 dark:bg-slate-800"}`}>
            {step.done ? <CheckCircle2 size={15} /> : index + 1}
          </span>
          <span className={step.done ? "text-sm font-semibold text-slate-800 dark:text-slate-100" : "text-sm text-slate-500 dark:text-slate-400"}>{step.label}</span>
        </div>
      ))}
    </div>
  );
}

export function LaunchKitDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const kitQuery = useQuery({
    queryKey: ["launch-kit", id],
    queryFn: () => api.getLaunchKit(id ?? ""),
    enabled: Boolean(id),
    refetchInterval: (query) => (query.state.data?.status === "generating" ? 3000 : false),
  });

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const generateMutation = useMutation({
    mutationFn: () => api.generateLaunchKit(id ?? ""),
    onSuccess: async (updated) => {
      queryClient.setQueryData(["launch-kit", id], updated);
      await queryClient.invalidateQueries({ queryKey: ["launch-kits"] });
    },
  });

  const exportMutation = useMutation({
    mutationFn: () => api.exportLaunchKitMarkdown(id ?? ""),
    onSuccess: async (markdown) => {
      const productName = kitQuery.data?.product_name ?? "launch-kit";
      downloadTextFile(`${slugifyFilename(productName)}-${id ?? "export"}.md`, markdown, "text/markdown;charset=utf-8");
      const feedback = recordValue(kitQuery.data?.seller_feedback);
      const metrics = recordValue(feedback?.metrics);
      const updated = await api.saveLaunchKitFeedback(id ?? "", {
        used: booleanOrNull(feedback?.used),
        edited: booleanOrNull(feedback?.edited),
        would_reuse: booleanOrNull(feedback?.would_reuse),
        would_pay: booleanOrNull(feedback?.would_pay),
        notes: stringValue(feedback?.notes),
        metrics: {
          ...metrics,
          downloaded_markdown: Number(metrics?.downloaded_markdown ?? 0) + 1,
          last_export_action_at: new Date().toISOString(),
        },
      });
      queryClient.setQueryData(["launch-kit", id], updated);
    },
  });

  const kit = kitQuery.data;

  return (
    <div className="flex min-h-screen flex-col bg-slate-50 dark:bg-[#060a12]">
      <TopNav breadcrumbs={kit?.product_name ?? "LaunchKit"} onHome={() => navigate("/launch-kits")} onLogout={() => logoutMutation.mutate()} />
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 pt-4 pb-40 sm:px-6 lg:py-10">
        <button type="button" onClick={() => navigate("/launch-kits")} className="mb-4 inline-flex items-center text-sm font-semibold text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white">
          <ArrowLeft size={16} className="mr-1" /> Back to LaunchKits
        </button>

        {kitQuery.isError ? (
          <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
            <AlertCircle size={16} className="mr-2 inline" /> Could not load this LaunchKit.
          </div>
        ) : kitQuery.isLoading || !kit ? (
          <div className="flex min-h-80 items-center justify-center rounded-3xl border border-slate-200 bg-white dark:border-slate-700 dark:bg-[#0f1726]">
            <Loader2 className="animate-spin text-slate-400" />
          </div>
        ) : (
          <div className="space-y-5">
            <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/25 lg:p-7">
              <div className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <div className="mb-3 flex flex-wrap items-center gap-2">
                    <span className={`rounded-full px-3 py-1 text-xs font-bold uppercase tracking-[0.12em] ring-1 ${statusTone[kit.status]}`}>{kit.status}</span>
                    <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">{kit.category_key}</span>
                  </div>
                  <h1 className="text-2xl font-semibold tracking-tight text-slate-950 dark:text-white lg:text-3xl">{kit.product_name}</h1>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {kit.target_platforms.map((platform) => (
                      <span key={platform} className="inline-flex items-center rounded-full bg-emerald-50 px-3 py-1 text-sm font-semibold text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200">
                        <Store size={14} className="mr-1.5" /> {platformLabel(platform)}
                      </span>
                    ))}
                  </div>
                  <p className="mt-4 text-sm text-slate-500 dark:text-slate-400">Created {formatDateTime(kit.created_at)} · Updated {formatDateTime(kit.updated_at)}</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <button
                    type="button"
                    disabled={kit.status === "generating" || generateMutation.isPending}
                    onClick={() => generateMutation.mutate()}
                    className="inline-flex items-center rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm shadow-emerald-600/20 transition hover:bg-emerald-500 disabled:opacity-50"
                  >
                    <ClipboardList size={16} className="mr-1.5" /> {generationButtonLabel(kit.status, generateMutation.isPending)}
                  </button>
                  <button
                    type="button"
                    disabled={!kit.export_snapshot || exportMutation.isPending}
                    onClick={() => exportMutation.mutate()}
                    className="inline-flex items-center rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-sm font-semibold text-slate-700 transition hover:border-emerald-200 hover:bg-emerald-50 disabled:text-slate-400 disabled:opacity-60 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-emerald-500/10"
                  >
                    <Download size={16} className="mr-1.5" /> {exportMutation.isPending ? "Exporting…" : "Export MD"}
                  </button>
                </div>
              </div>
              {generateMutation.isError ? (
                <div className="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
                  Queue submission failed or another generation is active. Try again after checking backend queue settings.
                </div>
              ) : null}
              {exportMutation.isError ? (
                <div className="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
                  Export failed. Generate the LaunchKit first, then try downloading again.
                </div>
              ) : null}
            </section>

            <div className="grid gap-5 lg:grid-cols-[320px_1fr]">
              <aside className="space-y-5">
                <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/25">
                  <h2 className="mb-4 text-base font-semibold text-slate-950 dark:text-white">Readiness</h2>
                  <ReadinessRail kit={kit} />
                </section>
                <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/25">
                  <h2 className="mb-3 text-base font-semibold text-slate-950 dark:text-white">Latest task</h2>
<GenerationProgress kit={kit} />
                </section>
              </aside>

              <section className="space-y-5">
                <Panel title="Source references" icon={FileText}><JsonPreview value={kit.source_references} /></Panel>
                <Panel title="Selected buyer angle" icon={ClipboardList}><AngleCard value={recordValue(kit.selected_angle)} /></Panel>
                <Panel title="Generated summary" icon={ClipboardList}><JsonPreview value={kit.generated_summary} /></Panel>
                <Panel title="Quality score" icon={CheckCircle2}><QualityScoreCard value={recordValue(kit.quality_score_summary)} /></Panel>
                <Panel title="Manual export" icon={Download}><ManualExportCard kit={kit} /></Panel>
                <Panel title="Feedback" icon={CheckCircle2}><FeedbackPanel kit={kit} /></Panel>
              </section>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function Panel({ title, icon: Icon, children }: { title: string; icon: typeof FileText; children: React.ReactNode }) {
  return (
    <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/60 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-black/25">
      <h2 className="mb-4 flex items-center text-base font-semibold text-slate-950 dark:text-white"><Icon size={17} className="mr-2 text-emerald-600 dark:text-emerald-300" /> {title}</h2>
      {children}
    </section>
  );
}
