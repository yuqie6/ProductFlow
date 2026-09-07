import { ArrowRight, Check, Flag, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";

import { useI18n } from "../../lib/preferences";
import { StudioCamera } from "./StudioCamera";
import { useHomeTour } from "./HomeTourContext";

const tasks = ["home.guide.task0", "home.guide.task1", "home.guide.task2", "home.guide.task3"] as const;
const stops = ["demo", "canvas", "edit", "reuse"] as const;
type Phase = "entering" | "active" | "exiting";

export function HomeGuide({ open, onClose, origin }: { open: boolean; onClose: () => void; origin: RefObject<HTMLSpanElement | null> }) {
  const { t } = useI18n();
  const { completed } = useHomeTour();
  const nextButton = useRef<HTMLButtonElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const character = useRef<HTMLDivElement>(null);
  const closeCallback = useRef(onClose);
  closeCallback.current = onClose;
  const [stop, setStop] = useState(0);
  const [phase, setPhase] = useState<Phase>("entering");
  const [celebrating, setCelebrating] = useState(false);
  const done = completed[stop];

  useEffect(() => {
    if (open) return;
    setStop(0); setPhase("entering"); setCelebrating(false);
  }, [open]);

  useLayoutEffect(() => {
    if (!open || phase === "active" || !character.current || !origin.current) return;
    const actor = character.current;
    const dock = actor.getBoundingClientRect();
    const source = origin.current.getBoundingClientRect();
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const leaving = phase === "exiting";
    if (leaving) document.querySelector(".home-intro")?.scrollIntoView({ behavior: reduced ? "instant" : "smooth", block: "start" });
    const finish = () => {
      actor.style.removeProperty("transform");
      if (leaving) closeCallback.current();
      else setPhase("active");
    };
    if (reduced) { finish(); return; }
    let frame = 0;
    const start = performance.now();
    const tick = (now: number) => {
      const progress = Math.min(1, (now - start) / 1100);
      const eased = 1 - Math.pow(1 - progress, 3);
      const target = leaving ? origin.current?.getBoundingClientRect() ?? source : dock;
      const from = leaving ? dock : source;
      const x = from.left + (target.left - from.left) * eased - dock.left;
      const y = from.top + (target.top - from.top) * eased - dock.top - Math.sin(progress * Math.PI) * 105;
      const scale = leaving ? 1 + (target.width / dock.width - 1) * eased : source.width / dock.width + (1 - source.width / dock.width) * eased;
      const squash = progress > .86 ? Math.sin((progress - .86) / .14 * Math.PI * 2) * .09 : 0;
      actor.style.transform = `translate(${x}px, ${y}px) rotate(${Math.sin(progress * Math.PI * 2) * 9}deg) scale(${scale * (1 + squash)}, ${scale * (1 - squash)})`;
      if (progress < 1) frame = requestAnimationFrame(tick);
      else finish();
    };
    tick(start);
    return () => { cancelAnimationFrame(frame); actor.style.removeProperty("transform"); };
  }, [open, phase, origin]);

  useEffect(() => {
    if (open && phase === "active") closeButton.current?.focus({ preventScroll: true });
  }, [open, phase]);
  useEffect(() => {
    if (!open) return;
    const trigger = document.activeElement;
    return () => { if (trigger instanceof HTMLElement) trigger.focus({ preventScroll: true }); };
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.composedPath().some(target => target instanceof Element && target.getAttribute("role") === "dialog")) setPhase("exiting");
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [open]);
  useEffect(() => {
    if (!open || phase !== "active") return;
    document.getElementById(`home-${stops[stop]}`)?.scrollIntoView({
      behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth",
      block: "center",
    });
  }, [open, stop, phase]);

  if (!open) return null;
  const advance = () => {
    if (stop < 3) setStop(stop + 1);
    else { setCelebrating(true); }
  };
  return createPortal(
    <aside className="home-guide" data-stop={stop} data-phase={phase} data-complete={done} aria-label={t("home.guide.start")}>
      <div className="home-guide-flight" ref={character}><div className="home-guide-character" key={`${stop}-${done}`}><StudioCamera step={stop % 3} /></div></div>
      <div className="home-guide-bubble">
        <button className="home-guide-close" ref={closeButton} type="button" disabled={phase !== "active"} onClick={() => setPhase("exiting")} aria-label={t("home.close")}><X size={16} /></button>
        <span className="home-guide-count">{t(celebrating ? "home.guide.clear" : "home.guide.level")} {!celebrating && `0${stop + 1} / 04`}</span>
        <div className="home-guide-progress" aria-hidden="true">{completed.map((value, index) => <span key={index} data-done={value} data-current={stop === index} />)}</div>
        <div className="home-guide-level" key={celebrating ? "clear" : stop}>
          <p aria-live="polite">{t(celebrating ? "home.guide.clearNote" : `home.guide.${stops[stop]}`)}</p>
          <div className="home-guide-objective" data-done={done} aria-live="polite">{done ? <Check size={15} /> : <Flag size={15} />}<span>{t(done ? "home.guide.completed" : tasks[stop])}</span></div>
        </div>
        {celebrating ? <button className="home-guide-next" ref={nextButton} type="button" onClick={() => setPhase("exiting")}>{t("home.guide.return")}<ArrowRight size={16} /></button> : <>
          <button className="home-guide-next" ref={nextButton} type="button" disabled={!done || phase !== "active"} onClick={advance}>{t(stop === 3 ? "home.guide.finish" : "home.guide.next")}<ArrowRight size={16} /></button>
          <button className="home-guide-skip" type="button" disabled={phase !== "active"} onClick={advance}>{t("home.guide.skip")}</button>
        </>}
      </div>
    </aside>, document.body,
  );
}
