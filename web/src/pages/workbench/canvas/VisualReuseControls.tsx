import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BookmarkPlus, Check, Layers } from "lucide-react";

import { Button } from "../../../components/ui/button";
import { IconButton } from "../../../components/ui/icon-button";
import { api } from "../../../lib/api";
import { useI18n } from "../../../lib/preferences";
import type { GraphNode } from "../../../lib/types";
import {
  brandPlaceholderLabelKey,
  hasNewerVisualVersion,
  layerLabelKey,
  overlayPayloadFromConfig,
} from "./visualReuse";

/**
 * 视觉方案节点上的保存/选择/预览/显式采用。
 * 追加新版本不会静默改本商品选择；有更新时需点采用。
 */
export function VisualReuseControls({
  productId,
  node,
  disabled,
  onAdoptPayload,
}: {
  productId: string;
  node: GraphNode;
  disabled?: boolean;
  onAdoptPayload?: (payload: Record<string, unknown>, versionId: string) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [systemId, setSystemId] = useState("");
  const overlay = overlayPayloadFromConfig(node.config);
  const systems = useQuery({
    queryKey: ["visual-systems"],
    queryFn: () => api.listVisualSystems(),
    retry: false,
  });
  const inheritance = useQuery({
    queryKey: ["visual-inheritance", productId, node.id],
    queryFn: () => api.getVisualInheritance(productId, overlay),
    retry: false,
  });
  const saveNew = useMutation({
    mutationFn: () =>
      api.createVisualSystem({
        name: name.trim() || t("visualReuse.defaultName"),
        payload: overlay,
      }),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["visual-systems"] });
      const versionId = created.current_version?.id;
      if (versionId) {
        await api.selectProductVisualVersion(productId, versionId);
        onAdoptPayload?.(created.current_version?.payload ?? overlay, versionId);
        await queryClient.invalidateQueries({ queryKey: ["visual-inheritance", productId] });
      }
      setName("");
    },
  });
  const append = useMutation({
    mutationFn: () => api.appendVisualSystemVersion(systemId, overlay),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["visual-systems"] });
      await queryClient.invalidateQueries({ queryKey: ["visual-inheritance", productId] });
    },
  });
  const select = useMutation({
    mutationFn: (versionId: string) => api.selectProductVisualVersion(productId, versionId),
    onSuccess: async (selection) => {
      onAdoptPayload?.(selection.version.payload, selection.visual_system_version_id);
      await queryClient.invalidateQueries({ queryKey: ["visual-inheritance", productId] });
    },
  });
  const adoptNewer = useMutation({
    mutationFn: async () => {
      const newer = inheritance.data?.newer_version_available;
      if (!newer) throw new Error("missing newer version");
      return api.selectProductVisualVersion(productId, newer.id);
    },
    onSuccess: async (selection) => {
      onAdoptPayload?.(selection.version.payload, selection.visual_system_version_id);
      await queryClient.invalidateQueries({ queryKey: ["visual-inheritance", productId] });
    },
  });
  const busy = disabled || saveNew.isPending || append.isPending || select.isPending || adoptNewer.isPending;
  const fieldClass =
    "w-full min-w-0 rounded-md border border-border-l2 bg-surface-raised px-2 py-1.5 text-xs text-text-primary focus:outline-none focus:ring-2 focus:ring-accent";

  return (
    <section data-visual-reuse-controls className="space-y-3 border-t border-border-l1 pt-3">
      <div className="flex items-center gap-2 text-xs font-semibold text-text-primary">
        <Layers size={14} aria-hidden="true" />
        {t("visualReuse.title")}
      </div>
      <p className="text-[11px] leading-5 text-text-secondary">{t("visualReuse.priorityHint")}</p>

      <ul data-visual-reuse-layers className="space-y-1 text-[11px] text-text-secondary">
        {(inheritance.data?.layers ?? []).map((layer) => (
          <li
            key={layer.layer}
            data-visual-reuse-layer={layer.layer}
            data-active={layer.active ? "true" : "false"}
            className={layer.active ? "text-text-primary" : undefined}
          >
            <span className="font-medium">{t(layerLabelKey(layer.layer))}</span>
            {" · "}
            {layer.note}
          </li>
        ))}
      </ul>

      {inheritance.data?.brand_placeholder ? (
        <p data-visual-reuse-brand-placeholder className="text-[11px] text-text-muted">
          {inheritance.data.brand_placeholder.detail ||
            t(brandPlaceholderLabelKey(inheritance.data.brand_placeholder.reason))}
        </p>
      ) : null}

      {hasNewerVisualVersion(inheritance.data) ? (
        <div className="flex items-center gap-2 rounded-md border border-border-l2 bg-surface-subtle px-2 py-2">
          <p className="min-w-0 flex-1 text-[11px] text-text-secondary">
            {t("visualReuse.newerAvailable", {
              version: inheritance.data?.newer_version_available?.version ?? "",
            })}
          </p>
          <IconButton
            label={t("visualReuse.adoptNewer")}
            disabled={busy}
            onClick={() => adoptNewer.mutate()}
            data-visual-reuse-adopt-newer
          >
            <Check size={14} />
          </IconButton>
        </div>
      ) : null}

      <div className="space-y-2">
        <label className="block text-[11px] text-text-secondary" htmlFor={`visual-reuse-name-${node.id}`}>
          {t("visualReuse.saveAs")}
        </label>
        <div className="flex gap-2">
          <input
            id={`visual-reuse-name-${node.id}`}
            className={fieldClass}
            value={name}
            disabled={busy}
            maxLength={255}
            onChange={(event) => setName(event.target.value)}
            placeholder={t("visualReuse.defaultName")}
          />
          <IconButton
            label={t("visualReuse.save")}
            disabled={busy}
            onClick={() => saveNew.mutate()}
            data-visual-reuse-save
          >
            <BookmarkPlus size={14} />
          </IconButton>
        </div>
      </div>

      <div className="space-y-2">
        <label className="block text-[11px] text-text-secondary" htmlFor={`visual-reuse-system-${node.id}`}>
          {t("visualReuse.selectSystem")}
        </label>
        <select
          id={`visual-reuse-system-${node.id}`}
          className={fieldClass}
          value={systemId}
          disabled={busy || systems.isPending}
          onChange={(event) => setSystemId(event.target.value)}
        >
          <option value="">{t("visualReuse.chooseSystem")}</option>
          {(systems.data ?? []).map((system) => (
            <option key={system.id} value={system.id}>
              {system.name}
              {system.current_version ? ` · v${system.current_version.version}` : ""}
            </option>
          ))}
        </select>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            size="sm"
            variant="secondary"
            disabled={busy || !systemId}
            onClick={() => append.mutate()}
            data-visual-reuse-append
          >
            {t("visualReuse.appendVersion")}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={busy || !systemId || !systems.data?.find((item) => item.id === systemId)?.current_version?.id}
            onClick={() => {
              const versionId = systems.data?.find((item) => item.id === systemId)?.current_version?.id;
              if (versionId) select.mutate(versionId);
            }}
            data-visual-reuse-select
          >
            {t("visualReuse.selectVersion")}
          </Button>
        </div>
      </div>

      {saveNew.isError || append.isError || select.isError || adoptNewer.isError || systems.isError || inheritance.isError ? (
        <p role="alert" className="text-[11px] text-state-error">
          {(saveNew.error ?? append.error ?? select.error ?? adoptNewer.error ?? systems.error ?? inheritance.error)?.message}
        </p>
      ) : null}
    </section>
  );
}
