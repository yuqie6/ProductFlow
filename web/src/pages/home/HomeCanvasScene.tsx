import { Grip, Layers, RotateCcw } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useHomeTour } from "./HomeTourContext";
import { useI18n } from "../../lib/preferences";
import { WorkflowNodePresentationCard } from "../workbench/chrome/WorkflowNodeCard";
import { homeReference, homeShots } from "./HomeStudioDemo";

const nodes = [
  { kind: "product_source", key: "product", x: 24, y: 26 },
  { kind: "image_asset", key: "reference", x: 24, y: 202 },
  { kind: "visual_system", key: "style", x: 334, y: 26 },
  { kind: "creative_brief", key: "brief", x: 334, y: 202 },
  { kind: "image_prompt", key: "prompt", x: 644, y: 26 },
  { kind: "image_generation", key: "image", x: 644, y: 202 },
] as const;
const connections = [[0, 2], [0, 4], [2, 4], [3, 4], [4, 5], [1, 5]];

export default function HomeCanvasScene() {
  const { t } = useI18n();
  const { complete } = useHomeTour();
  const [selected, setSelected] = useState(4);
  const [shot, setShot] = useState(0);
  const [positions, setPositions] = useState<{ x: number; y: number }[]>(nodes.map(({ x, y }) => ({ x, y })));
  const [dragging, setDragging] = useState<number | null>(null);
  const [scale, setScale] = useState(1);
  const host = useRef<HTMLDivElement>(null);
  const drag = useRef<{ index: number; x: number; y: number; startX: number; startY: number } | null>(null);
  useEffect(() => {
    if (!host.current) return;
    const observer = new ResizeObserver(([entry]) => setScale(Math.max(.68, Math.min(1, entry.contentRect.width / 920))));
    observer.observe(host.current);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const canvas = host.current;
    if (!canvas || drag.current || canvas.scrollWidth <= canvas.clientWidth) return;
    canvas.scrollTo({ left: Math.max(0, (nodes[selected].x + 124) * scale - canvas.clientWidth / 2), behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth" });
  }, [selected, scale]);
  const title = (index: number) => t(nodes[index].key === "reference" ? "home.reference" : `home.story.${nodes[index].key}`);
  return (
    <div className="home-real-workbench">
      <header className="home-scene-toolbar"><span><Layers size={16} />{t("home.collection")}</span><span>{t("home.scene.sample")}</span></header>
      <div className="home-real-canvas" ref={host} aria-label={t("home.scene.canvasHint")}>
        <div style={{ width: 920 * scale, height: 454 * scale }}>
          <div className="home-node-plane" style={{ transform: `scale(${scale})` }}>
            <svg className="home-node-wires" viewBox="0 0 920 454" aria-hidden="true">
              {connections.map(([from, to]) => {
                const a = positions[from], b = positions[to];
                const x1 = a.x + 248, y1 = a.y + 65, x2 = b.x, y2 = b.y + 65;
                const sameColumn = from === 4 && to === 5;
                let d = sameColumn ? `M${a.x + 124},${a.y + 100} C${a.x + 124},${a.y + 160} ${b.x + 124},${b.y - 40} ${b.x + 124},${b.y}` : `M${x1},${y1} C${x1 + 40},${y1} ${x2 - 40},${y2} ${x2},${y2}`;
                if (from === 0 && to === 4) d = `M${x1},${y1} C${x1 + 30},${y1} ${x1 + 20},8 ${x1 + 60},8 L${x2 - 50},8 Q${x2 - 20},8 ${x2 - 20},${y2 - 24} Q${x2 - 20},${y2} ${x2},${y2}`;
                if (from === 1 && to === 5) d = `M${x1},${y1} C${x1 + 45},${y1} ${x1 + 10},444 ${x1 + 60},444 L${x2 - 60},444 C${x2 - 10},444 ${x2 - 45},${y2} ${x2},${y2}`;
                return <path key={`${from}-${to}`} d={d} data-active={selected === from || selected === to} />;
              })}
            </svg>
            {nodes.map((node, index) => {
              const src = index === 1 ? homeReference.src : index === 5 ? `/home-showcase/forma-${homeShots[shot].id}.webp` : null;
              return (
                <div key={node.kind} className="home-real-node" style={{ left: positions[index].x, top: positions[index].y }} data-demo-node={node.kind}
                  onPointerDown={(event) => {
                    if (event.button !== 0 || (event.target as HTMLElement).closest("a,button")) return;
                    drag.current = { index, x: event.clientX, y: event.clientY, startX: positions[index].x, startY: positions[index].y };
                    event.currentTarget.setPointerCapture(event.pointerId);
                    setSelected(index); setDragging(index); complete(1);
                  }}
                  onPointerMove={(event) => {
                    const current = drag.current;
                    if (!current || current.index !== index) return;
                    const next = { x: Math.max(8, Math.min(664, current.startX + (event.clientX - current.x) / scale)), y: Math.max(8, Math.min(index === 1 || index === 5 ? 210 : 315, current.startY + (event.clientY - current.y) / scale)) };
                    setPositions((previous) => previous.map((position, i) => i === index ? next : position));
                  }}
                  onPointerUp={() => { drag.current = null; setDragging(null); }}
                  onLostPointerCapture={() => { drag.current = null; setDragging(null); }}
                >
                  <WorkflowNodePresentationCard id={`home-${node.kind}`} kind={node.kind} title={index === 4 || index === 5 ? t(homeShots[shot].title) : title(index)} label={title(index)} status="idle" statusLabel={t("home.scene.sample")}
                    image={src ? { previewUrl: src, downloadUrl: src, filename: `example-${node.kind}.webp`, alt: title(index) } : null}
                    primarySelected={selected === index} dragging={dragging === index} onSelect={() => { setSelected(index); complete(1); }} />
                </div>
              );
            })}
          </div>
        </div>
      </div>
      <div className="home-scene-node-tabs" role="group" aria-label={t("home.scene.inspector")}>
        {nodes.map((node, index) => <button key={node.kind} type="button" aria-pressed={selected === index} onClick={() => { setSelected(index); complete(1); }}>{title(index)}</button>)}
      </div>
      <div className="home-real-inspector" key={`${selected}-${shot}`}>
        <div><span>{t("home.scene.inspector")}</span><h3>{title(selected)}</h3></div>
        <p>{selected === 0 ? t("home.collection") : selected === 1 ? t("home.demo.inputNote") : selected === 2 || selected === 3 ? t("home.demo.brief") : t(homeShots[shot].note)}</p>
      </div>
      <footer className="home-scene-footer"><span><Grip size={14} />{t("home.scene.drag")}</span><button type="button" onClick={() => setPositions(nodes.map(({ x, y }) => ({ x, y })))} aria-label={t("home.scene.resetPositions")}><RotateCcw size={15} /></button></footer>
      <div className="home-story-options" role="group" aria-label={t("home.demo.inspect")}>
        {homeShots.map((item, index) => <button type="button" key={item.id} aria-pressed={shot === index} onClick={() => { setShot(index); setSelected(5); }}>{t(item.title)}</button>)}
      </div>
    </div>
  );
}
