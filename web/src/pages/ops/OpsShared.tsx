import { useEffect, useMemo, useRef, type ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";

import { TopNav } from "../../components/TopNav";
import { Button } from "../../components/ui/button";
import { ApiError } from "../../lib/api";
import { getAccountGeneration, isCurrentAccountGeneration } from "../../lib/accountBoundary";
import type { TranslationKey } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";

export const OPS_PAGE_SIZE = 20;
export const merchantPath = (id: string) => `/ops/merchants/${encodeURIComponent(id)}`;
export const productPath = (merchantId: string, productId: string) => `${merchantPath(merchantId)}/products/${encodeURIComponent(productId)}`;

export function useLiveView() {
  const live = useRef(true);
  const generation = useRef(getAccountGeneration());
  useEffect(() => { live.current = true; return () => { live.current = false; }; }, []);
  return useMemo(() => ({ get current() { return live.current && isCurrentAccountGeneration(generation.current); } }), []);
}

export function OpsShell({ title, back, children }: { title: string; back?: { to: string; label: string }; children: ReactNode }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  return <div className="min-h-screen bg-surface-base text-text-primary">
    <TopNav breadcrumbs={t("ops.title")} onHome={() => navigate("/home")} />
    <main className="mx-auto max-w-6xl px-5 pt-7 pb-40 sm:px-8 lg:pt-10 lg:pb-24">
      {back ? <Link to={back.to} className="mb-4 flex min-h-11 w-fit items-center gap-2 rounded-control text-sm text-text-muted hover:text-text-primary focus-visible:ring-2 focus-visible:ring-focus-ring"><ArrowLeft size={15} aria-hidden="true" />{back.label}</Link> : null}
      <h1 className="mb-7 break-words text-2xl font-semibold tracking-tight">{title}</h1>
      {children}
    </main>
  </div>;
}

export function OpsError({ error, retry }: { error: Error; retry?: () => void }) {
  const { t } = useI18n();
  return <div className="space-y-3 py-4"><p role="alert" className="break-words text-sm text-state-error">{error instanceof ApiError ? error.detail : t("account.error")}</p>{retry ? <Button onClick={retry}>{t("account.retry")}</Button> : null}</div>;
}

export function OpsLoading() {
  const { t } = useI18n();
  return <p role="status" className="py-8 text-sm text-text-muted">{t("app.loading")}</p>;
}

export function OpsEmpty() {
  const { t } = useI18n();
  return <p className="py-10 text-center text-sm text-text-muted">{t("ops.empty")}</p>;
}

export function OpsPagination({ page, total, onPage }: { page: number; total: number; onPage: (page: number) => void }) {
  const { t } = useI18n();
  return <div className="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-border-l1 pt-4">
    <span className="text-xs text-text-muted">{t("ops.total", { count: total })} · {t("account.page", { page })}</span>
    <div className="flex gap-2"><Button disabled={page <= 1} onClick={() => onPage(page - 1)}>{t("account.previous")}</Button><Button disabled={page * OPS_PAGE_SIZE >= total} onClick={() => onPage(page + 1)}>{t("account.next")}</Button></div>
  </div>;
}

export function OpsTime({ value }: { value: string | null }) {
  const { locale } = useI18n();
  return value ? <time dateTime={value}>{new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value))}</time> : <span>—</span>;
}

const statusKeys: Record<string, TranslationKey> = {
  active: "ops.active", suspended: "ops.suspended", succeeded: "agentWorkbench.status.succeeded",
  completed: "agentWorkbench.status.succeeded", failed: "agentWorkbench.status.failed", rejected: "ops.rejected",
  unknown: "agentWorkbench.status.unknown", canceled: "agentWorkbench.status.canceled", cancelled: "agentWorkbench.status.canceled",
  queued: "agentWorkbench.status.queued", running: "agentWorkbench.status.running", pending: "agentWorkbench.status.queued",
  waiting: "agentWorkbench.toolStep.detail.pending", awaiting_confirmation: "agentWorkbench.toolStep.detail.pending",
  collecting: "agentWorkbench.status.running", blocked: "graph.preview.blocked",
  waiting_user: "globalAgent.taskStatus.waitingUser", paused: "globalAgent.taskStatus.paused", draft: "status.draft",
};
export function OpsStatus({ status }: { status: string }) {
  const { t } = useI18n();
  return <span className="inline-flex rounded-control border border-border-l1 px-2 py-1 text-xs text-text-secondary">{statusKeys[status] ? t(statusKeys[status]) : status}</span>;
}
