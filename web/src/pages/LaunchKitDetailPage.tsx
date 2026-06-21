import type React from "react";
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

function ManualExportCard({ kit }: { kit: LaunchKitDetail }) {
  const snapshot = recordValue(kit.export_snapshot);
  const manualExport = recordValue(snapshot?.manual_export);
  const platformBlocks = Array.isArray(manualExport?.platform_blocks)
    ? manualExport.platform_blocks.map(recordValue).filter((item): item is Record<string, unknown> => Boolean(item))
    : [];
  const checklist = stringList(manualExport?.checklist ?? snapshot?.checklist_items);
  if (!manualExport && platformBlocks.length === 0) {
    return <JsonPreview value={kit.export_snapshot ?? kit.exports} />;
  }
  return (
    <div className="space-y-4">
      {platformBlocks.map((block) => {
        const platform = stringValue(block.platform) ?? "platform";
        return (
          <article key={`${platform}-${stringValue(block.title)}`} className="rounded-2xl border border-slate-200 p-4 dark:border-slate-700">
            <div className="mb-2 text-xs font-bold uppercase tracking-[0.18em] text-emerald-700 dark:text-emerald-300">
              {platformLabel(platform)} copy block
            </div>
            <h3 className="text-base font-semibold text-slate-950 dark:text-white">{stringValue(block.title) ?? "Untitled"}</h3>
            {stringValue(block.hook) ? <p className="mt-2 text-sm font-medium text-slate-700 dark:text-slate-200">{stringValue(block.hook)}</p> : null}
            <pre className="mt-3 whitespace-pre-wrap rounded-xl bg-slate-50 p-3 text-sm leading-6 text-slate-700 dark:bg-slate-900 dark:text-slate-200">{stringValue(block.description) ?? ""}</pre>
            {stringList(block.hashtags).length ? <p className="mt-3 text-sm text-emerald-700 dark:text-emerald-300">{stringList(block.hashtags).join(" ")}</p> : null}
          </article>
        );
      })}
      {checklist.length ? (
        <div className="rounded-2xl bg-amber-50 p-4 text-sm text-amber-800 dark:bg-amber-500/10 dark:text-amber-100">
          <div className="mb-2 font-semibold">Manual export checklist</div>
          <ul className="space-y-1">{checklist.map((item) => <li key={item}>• {item}</li>)}</ul>
        </div>
      ) : null}
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
                    <ClipboardList size={16} className="mr-1.5" /> {generateMutation.isPending ? "Submitting…" : "Generate"}
                  </button>
                  <button type="button" disabled className="inline-flex items-center rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-sm font-semibold text-slate-500 opacity-60 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400">
                    <Download size={16} className="mr-1.5" /> Export
                  </button>
                </div>
              </div>
              {generateMutation.isError ? (
                <div className="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
                  Queue submission failed or another generation is active. Try again after checking backend queue settings.
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
                  {kit.latest_task ? (
                    <div className="space-y-2 text-sm text-slate-600 dark:text-slate-300">
                      <p><span className="font-semibold">Status:</span> {kit.latest_task.status}</p>
                      <p><span className="font-semibold">Stage:</span> {kit.latest_task.progress_stage ?? "—"}</p>
                      {kit.latest_task.failure_detail ? <p className="text-red-600 dark:text-red-300">{kit.latest_task.failure_detail}</p> : null}
                    </div>
                  ) : <p className="text-sm text-slate-500 dark:text-slate-400">No generation task yet.</p>}
                </section>
              </aside>

              <section className="space-y-5">
                <Panel title="Source references" icon={FileText}><JsonPreview value={kit.source_references} /></Panel>
                <Panel title="Selected buyer angle" icon={ClipboardList}><AngleCard value={recordValue(kit.selected_angle)} /></Panel>
                <Panel title="Generated summary" icon={ClipboardList}><JsonPreview value={kit.generated_summary} /></Panel>
                <Panel title="Quality score" icon={CheckCircle2}><QualityScoreCard value={recordValue(kit.quality_score_summary)} /></Panel>
                <Panel title="Manual export" icon={Download}><ManualExportCard kit={kit} /></Panel>
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
