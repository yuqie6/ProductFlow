import * as TabsPrimitive from "@radix-ui/react-tabs";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { Trash2 } from "lucide-react";
import { Button } from "../components/ui/button";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Tabs, TabList, TabTrigger, TabPanel } from "../components/ui/tabs";
import { api } from "../lib/api";
import { useI18n } from "../lib/preferences";
import { OpsFactsEditor } from "./ops/OpsFactsEditor";
import { OpsMedia } from "./ops/OpsMedia";
import { OpsActions, OpsTasks } from "./ops/OpsRecords";
import { merchantPath, OpsError, OpsLoading, OpsShell, OpsStatus, useLiveView } from "./ops/OpsShared";

type ProductTab = "facts" | "media" | "tasks" | "actions";
export function OpsProductPage() {
  const { merchantId = "", productId = "" } = useParams();
  return <ProductView key={`${merchantId}:${productId}`} merchantId={merchantId} productId={productId} />;
}

function ProductView({ merchantId, productId }: { merchantId: string; productId: string }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const live = useLiveView();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<ProductTab>("facts");
  const [deleting, setDeleting] = useState(false);
  const merchant = useQuery({ queryKey: ["ops-merchant", merchantId], queryFn: () => api.getOpsMerchant(merchantId), retry: false });
  const facts = useQuery({ queryKey: ["ops-facts", merchantId, productId], queryFn: () => api.getOpsProductFacts(merchantId, productId), enabled: merchant.isSuccess, retry: false });
  const deletion = useMutation({ mutationFn: () => api.deleteOpsProduct(merchantId, productId), onSuccess: () => {
    if (!live.current) return;
    queryClient.removeQueries({ queryKey: ["ops-facts", merchantId, productId] });
    queryClient.removeQueries({ queryKey: ["ops-media", merchantId, productId] });
    void queryClient.invalidateQueries({ queryKey: ["ops-products", merchantId] });
    if (live.current) navigate(merchantPath(merchantId), { replace: true });
  }, onSettled: () => { if (!live.current) return; void queryClient.invalidateQueries({ queryKey: ["ops-actions", merchantId] }); } });
  return <OpsShell title={facts.data?.product.name ?? t("ops.detail")} back={{ to: merchantPath(merchantId), label: merchant.data?.name ?? t("ops.merchant") }}>
    {merchant.isPending ? <OpsLoading /> : merchant.isError ? <OpsError error={merchant.error} retry={() => void merchant.refetch()} /> : <>
      <div className="mb-7 flex flex-wrap items-center justify-between gap-4"><p className="min-w-0 break-words text-sm text-text-muted">{t("ops.merchant")}: {merchant.data.name} <OpsStatus status={merchant.data.status} /></p><Button variant="dangerSoft" disabled={!facts.isSuccess} onClick={() => { deletion.reset(); setDeleting(true); }}><Trash2 size={15} aria-hidden="true" />{t("ops.productDelete")}</Button></div>
      {facts.isPending ? <OpsLoading /> : facts.isError ? <OpsError error={facts.error} retry={() => void facts.refetch()} /> : <Tabs value={tab} onValueChange={(value) => { if (value === "facts" || value === "media" || value === "tasks" || value === "actions") setTab(value); }}>
        <TabList aria-label={t("ops.detail")} className="mb-6 flex w-full flex-wrap sm:w-fit"><TabTrigger value="facts" className="min-h-11 px-2 text-[11px] sm:flex-none sm:px-3 sm:text-xs sm:whitespace-nowrap">{t("graph.inspector.productFacts")}</TabTrigger><TabTrigger value="media" className="min-h-11 px-2 text-[11px] sm:flex-none sm:px-3 sm:text-xs sm:whitespace-nowrap">{t("ops.media")}</TabTrigger><TabTrigger value="tasks" className="min-h-11 px-2 text-[11px] sm:flex-none sm:px-3 sm:text-xs sm:whitespace-nowrap">{t("ops.tasks")}</TabTrigger><TabTrigger value="actions" className="min-h-11 px-2 text-[11px] sm:flex-none sm:px-3 sm:text-xs sm:whitespace-nowrap">{t("ops.actions")}</TabTrigger></TabList>
        <TabsPrimitive.Content value="facts" forceMount hidden={tab !== "facts"} className="outline-none"><OpsFactsEditor merchantId={merchantId} productId={productId} initial={facts.data} /></TabsPrimitive.Content>
        <TabPanel value="media"><OpsMedia merchantId={merchantId} productId={productId} /></TabPanel>
        <TabPanel value="tasks"><OpsTasks merchantId={merchantId} productId={productId} /></TabPanel>
        <TabPanel value="actions"><OpsActions merchantId={merchantId} productId={productId} /></TabPanel>
      </Tabs>}
    </>}
    <ConfirmDialog open={deleting} title={t("ops.deleteTitle")} description={`${merchant.data?.name ?? ""} · ${facts.data?.product.name ?? ""}. ${t("ops.deleteNote")}`} confirmLabel={t("ops.productDelete")} cancelLabel={t("account.cancel")} busy={deletion.isPending} onConfirm={() => deletion.mutate()} onClose={() => setDeleting(false)} body={deletion.isError ? <OpsError error={deletion.error} /> : undefined} />
  </OpsShell>;
}
