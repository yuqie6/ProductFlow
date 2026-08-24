import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError, api } from "../../../lib/api";
import {
  createImageFidelityIdempotencyKey,
  imageFidelityRequestFingerprint,
  normalizeImageFidelityNotes,
} from "../../../lib/imageFidelityChecks";
import { useI18n } from "../../../lib/preferences";
import type { TranslateFunction } from "../../../lib/preferences";
import type {
  ProductImageFidelityCheckListResponse,
} from "../../../lib/types";
import {
  ImageFidelityCheckPanel,
  type ImageFidelityCheckDraft,
  type ImageFidelityCheckLabels,
  type ImageFidelityCheckSubmitPayload,
} from "./ImageFidelityCheckPanel";

export interface ImageFidelityCheckControllerProps {
  productId: string;
  assetId: string;
  locale: NonNullable<TranslateFunction["locale"]>;
}

export function emptyImageFidelityCheckDraft(): ImageFidelityCheckDraft {
  return {
    shape_fidelity: null,
    color_material_fidelity: null,
    logo_text_legibility: null,
    text_policy_compliance: null,
    notes: "",
  };
}

export function imageFidelityCheckLabels(t: TranslateFunction): ImageFidelityCheckLabels {
  return {
    title: t("graph.fidelity.title"),
    latestVersion: t("graph.fidelity.latestVersion"),
    latestCheck: t("graph.fidelity.latestCheck"),
    emptyHistory: t("graph.fidelity.emptyHistory"),
    checkedBy: t("graph.fidelity.checkedBy"),
    checkedAt: t("graph.fidelity.checkedAt"),
    noNotes: t("graph.fidelity.noNotes"),
    loading: t("graph.fidelity.loading"),
    saving: t("graph.fidelity.saving"),
    reload: t("graph.fidelity.reload"),
    reloading: t("graph.fidelity.reloading"),
    save: t("graph.fidelity.save"),
    notes: t("graph.fidelity.notes"),
    notesCount: t("graph.fidelity.notesCount"),
    required: t("graph.fidelity.required"),
    notesTooLong: t("graph.fidelity.notesTooLong"),
    errorTitle: t("graph.fidelity.errorTitle"),
    conflictTitle: t("graph.fidelity.conflictTitle"),
    outcomeLabels: {
      pass: t("graph.fidelity.outcome.pass"),
      fail: t("graph.fidelity.outcome.fail"),
      not_applicable: t("graph.fidelity.outcome.notApplicable"),
    },
    fieldLabels: {
      shape_fidelity: t("graph.fidelity.field.shape"),
      color_material_fidelity: t("graph.fidelity.field.colorMaterial"),
      logo_text_legibility: t("graph.fidelity.field.logoText"),
      text_policy_compliance: t("graph.fidelity.field.textPolicy"),
    },
  };
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.detail) return error.detail;
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

interface ImageFidelityCheckControllerState {
  draft: ImageFidelityCheckDraft;
  history: ProductImageFidelityCheckListResponse | null;
  latestVersionKnown: boolean;
  loading: boolean;
  saving: boolean;
  reloading: boolean;
  error: string | null;
  conflict: string | null;
  updateDraft: (draft: ImageFidelityCheckDraft) => void;
  reload: () => Promise<void>;
  submit: (payload: ImageFidelityCheckSubmitPayload) => Promise<void>;
}

