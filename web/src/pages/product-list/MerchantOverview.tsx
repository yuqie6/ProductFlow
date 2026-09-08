import { useEffect, useMemo } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ArrowUpRight, RefreshCw, Package, CheckCheck, Bot, Workflow, Images, Pencil, Clock3, Inbox, AlertCircle } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { Button } from "../../components/ui/button";
import { Select } from "../../components/ui/select";
import { getMerchantOverview } from "../../lib/merchantOverviewApi";
import { openGlobalAgent } from "../../lib/globalAgentEvents";
import { useI18n } from "../../lib/preferences";
import type { TranslationKey } from "../../lib/i18n";
import type { MerchantWorkRecord } from "../../lib/types";
import { overviewMessage } from "./overviewMessages";
import { parseOverviewParams, patchOverviewParams, WORK_KINDS, WORK_STATES, workRecordLink } from "./overviewModel";

const sourceIcons = { agent_task: Bot, workflow_run: Workflow, image_session: Images, local_edit: Pencil };
const sourceStateColumns = [
  { label: "active", field: "active", state: "active", tone: "text-accent bg-accent-soft" },
  { label: "waiting", field: "waiting", state: "waiting", tone: "text-state-warning bg-state-warning-soft" },
  { label: "unknown", field: "unknown", state: "unknown", tone: "text-text-secondary bg-surface-subtle" },
  { label: "recentFailed", field: "recent_failed", state: "failed", tone: "text-state-error bg-state-error-soft" },
] as const;

const statusKeys: Record<string, TranslationKey> = {
  queued: "agentWorkbench.status.queued", running: "agentWorkbench.status.running",
  waiting_user: "globalAgent.taskStatus.waitingUser", awaiting_confirmation: "agentWorkbench.toolStep.detail.pending",
  paused: "globalAgent.taskStatus.paused", draft: "status.draft", unknown: "agentWorkbench.status.unknown",
  failed: "agentWorkbench.status.failed", succeeded: "agentWorkbench.status.succeeded",
  canceled: "agentWorkbench.status.canceled", cancelled: "agentWorkbench.status.canceled",
};

