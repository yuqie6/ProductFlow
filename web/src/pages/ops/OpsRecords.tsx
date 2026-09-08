import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../lib/api";
import type { TranslationKey } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";
import type { OpsTask } from "../../lib/types";
import { OPS_PAGE_SIZE, OpsEmpty, OpsError, OpsLoading, OpsPagination, OpsStatus, OpsTime, productPath } from "./OpsShared";

const taskKindKeys: Record<OpsTask["kind"], TranslationKey> = {
  agent_task: "ops.agentTask", workflow_run: "ops.workflowRun", image_session: "ops.imageSession", local_edit: "ops.localEdit",
};

export function OpsTasks({ merchantId, productId }: { merchantId: string; productId?: string }) {
  const { t } = useI18n();
  const [page, setPage] = useState(1);
  const tasks = useQuery({ queryKey: ["ops-tasks", merchantId, productId, page], queryFn: () => api.listOpsTasks(merchantId, { page, page_size: OPS_PAGE_SIZE, product_id: productId }), retry: false });
  if (tasks.isPending) return <OpsLoading />;
  if (tasks.isError) return <OpsError error={tasks.error} retry={() => void tasks.refetch()} />;
  return <>
    {tasks.data.items.length === 0 ? <OpsEmpty /> : <ul className="divide-y divide-border-l1">{tasks.data.items.map((task) => <li key={`${task.kind}:${task.id}`} className="space-y-3 py-5">
      <div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><p className="break-words text-sm font-semibold">{task.title || t(taskKindKeys[task.kind])}</p><p className="mt-1 text-xs text-text-muted">{t(taskKindKeys[task.kind])}</p></div><OpsStatus status={task.status} /></div>
      <dl className="grid gap-2 text-xs text-text-muted sm:grid-cols-3"><div><dt>{t("ops.time")}</dt><dd className="mt-1"><OpsTime value={task.created_at} /></dd></div><div><dt>{t("ops.started")}</dt><dd className="mt-1"><OpsTime value={task.started_at} /></dd></div><div><dt>{t("ops.finished")}</dt><dd className="mt-1"><OpsTime value={task.finished_at} /></dd></div></dl>
      {task.failure_reason ? <p className="break-words text-sm text-state-error">{task.failure_reason}</p> : null}
      {!productId ? task.product_id ? <Link className="inline-flex min-h-11 items-center rounded-control text-xs text-accent hover:underline focus-visible:ring-2 focus-visible:ring-focus-ring" to={productPath(merchantId, task.product_id)}>{t("ops.viewProduct")}</Link> : <p className="text-xs text-text-muted">{t("ops.noProduct")}</p> : null}
    </li>)}</ul>}
    <OpsPagination page={page} total={tasks.data.total} onPage={setPage} />
  </>;
}

export function OpsActions({ merchantId, productId }: { merchantId: string; productId?: string }) {
  const { t } = useI18n();
  const [page, setPage] = useState(1);
  const actions = useQuery({ queryKey: ["ops-actions", merchantId, productId, page], queryFn: () => api.listOpsActions(merchantId, { page, page_size: OPS_PAGE_SIZE, product_id: productId }), retry: false });
  return <>
    <p className="mb-4 text-sm leading-6 text-text-muted">{t("ops.auditNote")}</p>
    {actions.isPending ? <OpsLoading /> : actions.isError ? <OpsError error={actions.error} retry={() => void actions.refetch()} /> : <>
      {actions.data.items.length === 0 ? <OpsEmpty /> : <ul className="divide-y divide-border-l1">{actions.data.items.map((action) => <li key={action.id} className="space-y-3 py-5">
        <div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><p className="text-sm font-semibold">{t(action.action === "product.delete" ? "ops.productDelete" : "ops.factsUpdate")}</p><p className="mt-1 break-words text-sm text-text-secondary">{action.product_name ?? action.product_id ?? "—"}</p></div><OpsStatus status={action.result} /></div>
        <div className="flex flex-wrap gap-x-6 gap-y-2 text-xs text-text-muted"><span className="min-w-0 break-words">{t("ops.actor")}: {action.actor_name || action.actor_user_id}</span><OpsTime value={action.created_at} /></div>
        {action.failure_reason ? <p className="break-words text-sm text-state-error">{action.failure_reason}</p> : null}
      </li>)}</ul>}
      <OpsPagination page={page} total={actions.data.total} onPage={setPage} />
    </>}
  </>;
}