export function useImageFidelityCheckController(
  productId: string,
  assetId: string,
  t: TranslateFunction,
): ImageFidelityCheckControllerState {
  const [draft, setDraft] = useState<ImageFidelityCheckDraft>(emptyImageFidelityCheckDraft);
  const [history, setHistory] = useState<ProductImageFidelityCheckListResponse | null>(null);
  const [latestVersionKnown, setLatestVersionKnown] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [reloading, setReloading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<string | null>(null);
  const requestSequenceRef = useRef(0);
  const idempotencyKeysRef = useRef(new Map<string, string>());
  const translateRef = useRef(t);
  translateRef.current = t;

  const loadHistory = useCallback(async (mode: "initial" | "reload") => {
    const requestSequence = requestSequenceRef.current + 1;
    requestSequenceRef.current = requestSequence;
    if (mode === "initial") {
      setLoading(true);
    } else {
      setReloading(true);
      setConflict(null);
    }
    setError(null);
    try {
      const nextHistory = await api.listProductImageFidelityChecks(productId, assetId);
      if (requestSequence !== requestSequenceRef.current) return;
      setHistory(nextHistory);
      setLatestVersionKnown(true);
    } catch (loadFailure) {
      if (requestSequence !== requestSequenceRef.current) return;
      setLatestVersionKnown(false);
      setError(errorMessage(loadFailure, translateRef.current("graph.fidelity.loadFailed")));
    } finally {
      if (requestSequence === requestSequenceRef.current) {
        if (mode === "initial") setLoading(false);
        else setReloading(false);
      }
    }
  }, [assetId, productId]);

  useEffect(() => {
    setDraft(emptyImageFidelityCheckDraft());
    idempotencyKeysRef.current.clear();
    setHistory(null);
    setLatestVersionKnown(false);
    setLoading(true);
    setSaving(false);
    setReloading(false);
    setError(null);
    setConflict(null);
    void loadHistory("initial");
  }, [assetId, loadHistory, productId]);

  const submit = useCallback(async (payload: ImageFidelityCheckSubmitPayload) => {
    if (!latestVersionKnown || saving) return;
    const expectedLatestVersion = history?.latest_version;
    if (expectedLatestVersion === undefined || payload.expectedLatestVersion !== expectedLatestVersion) return;

    const inputWithoutKey = {
      expected_latest_version: payload.expectedLatestVersion,
      shape_fidelity: payload.shape_fidelity,
      color_material_fidelity: payload.color_material_fidelity,
      logo_text_legibility: payload.logo_text_legibility,
      text_policy_compliance: payload.text_policy_compliance,
      notes: normalizeImageFidelityNotes(payload.notes) || null,
    } as const;
    const fingerprint = imageFidelityRequestFingerprint(assetId, inputWithoutKey);
    const idempotencyKey = idempotencyKeysRef.current.get(fingerprint) ?? createImageFidelityIdempotencyKey(assetId, inputWithoutKey);
    idempotencyKeysRef.current.set(fingerprint, idempotencyKey);
    setSaving(true);
    setError(null);
    let writeAccepted = false;
    try {
      await api.createProductImageFidelityCheck(productId, assetId, {
        ...inputWithoutKey,
        idempotency_key: idempotencyKey,
      });
      writeAccepted = true;
      const refreshedHistory = await api.listProductImageFidelityChecks(productId, assetId);
      setHistory(refreshedHistory);
      setLatestVersionKnown(true);
      setDraft(emptyImageFidelityCheckDraft());
      setConflict(null);
    } catch (saveFailure) {
      if (saveFailure instanceof ApiError && saveFailure.status === 409) {
        setConflict(errorMessage(saveFailure, translateRef.current("graph.fidelity.conflictTitle")));
      } else {
        if (writeAccepted) setLatestVersionKnown(false);
        setError(errorMessage(saveFailure, translateRef.current("graph.fidelity.saveFailed")));
      }
    } finally {
      setSaving(false);
    }
  }, [assetId, history?.latest_version, latestVersionKnown, productId, saving]);

  return {
    draft,
    history,
    latestVersionKnown,
    loading,
    saving,
    reloading,
    error,
    conflict,
    updateDraft: setDraft,
    reload: () => loadHistory("reload"),
    submit,
  };
}

export function ImageFidelityCheckController({
  productId,
  assetId,
  locale,
}: ImageFidelityCheckControllerProps) {
  const { t } = useI18n();
  const labels = imageFidelityCheckLabels(t);
  const controller = useImageFidelityCheckController(productId, assetId, t);
  return (
    <ImageFidelityCheckPanel
      value={controller.draft}
      onChange={controller.updateDraft}
      latestVersion={controller.history?.latest_version ?? 0}
      latestVersionKnown={controller.latestVersionKnown}
      latestCheck={controller.history?.items[0] ?? null}
      locale={locale}
      loading={controller.loading}
      saving={controller.saving}
      reloading={controller.reloading}
      error={controller.error}
      conflict={controller.conflict}
      labels={labels}
      onSubmit={controller.submit}
      onReload={controller.reload}
    />
  );
}