export function MerchantOverview({ merchantId }: { merchantId: string }) {
  const { locale, t } = useI18n();
  const m = (key: Parameters<typeof overviewMessage>[1]) => overviewMessage(locale, key);
  const [params, setParams] = useSearchParams();
  const input = useMemo(() => parseOverviewParams(params), [params]);
  const query = useQuery({ queryKey: ["merchant-overview", merchantId, input], queryFn: () => getMerchantOverview(input), enabled: Boolean(merchantId), retry: false, placeholderData: keepPreviousData });
  useEffect(() => {
    const next = patchOverviewParams(params, { days: input.days, kind: input.kind, state: input.state, page: input.page });
    if (next.toString() !== params.toString()) setParams(next, { replace: true });
  }, [input, params, setParams]);
  const update = (patch: Parameters<typeof patchOverviewParams>[1]) => setParams((current) => patchOverviewParams(current, patch));
  const data = query.data;
  const records = query.isPlaceholderData ? undefined : data?.work.records;
  const pages = Math.max(1, Math.ceil((records?.total ?? 0) / input.page_size));
  useEffect(() => {
    if (records && input.page > pages) setParams((current) => patchOverviewParams(current, { page: pages }), { replace: true });
  }, [input.page, pages, records, setParams]);
  const formatTime = (time: string) => new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(time));
  const adoptedShare = data && data.products.total > 0
    ? Math.min(100, data.products.current_adopted / data.products.total * 100)
    : 0;

  return (
    <section aria-labelledby="merchant-overview-title" className="mb-8 space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 id="merchant-overview-title" className="text-xl font-semibold tracking-tight text-text-primary">{m("title")}</h2>
          <p className="mt-1 text-sm text-text-muted">{m("subtitle")}</p>
        </div>
        <div className="flex items-center gap-2">
          <Select ariaLabel={m("window")} value={String(input.days)} options={[{ value: "7", label: m("days7") }, { value: "30", label: m("days30") }]} onChange={(value) => update({ days: value === "7" ? 7 : 30 })} className="w-40" />
          <Button aria-label={m("refresh")} title={m("refresh")} disabled={query.isFetching} onClick={() => void query.refetch()}>
            <RefreshCw size={16} className={query.isFetching ? "animate-spin motion-reduce:animate-none" : ""} aria-hidden="true" />
          </Button>
        </div>
      </header>

      {query.isError ? (
        <div role="alert" className="flex flex-wrap items-center gap-3 rounded-panel border border-state-error/25 bg-state-error-soft px-4 py-3 text-sm text-state-error">
          <AlertCircle size={18} aria-hidden="true" /><span className="min-w-0 flex-1">{m("loadError")}</span>
          <Button onClick={() => void query.refetch()}>{m("retry")}</Button>
        </div>
      ) : null}

      {query.isPending ? <OverviewSkeleton label={t("app.loading")} /> : data ? (
        <div className="overflow-hidden rounded-surface border border-border-l1 bg-surface-raised shadow-elev-1">
          <div className="grid lg:grid-cols-[minmax(260px,.8fr)_minmax(0,1.7fr)]">
            <div className="flex flex-col border-b border-border-l1 p-5 sm:p-6 lg:border-r lg:border-b-0">
              <div className="mb-7 flex items-center gap-2 text-sm font-semibold text-text-secondary">
                <Package size={17} className="text-accent" aria-hidden="true" />{m("productProgress")}
              </div>
              <dl className="grid grid-cols-2 gap-5">
                <div>
                  <dt className="min-h-10 text-xs leading-5 text-text-muted">{m("totalProducts")}</dt>
                  <dd className="mt-1 text-4xl font-semibold tracking-tight tabular-nums text-text-primary">{data.products.total.toLocaleString(locale)}</dd>
                </div>
                <div className="border-l border-border-l1 pl-5">
                  <dt className="min-h-10 text-xs leading-5 text-text-muted">{m("adoptedProducts")}</dt>
                  <dd className="mt-1 text-4xl font-semibold tracking-tight tabular-nums text-accent">{data.products.current_adopted.toLocaleString(locale)}</dd>
                </div>
              </dl>
              <div className="mt-6 h-1.5 overflow-hidden rounded-full bg-surface-subtle" aria-hidden="true">
                <div className="h-full rounded-full bg-accent" style={{ width: `${adoptedShare}%` }} />
              </div>
            </div>
            <div className="min-w-0 p-4 sm:p-6">
              <h3 className="mb-4 flex items-center gap-2 text-sm font-semibold text-text-secondary"><Workflow size={17} aria-hidden="true" />{m("sourceCounts")}</h3>
              <table className="w-full table-fixed text-left text-xs">
                <caption className="sr-only">{m("sourceCounts")}</caption>
                <thead><tr className="text-text-muted">
                  <th scope="col" className="w-[28%] pb-3 pr-2 font-medium">{m("allSources")}</th>
                  {sourceStateColumns.map(column => <th key={column.state} scope="col" className="px-1 pb-3 text-center align-bottom font-medium leading-4">{m(column.label)}</th>)}
                </tr></thead>
                <tbody>{WORK_KINDS.map(kind => {
                  const Icon = sourceIcons[kind];
                  return (
                    <tr key={kind} className="border-t border-border-l1">
                      <th scope="row" className="py-2 pr-2 font-medium text-text-secondary">
                        <span className="flex items-center gap-2"><Icon size={15} className="hidden shrink-0 text-text-muted sm:block" aria-hidden="true" /><span className="break-words">{m(kind)}</span></span>
                      </th>
                      {sourceStateColumns.map(column => {
                        const count = data.work.by_source[kind][column.field];
                        return <td key={column.state} className="px-0.5 py-1.5 text-center">
                          <button type="button" aria-label={`${m(kind)} · ${m(column.label)} · ${count}`} onClick={() => update({kind, state: column.state})} className={`min-h-11 w-full rounded-control px-1 font-semibold tabular-nums outline-none transition-colors hover:ring-1 hover:ring-accent focus-visible:ring-2 focus-visible:ring-focus-ring ${count > 0 ? column.tone : "text-text-muted"}`}>
                            {count.toLocaleString(locale)}
                          </button>
                        </td>;
                      })}
                    </tr>
                  );
                })}</tbody>
              </table>
            </div>
          </div>
          <footer className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-border-l1 bg-surface-base/60 px-5 py-3 text-[11px] leading-5 text-text-muted sm:px-6">
            <span className="flex min-w-0 items-start gap-2"><Clock3 size={13} className="mt-1 shrink-0" aria-hidden="true" /><span>{m("window")}: <time dateTime={data.recent_window.from}>{formatTime(data.recent_window.from)}</time> – <time dateTime={data.recent_window.to}>{formatTime(data.recent_window.to)}</time></span></span>
            <span>{m("updated")}: <time dateTime={data.as_of}>{formatTime(data.as_of)}</time></span>
          </footer>
        </div>
      ) : null}

      <div className="overflow-hidden rounded-surface border border-border-l1 bg-surface-raised shadow-elev-1">
        <div className="border-b border-border-l1 p-4 sm:p-5">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex items-center gap-2.5"><span className="flex h-8 w-8 items-center justify-center rounded-control bg-accent-soft text-accent"><CheckCheck size={17} aria-hidden="true" /></span><h3 className="font-semibold text-text-primary">{m("workTitle")}</h3>{records ? <span className="rounded-full bg-surface-subtle px-2.5 py-1 text-xs tabular-nums text-text-muted">{records.total.toLocaleString(locale)}</span> : null}</div>
            <div className="grid w-full grid-cols-2 gap-2 sm:w-auto">
              <Select ariaLabel={m("allSources")} value={input.kind} options={[{ value: "all", label: m("allSources") }, ...WORK_KINDS.map(value => ({ value, label: m(value) }))]} onChange={value => update({ kind: WORK_KINDS.find(kind => kind === value) ?? "all" })} className="sm:w-44" />
              <Select ariaLabel={m("allStates")} value={input.state} options={WORK_STATES.map(value => ({ value, label: m(value === "all" ? "allStates" : value === "failed" ? "recentFailed" : value) }))} onChange={value => update({ state: WORK_STATES.find(state => state === value) ?? "all" })} className="sm:w-52" />
            </div>
          </div>
        </div>
        {query.isPlaceholderData ? <div role="status" className="px-5 py-10 text-center text-sm text-text-muted">{t("app.loading")}</div> : null}
        {records ? <>
          {records.items.length ? (
            <ul className="divide-y divide-border-l1">{records.items.map(record => <WorkRecord key={`${record.kind}:${record.id}`} record={record} />)}</ul>
          ) : (
            <div className="flex min-h-52 flex-col items-center justify-center gap-4 px-6 py-10 text-center">
              <span className="flex h-12 w-12 items-center justify-center rounded-panel border border-border-l1 bg-surface-base text-text-muted"><Inbox size={24} strokeWidth={1.5} aria-hidden="true" /></span>
              <p className="max-w-sm text-sm leading-6 text-text-muted">{m("empty")}</p>
            </div>
          )}
          <footer className="flex flex-wrap items-center justify-between gap-3 border-t border-border-l1 bg-surface-base/60 px-5 py-3 text-xs text-text-muted">
            <span className="tabular-nums">{m("page")} {records.page} / {pages} · {records.total}</span>
            <div className="flex gap-2"><Button disabled={input.page <= 1 || query.isFetching} onClick={() => update({ page: input.page - 1 })}>{m("previous")}</Button><Button disabled={input.page >= pages || query.isFetching} onClick={() => update({ page: input.page + 1 })}>{m("next")}</Button></div>
          </footer>
        </> : null}
      </div>
    </section>
  );
}

