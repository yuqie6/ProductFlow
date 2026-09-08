import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "../../components/ui/button";
import { Input, TextArea } from "../../components/ui/field";
import { api, ApiError } from "../../lib/api";
import type { TranslationKey } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";
import { OPS_PAGE_SIZE, OpsEmpty, OpsError, OpsLoading, OpsPagination, OpsTime, useLiveView } from "./OpsShared";

const eventKeys: Record<string, TranslationKey> = { reserve: "ops.reserve", settle: "ops.settle", release: "ops.release", adjust: "ops.adjust", mark_unknown: "ops.markUnknown" };

export function OpsQuota({ merchantId }: { merchantId: string }) {
  const { t, locale } = useI18n();
  const queryClient = useQueryClient();
  const live = useLiveView();
  const [page, setPage] = useState(1);
  const [delta, setDelta] = useState("");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [operation, setOperation] = useState<{ idempotency_key: string; delta_units: number; reason: string } | null>(null);
  const balance = useQuery({ queryKey: ["ops-quota", merchantId], queryFn: () => api.getOpsQuota(merchantId), retry: false });
  const events = useQuery({ queryKey: ["ops-quota-events", merchantId, page], queryFn: () => api.listOpsQuotaEvents(merchantId, { page, page_size: OPS_PAGE_SIZE }), retry: false });
  const adjustment = useMutation({
    mutationFn: (input: NonNullable<typeof operation>) => api.adjustOpsQuota(merchantId, input),
    onSuccess: (account) => {
      if (!live.current) return;
      queryClient.setQueryData(["ops-quota", merchantId], account);
      void queryClient.invalidateQueries({ queryKey: ["ops-quota-events", merchantId] });
      if (live.current) { setError(""); setPage(1); }
    },
  });
  const number = (value: number) => new Intl.NumberFormat(locale).format(value);
  const rejected = adjustment.error instanceof ApiError && adjustment.error.status >= 400 && adjustment.error.status < 500 && ![408, 429].includes(adjustment.error.status);
  return <div>
    {balance.isPending ? <OpsLoading /> : balance.isError ? <OpsError error={balance.error} retry={() => void balance.refetch()} /> : <dl className="mb-8 grid grid-cols-2 gap-4 border-b border-border-l1 pb-6"><div><dt className="text-xs text-text-muted">{t("ops.available")}</dt><dd className="mt-2 text-2xl font-semibold tabular-nums">{number(balance.data.available_units)}</dd></div><div><dt className="text-xs text-text-muted">{t("ops.reserved")}</dt><dd className="mt-2 text-2xl font-semibold tabular-nums">{number(balance.data.reserved_units)}</dd></div><p className="col-span-2 text-xs text-text-muted">{t("ops.units")}</p></dl>}
    <section aria-labelledby="quota-adjust-heading" className="mb-8 border-b border-border-l1 pb-8">
      <h2 id="quota-adjust-heading" className="mb-4 font-semibold">{t("ops.adjust")}</h2>
      <form className="max-w-lg space-y-4" onSubmit={(event) => {
        event.preventDefault(); if (adjustment.isPending || adjustment.isSuccess) return;
        const amount = Number(delta);
        if (!operation && (!Number.isSafeInteger(amount) || amount === 0 || !reason.trim())) { setError(t("ops.adjustInvalid")); return; }
        const input = operation ?? { idempotency_key: crypto.randomUUID(), delta_units: amount, reason: reason.trim() };
        setOperation(input); setError(""); adjustment.mutate(input);
      }}>
        <Input label={t("ops.delta")} value={delta} onChange={(event) => setDelta(event.target.value)} inputMode="numeric" disabled={Boolean(operation)} required />
        <TextArea label={t("ops.reason")} value={reason} onChange={setReason} disabled={Boolean(operation)} required maxLength={2000} />
        {error ? <p role="alert" className="text-sm text-state-error">{error}</p> : null}
        {adjustment.isError ? <><OpsError error={adjustment.error} />{!rejected ? <p className="text-sm leading-6 text-text-muted">{t("ops.adjustRetry")}</p> : null}</> : null}
        {adjustment.isSuccess ? <p role="status" className="text-sm text-state-success">{t("ops.adjusted")}</p> : null}
        <div className="flex flex-wrap gap-3"><Button type="submit" variant="primary" busy={adjustment.isPending} disabled={adjustment.isSuccess}>{t(adjustment.isError ? "account.retry" : "ops.adjust")}</Button>{adjustment.isSuccess || rejected ? <Button onClick={() => { setOperation(null); setDelta(""); setReason(""); setError(""); adjustment.reset(); }}>{t("ops.newAdjustment")}</Button> : null}</div>
      </form>
    </section>
    {events.isPending ? <OpsLoading /> : events.isError ? <OpsError error={events.error} retry={() => void events.refetch()} /> : <>
      {events.data.items.length === 0 ? <OpsEmpty /> : <ul className="divide-y divide-border-l1">{events.data.items.map((event) => <li key={event.id} className="space-y-3 py-5">
        <div className="flex flex-wrap items-center justify-between gap-3"><p className="text-sm font-semibold">{t(eventKeys[event.event_type] ?? "ops.quota")}</p><span className="font-semibold tabular-nums">{number(event.amount_units)} <span className="text-xs font-normal text-text-muted">{t("ops.units")}</span></span></div>
        {event.reason ? <p className="break-words text-sm text-text-secondary">{event.reason}</p> : null}
        <dl className="flex flex-wrap gap-x-6 gap-y-2 text-xs text-text-muted"><div><dt className="inline">{t("ops.available")}: </dt><dd className="inline">{number(event.available_after)}</dd></div><div><dt className="inline">{t("ops.reserved")}: </dt><dd className="inline">{number(event.reserved_after)}</dd></div><div><dt className="sr-only">{t("ops.time")}</dt><dd><OpsTime value={event.created_at} /></dd></div>{event.actor_user_id ? <div className="min-w-0 break-all"><dt className="inline">{t("ops.actor")}: </dt><dd className="inline">{event.actor_user_id}</dd></div> : null}</dl>
      </li>)}</ul>}
      <OpsPagination page={page} total={events.data.total} onPage={setPage} />
    </>}
  </div>;
}
