import { Send } from "lucide-react";
import { useEffect, useState } from "react";

import { IconButton } from "../../../components/ui/icon-button";
import { useI18n } from "../../../lib/preferences";
import type { AgentQuestion, AgentQuestionAnswer } from "../../../lib/types";

interface AgentQuestionPromptProps {
  question: AgentQuestion;
  busy: boolean;
  error: string | null;
  onAnswer: (answer: AgentQuestionAnswer) => void;
}

export function AgentQuestionPrompt({
  question,
  busy,
  error,
  onAnswer,
}: AgentQuestionPromptProps) {
  const { t } = useI18n();
  const [text, setText] = useState("");

  useEffect(() => setText(""), [question.id]);

  return (
    <section className="px-4 pb-1 pt-3" aria-labelledby={`agent-question-${question.id}`}>
      <div className="text-[11px] font-semibold uppercase tracking-wide text-accent">{question.header}</div>
      <h3 id={`agent-question-${question.id}`} className="mt-1 text-sm font-semibold leading-6 text-text-primary">
        {question.question}
      </h3>
      {question.options.length ? (
        <div className="mt-3 grid gap-2 sm:grid-cols-2">
          {question.options.map((option, index) => (
            <button
              key={`${index}:${option.label}`}
              type="button"
              disabled={busy}
              onClick={() => onAnswer({ option: index })}
              className="min-h-11 rounded-xl border border-border-l2 bg-surface-base px-3 py-2 text-left hover:border-accent/50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
            >
              <span className="block text-xs font-semibold text-text-primary">{option.label}</span>
              {option.description ? (
                <span className="mt-1 block text-[11px] leading-4 text-text-muted">{option.description}</span>
              ) : null}
            </button>
          ))}
        </div>
      ) : null}
      <div className="mt-3 grid grid-cols-[minmax(0,1fr)_44px] items-end gap-2">
        <textarea
          value={text}
          rows={2}
          maxLength={4000}
          disabled={busy}
          onChange={(event) => setText(event.target.value)}
          placeholder={t("agentWorkbench.questionTextPlaceholder")}
          aria-label={t("agentWorkbench.questionTextLabel")}
          className="min-h-11 resize-none rounded-xl border border-border-l2 bg-surface-base px-3 py-2 text-sm leading-5 outline-none focus:border-accent focus:ring-2 focus:ring-accent/15 disabled:opacity-50"
        />
        <IconButton
          label={t("agentWorkbench.answer")}
          variant="primary"
          size="toolbar"
          disabled={busy || !text.trim()}
          busy={busy}
          onClick={() => onAnswer({ text: text.trim() })}
        >
          <Send size={16} />
        </IconButton>
      </div>
      <div className="mt-2 flex items-center justify-between gap-3">
        <button
          type="button"
          disabled={busy}
          onClick={() => onAnswer({ skip: true })}
          className="h-8 rounded-md px-2 text-xs font-medium text-text-muted transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-40"
        >
          {t("agentWorkbench.skipQuestion")}
        </button>
        {error ? <div role="alert" className="text-xs leading-5 text-state-error">{error}</div> : null}
      </div>
    </section>
  );
}
