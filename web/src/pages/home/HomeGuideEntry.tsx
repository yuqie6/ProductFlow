import { ArrowRight } from "lucide-react";
import { useRef, useState } from "react";

import { useI18n } from "../../lib/preferences";
import { useHomeTour } from "./HomeTourContext";
import { HomeGuide } from "./HomeGuide";
import { StudioCamera } from "./StudioCamera";

export function HomeGuideEntry() {
  const { t } = useI18n();
  const { reset } = useHomeTour();
  const camera = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  return (
    <div className="home-guide-entry" data-guiding={open}>
      <div className="home-guide-greeting"><span className="home-guide-greeting-dot" aria-hidden="true" /><p>{t("home.guide.greeting")}</p></div>
      <button type="button" className="home-guide-launch" onClick={() => { reset(); setOpen(true); }} aria-label={t("home.guide.start")} aria-expanded={open}>
        <span className="home-guide-entry-camera" ref={camera}><StudioCamera step={0} /></span>
        <span className="home-guide-entry-action">{t("home.guide.start")}<ArrowRight size={17} aria-hidden="true" /></span>
      </button>
      <p className="home-guide-entry-note">{t("home.guide.invitation")}</p>
      <HomeGuide origin={camera} open={open} onClose={() => setOpen(false)} />
    </div>
  );
}
