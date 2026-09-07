import { ArrowRight, Check, Maximize2, Pause, Play, ScanLine } from "lucide-react";
import { useEffect, useState } from "react";

import { useHomeTour } from "./HomeTourContext";
import { useI18n } from "../../lib/preferences";

export const homeReference = { src: "/home-showcase/forma-reference.webp", width: 806, height: 1475 };
export const homeShots = [
  { id: "hero", title: "home.shot.hero", note: "home.shot.heroNote", width: 1254, height: 1254 },
  { id: "scene", title: "home.shot.scene", note: "home.shot.sceneNote", width: 1134, height: 1387 },
  { id: "detail", title: "home.shot.detail", note: "home.shot.detailNote", width: 1122, height: 1402 },
] as const;

const steps = ["input", "direction", "result"] as const;

interface HomeStudioDemoProps {
  onPreview: (index: number, reference: boolean, trigger: HTMLButtonElement) => void;
}

export function HomeStudioDemo({ onPreview }: HomeStudioDemoProps) {
  const { t } = useI18n();
  const { complete } = useHomeTour();
  const [step, setStep] = useState(2);
  const [playing, setPlaying] = useState(false);
  const [direction, setDirection] = useState(0);

  useEffect(() => {
    if (!playing) return;
    const timeout = window.setTimeout(() => {
      if (step < 2) setStep(step + 1);
      else { setPlaying(false); complete(0); }
    }, step === 1 ? 3600 : 2800);
    return () => window.clearTimeout(timeout);
  }, [playing, step, complete]);

  function selectStep(index: number) {
    setPlaying(false);
    setStep(index);
  }

  return (
    <section id="home-demo" className="studio-demo home-width" aria-labelledby="studio-demo-title">
      <div className="studio-demo-top">
        <h2 id="studio-demo-title"><ScanLine size={16} aria-hidden="true" />{t("home.demo.title")}</h2>
        <button className="studio-play" type="button" aria-label={t(playing ? "home.demo.pause" : "home.demo.play")} onClick={() => {
          if (playing) setPlaying(false);
          else { if (step === 2) setStep(0); setPlaying(true); }
        }}>
          {playing ? <Pause size={15} aria-hidden="true" /> : <Play size={15} aria-hidden="true" />}
          <span>{t(playing ? "home.demo.pause" : "home.demo.play")}</span>
        </button>
      </div>
      <div className="studio-story" data-step={step} data-playing={playing}>
        <div className="studio-story-copy">
          <div className="studio-step-nav" role="group" aria-label={t("home.demo.title")}>
            {steps.map((name, index) => (
              <button type="button" key={name} aria-pressed={step === index} onClick={() => selectStep(index)}>
                <span className="studio-step-dot" aria-hidden="true">{step > index ? <Check size={11} /> : null}</span>
                {t(`home.demo.${name}`)}
              </button>
            ))}
          </div>
          <div className="studio-story-caption" key={step}>
            <h3>{t(`home.demo.${steps[step]}`)}</h3>
            <p>{t(`home.demo.${steps[step]}Note`)}</p>
          </div>
          <p className="studio-disclosure">{t("home.demo.sample")}</p>
        </div>

        <div className="studio-stage">
          {step === 0 && (
            <div className="studio-input-scene studio-scene" key="input">
              <div className="studio-reference-mount">
                <span className="studio-corner studio-corner-tl" /><span className="studio-corner studio-corner-tr" />
                <span className="studio-corner studio-corner-bl" /><span className="studio-corner studio-corner-br" />
                <button className="home-source-button" type="button" aria-label={t("home.preview", { name: t("home.reference") })} onClick={(event) => { setPlaying(false); onPreview(0, true, event.currentTarget); }}>
                  <img src={homeReference.src} alt={t("home.reference")} width={homeReference.width} height={homeReference.height} />
                </button>
                <span className="studio-scan" aria-hidden="true" />
                <span className="studio-reference-label"><ScanLine size={13} />{t("home.reference")}</span>
              </div>
              <div className="studio-brief"><span>{t("home.demo.request")}</span><p>{t("home.demo.brief")}</p></div>
            </div>
          )}
          {step === 1 && (
            <div className="studio-direction-scene studio-scene" key="direction">
              <div className="studio-direction-image">
                <img key={direction} src={`/home-showcase/forma-${homeShots[direction].id}.webp`} alt={t(homeShots[direction].note)} width={homeShots[direction].width} height={homeShots[direction].height} />
                <div className="studio-image-label"><span>{t(homeShots[direction].title)}</span><p>{t(homeShots[direction].note)}</p></div>
              </div>
              <div className="studio-direction-options" role="group" aria-label={t("home.demo.inspect")}>
                {homeShots.map((shot, index) => <button type="button" key={shot.id} aria-pressed={direction === index} onClick={() => { setPlaying(false); setDirection(index); }}><span>{t(shot.title)}</span><ArrowRight size={14} aria-hidden="true" /></button>)}
              </div>
            </div>
          )}
          {step === 2 && (
            <div className="studio-output-scene studio-scene" key="output">
              <div className="studio-output-heading"><span>{t("home.collection")}</span><span>{t("home.category")}</span></div>
              <div className="home-photo-grid">
                {homeShots.map((shot, index) => (
                  <figure className={`home-photo home-photo-${shot.id}`} key={shot.id}>
                    <button className="home-photo-button" type="button" aria-label={t("home.preview", { name: t(shot.title) })} onClick={(event) => { setPlaying(false); onPreview(index, false, event.currentTarget); }}>
                      <img src={`/home-showcase/forma-${shot.id}.webp`} alt={t(shot.note)} width={shot.width} height={shot.height} decoding="async" fetchPriority={index === 0 ? "high" : "auto"} />
                      <span className="home-photo-expand"><Maximize2 size={16} aria-hidden="true" /></span>
                    </button>
                    <figcaption>{t(shot.title)}</figcaption>
                  </figure>
                ))}
              </div>
              <button className="studio-source-chip home-source-button" type="button" onClick={(event) => { setPlaying(false); onPreview(0, true, event.currentTarget); }} aria-label={t("home.preview", { name: t("home.reference") })}>
                <img src={homeReference.src} alt="" width={homeReference.width} height={homeReference.height} /><span>{t("home.reference")}</span><ArrowRight size={14} aria-hidden="true" />
              </button>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
