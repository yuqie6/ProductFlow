import { useEffect, useRef, useState } from "react";

import type { DownloadableImage } from "../../../../lib/image-downloads";
import { api } from "../../../../lib/api";
import type {
  AgentAttachment,
  AgentPageContextSnapshotInput,
  AgentQuestionAnswer,
  AgentTurn,
  SubmitAgentTurnInput,
} from "../../../../lib/types";
import { agentTurnRetrySubmitInput, canRetryAgentTurn, retryIdempotencyKey } from "../agentTurnRetry";
import type { UseAgentTurnEventsResult } from "../useAgentTurnEvents";
import { canSubmitAgentConversationMessage, errorDetail } from "./helpers";
import { echoMatchesTurn, type PendingUserEcho } from "./types";

export interface ConversationChromeAgent {
  activeTurn: AgentTurn | null;
  latestTurn: AgentTurn | null;
  turns: AgentTurn[];
  submitTurnMutation: {
    isPending: boolean;
    error: unknown;
    mutateAsync: (input: SubmitAgentTurnInput) => Promise<unknown>;
  };
  cancelTurnMutation: { isPending: boolean; error: unknown; mutate: (id: string) => void };
  resumeTurnMutation: { isPending: boolean; error: unknown; mutate: (id: string) => void };
  answerQuestionMutation: {
    isPending: boolean;
    error: unknown;
    mutateAsync: (input: {
      projectionId: string;
      questionId: string;
      answer: AgentQuestionAnswer;
    }) => Promise<unknown>;
  };
}

export interface ConversationChrome<TAsset extends AgentAttachment> {
  composerText: string;
  setComposerText: (value: string) => void;
  composerAssets: TAsset[];
  setComposerAssets: (value: TAsset[] | ((current: TAsset[]) => TAsset[])) => void;
  assetPickerOpen: boolean;
  setAssetPickerOpen: (open: boolean) => void;
  preview: DownloadableImage | null;
  setPreview: (image: DownloadableImage | null) => void;
  previewError: string | null;
  pendingEcho: PendingUserEcho | null;
  retryingTurnId: string | null;
  canSubmitMessage: boolean;
  stopAvailable: boolean;
  isStopping: boolean;
  activeQuestion: NonNullable<AgentTurn["question"]> | null;
  questionAnswered: boolean;
  rotateComposerKey: () => void;
  submitMessage: () => Promise<void>;
  retryTurn: (turn: AgentTurn) => Promise<void>;
  answerQuestion: (answer: AgentQuestionAnswer) => Promise<void>;
  previewSelectedAsset: (asset: AgentAttachment) => void;
  previewTurnAsset: (assetId: string) => Promise<void>;
  removeComposerAsset: (assetId: string) => void;
}

