import { useEffect, useMemo } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ArrowUpRight, RefreshCw } from "lucide-react";
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
  return <section aria-labelledby="merchant-overview-title" className="mb-8 border-y border-border-l1 py-6">
    <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
      <div><h2 id="merchant-overview-title" className="text-lg font-semibold text-text-primary">{m("title")}</h2><p className="mt-1 text-sm text-text-muted">{m("subtitle")}</p></div>
      <div className="flex items-center gap-2">
        <Select ariaLabel={m("window")} value={String(input.days)} options={[{ value: "7", label: m("days7") }, { value: "30", label: m("days30") }]} onChange={(value) => update({ days: value === "7" ? 7 : 30 })} className="w-40" />
        <Button aria-label={m("refresh")} title={m("refresh")} disabled={query.isFetching} onClick={() => void query.refetch()}><RefreshCw size={16} className={query.isFetching ? "animate-spin motion-reduce:animate-none" : ""} aria-hidden="true" /></Button>
      </div>
    </div>
    {query.isError ? <div role="alert" className="mb-5 flex flex-wrap items-center gap-3 text-sm text-state-error"><span>{m("loadError")}</span><Button onClick={() => void query.refetch()}>{m("retry")}</Button></div> : null}
    {query.isPending ? <p role="status" className="py-5 text-sm text-text-muted">{t("app.loading")}</p> : data ? <>
      <div className="grid gap-6 pb-6 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
        <div>
          <dl className="grid grid-cols-2 gap-5">
            <div><dt className="min-h-8 text-xs text-text-muted">{m("totalProducts")}</dt><dd className="mt-2 text-3xl font-semibold tabular-nums text-text-primary">{data.products.total}</dd></div>
            <div><dt className="min-h-8 text-xs text-text-muted">{m("adoptedProducts")}</dt><dd className="mt-2 text-3xl font-semibold tabular-nums text-text-primary">{data.products.current_adopted}</dd></div>
          </dl>
          <p className="mt-4 text-xs leading-5 text-text-muted">{m("adoptionNote")}</p>
        </div>
        <div className="min-w-0">
          <table className="w-full table-fixed text-left text-xs"><caption className="sr-only">{m("sourceCounts")}</caption><thead><tr className="text-text-muted"><th className="w-[28%] pb-2 pr-2 font-medium">{m("allSources")}</th>{(["active", "waiting", "unknown", "recentFailed"] as const).map((key) => <th key={key} className="px-1 pb-2 text-right align-bottom font-medium leading-4">{m(key)}</th>)}</tr></thead><tbody>{WORK_KINDS.map((kind) => {
            const counts = data.work.by_source[kind];
            return <tr key={kind} className="border-t border-border-l1"><th className="py-2 pr-2 font-medium text-text-secondary">{m(kind)}</th>{([counts.active, counts.waiting, counts.unknown, counts.recent_failed]).map((count, index) => <td key={index} className="px-1 py-2 text-right tabular-nums text-text-primary">{count}</td>)}</tr>;
          })}</tbody></table>
          <p className="mt-2 text-xs leading-5 text-text-muted">{m("sourceNote")}</p>
        </div>
      </div>
      <div className="mb-5 flex flex-wrap justify-between gap-2 border-y border-border-l1 py-3 text-xs leading-5 text-text-muted">
        <p>{m("window")}: <time dateTime={data.recent_window.from}>{formatTime(data.recent_window.from)}</time> – <time dateTime={data.recent_window.to}>{formatTime(data.recent_window.to)}</time></p>
        <p>{m("updated")}: <time dateTime={data.as_of}>{formatTime(data.as_of)}</time></p>
      </div>
    </> : null}
    <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
      <h3 className="font-semibold text-text-primary">{m("workTitle")}</h3>
      <div className="grid w-full grid-cols-2 gap-2 sm:w-auto">
        <Select ariaLabel={m("allSources")} value={input.kind} options={[{ value: "all", label: m("allSources") }, ...WORK_KINDS.map((value) => ({ value, label: m(value) }))]} onChange={(value) => update({ kind: WORK_KINDS.find((kind) => kind === value) ?? "all" })} className="sm:w-44" />
        <Select ariaLabel={m("allStates")} value={input.state} options={WORK_STATES.map((value) => ({ value, label: m(value === "all" ? "allStates" : value === "failed" ? "recentFailed" : value) }))} onChange={(value) => update({ state: WORK_STATES.find((state) => state === value) ?? "all" })} className="sm:w-52" />
      </div>
    </div>
    <p className="mb-1 text-xs leading-5 text-text-muted">{m("currentNote")} {m("failureNote")}</p>
    <p className="mb-4 text-xs leading-5 text-text-muted">{m("filterNote")}</p>
    {query.isPlaceholderData ? <p role="status" className="py-5 text-sm text-text-muted">{t("app.loading")}</p> : null}
    {records ? <>
      {records.items.length ? <ul className="divide-y divide-border-l1 border-y border-border-l1">{records.items.map((record) => <WorkRecord key={`${record.kind}:${record.id}`} record={record} />)}</ul> : <p className="border-y border-border-l1 py-8 text-center text-sm text-text-muted">{m("empty")}</p>}
      <div className="mt-4 flex items-center justify-between gap-3 text-xs text-text-muted"><span>{m("page")} {records.page} / {pages} · {records.total}</span><div className="flex gap-2"><Button disabled={input.page <= 1 || query.isFetching} onClick={() => update({ page: input.page - 1 })}>{m("previous")}</Button><Button disabled={input.page >= pages || query.isFetching} onClick={() => update({ page: input.page + 1 })}>{m("next")}</Button></div></div>
    </> : null}
  </section>;
}

