import { Check, Loader2, Play, Send } from "lucide-react";
import { useEffect, useState } from "react";

import { useI18n } from "../../../lib/preferences";
import type { AgentQuestion, AgentQuestionAnswer } from "../../../lib/types";

interface AgentQuestionPromptProps {
  question: AgentQuestion;
  answered: boolean;
  resumeRequired: boolean;
  busy: boolean;
  error: string | null;
  onAnswer: (answer: AgentQuestionAnswer) => void;
  onResume: () => void;
}

export function AgentQuestionPrompt({
  question,
  answered,
  resumeRequired,
  busy,
  error,
  onAnswer,
  onResume,
}: AgentQuestionPromptProps) {
  const { t } = useI18n();
  const [text, setText] = useState("");

  useEffect(() => setText(""), [question.id]);

  if (answered) {
    return (
      <div className="border-t border-zinc-200 bg-blue-50 px-4 py-3 dark:border-slate-800 dark:bg-cyan-400/5">
        <div className="flex items-start gap-2 text-sm text-blue-900 dark:text-cyan-100">
          <Check size={17} className="mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{t("agentWorkbench.questionAnswered")}</div>
            {error ? <div role="alert" className="mt-1 text-xs leading-5 text-red-700 dark:text-red-200">{error}</div> : null}
          </div>
          {resumeRequired ? (
            <button
              type="button"
              onClick={onResume}
              disabled={busy}
              className="inline-flex h-10 shrink-0 items-center gap-2 rounded-md bg-blue-600 px-3 text-xs font-semibold text-white hover:bg-blue-700 disabled:opacity-50 dark:bg-cyan-400 dark:text-[#071018]"
            >
              {busy ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
              {t("agentWorkbench.resume")}
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <section className="agent-question-prompt min-h-0 max-h-[42dvh] shrink overflow-y-auto border-t border-zinc-200 bg-amber-50/80 px-4 py-4 dark:border-slate-800 dark:bg-amber-400/5 sm:max-h-none sm:overflow-visible" aria-labelledby={`agent-question-${question.id}`}>
      <div className="text-xs font-semibold text-amber-800 dark:text-amber-300">{question.header}</div>
      <h3 id={`agent-question-${question.id}`} className="mt-1 text-sm font-semibold leading-6 text-zinc-950 dark:text-white">
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
              className="min-h-11 rounded-md border border-amber-200 bg-white px-3 py-2 text-left hover:border-amber-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-500 disabled:opacity-50 dark:border-amber-300/20 dark:bg-[#111820] dark:hover:border-amber-300/60"
            >
              <span className="block text-xs font-semibold text-zinc-900 dark:text-white">{option.label}</span>
              {option.description ? (
                <span className="mt-1 block text-[11px] leading-4 text-zinc-500 dark:text-slate-400">{option.description}</span>
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
          className="min-h-11 resize-none rounded-md border border-amber-200 bg-white px-3 py-2 text-sm leading-5 outline-none focus:border-amber-500 focus:ring-2 focus:ring-amber-100 disabled:opacity-50 dark:border-amber-300/20 dark:bg-[#111820] dark:text-white dark:focus:border-amber-300 dark:focus:ring-amber-300/10"
        />
        <button
          type="button"
          disabled={busy || !text.trim()}
          onClick={() => onAnswer({ text: text.trim() })}
          aria-label={t("agentWorkbench.answer")}
          title={t("agentWorkbench.answer")}
          className="flex h-11 w-11 items-center justify-center rounded-md bg-amber-600 text-white hover:bg-amber-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-500 disabled:opacity-40"
        >
          {busy ? <Loader2 size={16} className="animate-spin" /> : <Send size={16} />}
        </button>
      </div>
      {error ? <div role="alert" className="mt-2 text-xs leading-5 text-red-700 dark:text-red-200">{error}</div> : null}
    </section>
  );
}