export function useConversationChrome<TAsset extends AgentAttachment>(input: {
  conversationKey: string;
  resetComposerOnKeyChange?: boolean;
  agent: ConversationChromeAgent;
  events: UseAgentTurnEventsResult;
  pageContext?: AgentPageContextSnapshotInput | null;
  submitTaskId?: string | null;
  loadPreviewAsset: (assetId: string) => Promise<AgentAttachment>;
  previewFailedLabel: string;
}): ConversationChrome<TAsset> {
  const {
    conversationKey,
    resetComposerOnKeyChange = false,
    agent,
    events,
    pageContext = null,
    submitTaskId = null,
    loadPreviewAsset,
    previewFailedLabel,
  } = input;
  const [composerText, setComposerText] = useState("");
  const [composerAssets, setComposerAssets] = useState<TAsset[]>([]);
  const [assetPickerOpen, setAssetPickerOpen] = useState(false);
  const [preview, setPreview] = useState<DownloadableImage | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [answeredQuestionId, setAnsweredQuestionId] = useState<string | null>(null);
  const composerKeyRef = useRef(globalThis.crypto.randomUUID());
  const retryKeysRef = useRef(new Map<string, string>());
  const [retryingTurnId, setRetryingTurnId] = useState<string | null>(null);
  const [pendingEcho, setPendingEcho] = useState<PendingUserEcho | null>(null);

  const activeQuestion =
    events.state.turn_key === agent.activeTurn?.id && events.state.question
      ? events.state.question
      : agent.activeTurn?.question ?? null;

  useEffect(() => setAnsweredQuestionId(null), [activeQuestion?.id]);
  useEffect(() => {
    setPendingEcho(null);
    if (!resetComposerOnKeyChange) return;
    setComposerText("");
    setComposerAssets([]);
    setAssetPickerOpen(false);
    setPreview(null);
    setPreviewError(null);
    setAnsweredQuestionId(null);
    setRetryingTurnId(null);
    composerKeyRef.current = globalThis.crypto.randomUUID();
    retryKeysRef.current = new Map();
  }, [conversationKey, resetComposerOnKeyChange]);
  useEffect(() => {
    if (!pendingEcho) return;
    const turns = [agent.latestTurn, ...agent.turns].filter((item): item is AgentTurn => Boolean(item));
    if (turns.some((item) => echoMatchesTurn(pendingEcho, item))) {
      setPendingEcho(null);
    }
  }, [agent.latestTurn, agent.turns, pendingEcho]);
  useEffect(() => {
    if (!assetPickerOpen && !preview) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (preview) setPreview(null);
      else setAssetPickerOpen(false);
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [assetPickerOpen, preview]);

  const rotateComposerKey = () => {
    composerKeyRef.current = globalThis.crypto.randomUUID();
  };
  const snapshotPageContext = (assetIds: string[]): AgentPageContextSnapshotInput | null => {
    if (!pageContext) return null;
    return {
      ...pageContext,
      selected_asset_ids: assetIds,
      captured_at: new Date().toISOString(),
    };
  };
  const canSubmitMessage = canSubmitAgentConversationMessage({ activeTurn: agent.activeTurn }) && !pendingEcho;
  const submitMessage = async () => {
    const normalized = composerText.trim();
    if (!normalized || !canSubmitMessage || agent.submitTurnMutation.isPending) {
      return;
    }
    const assets = composerAssets.map((asset) => asset.id);
    const selected = composerAssets;
    setPendingEcho({
      text: normalized,
      assetIds: assets,
      createdAt: new Date().toISOString(),
    });
    setComposerText("");
    setComposerAssets([]);
    try {
      await agent.submitTurnMutation.mutateAsync({
        input_text: normalized,
        asset_ids: assets,
        idempotency_key: composerKeyRef.current,
        task_id: submitTaskId,
        page_context: snapshotPageContext(assets),
      });
      rotateComposerKey();
    } catch {
      setPendingEcho(null);
      setComposerText(normalized);
      setComposerAssets(selected);
    }
  };
  const answerQuestion = async (answer: AgentQuestionAnswer) => {
    if (!agent.activeTurn || !activeQuestion || agent.answerQuestionMutation.isPending) {
      return;
    }
    try {
      await agent.answerQuestionMutation.mutateAsync({
        projectionId: agent.activeTurn.id,
        questionId: activeQuestion.id,
        answer,
      });
      setAnsweredQuestionId(activeQuestion.id);
    } catch {
      // 已持久化的答案和续跑仍可通过同一 question key 重试
    }
  };
  const retryTurn = async (turn: AgentTurn) => {
    if (
      !canRetryAgentTurn({ turn }) ||
      Boolean(agent.activeTurn) ||
      agent.submitTurnMutation.isPending
    ) {
      return;
    }
    let key = retryKeysRef.current.get(turn.id);
    if (!key) {
      key = retryIdempotencyKey(turn.id);
      retryKeysRef.current.set(turn.id, key);
    }
    setRetryingTurnId(turn.id);
    try {
      await agent.submitTurnMutation.mutateAsync(
        agentTurnRetrySubmitInput(turn, {
          idempotencyKey: key,
          taskId: turn.task_id ?? submitTaskId,
          pageContext: snapshotPageContext(turn.input_asset_ids),
        }),
      );
      retryKeysRef.current.delete(turn.id);
    } catch {
      // 失败请求沿用同一续跑键
    } finally {
      setRetryingTurnId(null);
    }
  };
  const previewSelectedAsset = (asset: AgentAttachment) => {
    setPreviewError(null);
    setPreview({
      previewUrl: api.toApiUrl(asset.preview_url),
      downloadUrl: api.toApiUrl(asset.download_url),
      filename: asset.original_filename,
      alt: asset.display_name,
    });
  };
  const previewTurnAsset = async (assetId: string) => {
    setPreviewError(null);
    try {
      previewSelectedAsset(await loadPreviewAsset(assetId));
    } catch (error) {
      setPreviewError(errorDetail(error, previewFailedLabel));
    }
  };
  const questionAnswered = Boolean(
    activeQuestion &&
    (answeredQuestionId === activeQuestion.id ||
      events.state.question_answered ||
      agent.activeTurn?.resume_required),
  );

  return {
    composerText,
    setComposerText,
    composerAssets,
    setComposerAssets,
    assetPickerOpen,
    setAssetPickerOpen,
    preview,
    setPreview,
    previewError,
    pendingEcho,
    retryingTurnId,
    canSubmitMessage,
    stopAvailable: Boolean(agent.activeTurn && agent.activeTurn.status !== "awaiting_confirmation"),
    isStopping:
      agent.cancelTurnMutation.isPending ||
      agent.activeTurn?.status === "cancel_requested" ||
      Boolean(events.state.terminal_kind && events.state.terminal_kind !== "turn.awaiting_confirmation"),
    activeQuestion,
    questionAnswered,
    rotateComposerKey,
    submitMessage,
    retryTurn,
    answerQuestion,
    previewSelectedAsset,
    previewTurnAsset,
    removeComposerAsset: (assetId: string) => {
      setComposerAssets((current) => current.filter((asset) => asset.id !== assetId));
      rotateComposerKey();
    },
  };
}