function WorkRecord({ record }: { record: MerchantWorkRecord }) {
  const { locale, t } = useI18n();
  const m = (key: Parameters<typeof overviewMessage>[1]) => overviewMessage(locale, key);
  const link = workRecordLink(record);
  const globalTask = record.kind === "agent_task" && !record.product_id && record.session_id;
  const action = record.kind === "image_session" ? "openSession" : record.kind === "agent_task" && record.session_id ? "openTask" : "openProduct";
  const formatTime = (time: string) => new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(time));
  const Icon = sourceIcons[record.kind];
  const active = record.status === "running" || record.status === "queued";
  const tone = record.status === "failed" ? "border-state-error/20 bg-state-error-soft text-state-error"
    : record.status === "waiting_user" || record.status === "awaiting_confirmation" || record.status === "unknown" ? "border-state-warning/20 bg-state-warning-soft text-state-warning"
    : active ? "border-accent/20 bg-accent-soft text-accent"
    : record.status === "succeeded" ? "border-state-success/20 bg-state-success-soft text-state-success"
    : "border-border-l1 bg-surface-base text-text-muted";
  return (
    <li className="group grid gap-4 px-4 py-5 transition-colors hover:bg-surface-base/70 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:px-5">
      <div className="flex min-w-0 gap-3 sm:gap-4">
        <span className="mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-panel border border-border-l1 bg-surface-base text-text-secondary"><Icon size={18} aria-hidden="true" /></span>
        <div className="min-w-0 flex-1">
          <div className="mb-2 flex flex-wrap items-center gap-2 text-[11px]">
            <span className="text-text-muted">{m(record.kind)}</span>
            <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 font-medium ${tone}`}><span className={`h-1.5 w-1.5 rounded-full bg-current ${active ? "motion-safe:animate-pulse" : ""}`} aria-hidden="true" />{statusKeys[record.status] ? t(statusKeys[record.status]) : record.status}</span>
          </div>
          <p className="break-words text-sm font-semibold leading-6 text-text-primary">{record.title}</p>
          <p className="mt-0.5 break-words text-xs leading-5 text-text-secondary">{record.product_name ?? m("noProduct")}</p>
          <dl className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] leading-5 text-text-muted">{(["created", "started", "finished"] as const).map(label => {
            const time = record[`${label}_at`];
            return time ? <div key={label}><dt className="mr-1 inline">{m(label)}</dt><dd className="inline"><time dateTime={time}>{formatTime(time)}</time></dd></div> : null;
          })}</dl>
          {record.failure_reason ? <p className="mt-2 break-words border-l-2 border-state-error/40 pl-2.5 text-xs leading-5 text-state-error">{record.failure_reason}</p> : null}
        </div>
      </div>
      <div className="flex justify-end pl-13 sm:pl-0">
        {link ? <Link to={link} className="inline-flex min-h-11 items-center justify-center gap-2 rounded-control border border-border-l1 bg-surface-raised px-3 text-xs font-semibold text-text-secondary transition-colors hover:border-accent hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"><span>{m(action)}</span><ArrowUpRight size={14} aria-hidden="true" /></Link> : globalTask ? <Button onClick={() => openGlobalAgent({ tab: "tasks", sessionId: globalTask, taskId: record.id })}>{m("openTask")}<ArrowUpRight size={14} aria-hidden="true" /></Button> : <span className="text-xs text-text-muted">{m("unavailable")}</span>}
      </div>
    </li>
  );
}

function OverviewSkeleton({label}: {label: string}) {
  return <div role="status" aria-label={label} className="grid gap-6 rounded-surface border border-border-l1 bg-surface-raised p-6 lg:grid-cols-[1fr_2fr]">
    <span className="sr-only">{label}</span>
    {[0,1].map(index => <div key={index} aria-hidden="true" className="space-y-5 motion-safe:animate-pulse"><div className="h-4 w-1/3 rounded bg-surface-subtle" /><div className="h-16 rounded-panel bg-surface-subtle" /><div className="h-4 w-2/3 rounded bg-surface-subtle" /></div>)}
  </div>;
}