function WorkRecord({ record }: { record: MerchantWorkRecord }) {
  const { locale, t } = useI18n();
  const m = (key: Parameters<typeof overviewMessage>[1]) => overviewMessage(locale, key);
  const link = workRecordLink(record);
  const globalTask = record.kind === "agent_task" && !record.product_id && record.session_id;
  const action = record.kind === "image_session" ? "openSession" : record.kind === "agent_task" && record.session_id ? "openTask" : "openProduct";
  const formatTime = (time: string) => new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(time));
  return <li className="grid gap-3 py-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
    <div className="min-w-0">
      <div className="mb-2 flex flex-wrap items-center gap-2 text-xs"><span className="text-text-muted">{m(record.kind)}</span><span className={`rounded-control border border-border-l1 px-2 py-1 ${record.status === "failed" ? "text-state-error" : record.status === "unknown" ? "text-state-warning" : "text-text-secondary"}`}>{statusKeys[record.status] ? t(statusKeys[record.status]) : record.status}</span></div>
      <p className="break-words text-sm font-semibold text-text-primary">{record.title}</p>
      <p className="mt-1 break-words text-xs text-text-secondary">{record.product_name ?? m("noProduct")}</p>
      <dl className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-text-muted">{(["created", "started", "finished"] as const).map((label) => {
        const time = record[`${label}_at`];
        return time ? <div key={label}><dt className="mr-1 inline">{m(label)}</dt><dd className="inline"><time dateTime={time}>{formatTime(time)}</time></dd></div> : null;
      })}</dl>
      {record.failure_reason ? <p className="mt-2 break-words text-xs text-state-error">{record.failure_reason}</p> : null}
    </div>
    {link ? <Link to={link} className="inline-flex min-h-11 items-center justify-center gap-2 rounded-control border border-border-l1 px-3 text-xs font-semibold text-text-secondary hover:border-accent hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"><span>{m(action)}</span><ArrowUpRight size={14} aria-hidden="true" /></Link> : globalTask ? <Button onClick={() => openGlobalAgent({ tab: "tasks", sessionId: globalTask, taskId: record.id })}>{m("openTask")}<ArrowUpRight size={14} aria-hidden="true" /></Button> : <span className="text-xs text-text-muted">{m("unavailable")}</span>}
  </li>;
}
