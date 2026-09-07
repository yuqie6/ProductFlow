import { useEffect, type RefObject } from "react";

// A damped spring follows the pointer and settles after release. Work is only
// scheduled while the spring is moving; there is no idle animation loop.
export function useHomeMotion(root: RefObject<HTMLDivElement | null>) {
  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    const finePointer = window.matchMedia("(pointer: fine)");
    let active: HTMLElement | null = null;
    let frame = 0;
    let last = 0;
    const point = { x: 0, y: 0, vx: 0, vy: 0, tx: 0, ty: 0 };
    const tick = (time: number) => {
      const dt = Math.min((time - (last || time - 16)) / 1000, .032);
      last = time;
      point.vx += ((point.tx - point.x) * 180 - point.vx * 19) * dt;
      point.vy += ((point.ty - point.y) * 180 - point.vy * 19) * dt;
      point.x += point.vx * dt; point.y += point.vy * dt;
      active?.style.setProperty("--pointer-x", `${point.x}`);
      active?.style.setProperty("--pointer-y", `${point.y}`);
      if (Math.abs(point.x - point.tx) + Math.abs(point.y - point.ty) + Math.abs(point.vx) + Math.abs(point.vy) > .015) frame = requestAnimationFrame(tick);
      else { frame = 0; last = 0; }
    };
    const start = () => { if (!frame) frame = requestAnimationFrame(tick); };
    const move = (event: PointerEvent) => {
      if (preference.matches || !finePointer.matches) return;
      const target = (event.target as Element).closest<HTMLElement>(".home-photo-button, .home-guide-launch, .home-edit-illustration");
      if (active !== target) {
        active?.style.removeProperty("--pointer-x"); active?.style.removeProperty("--pointer-y");
        Object.assign(point, { x: 0, y: 0, vx: 0, vy: 0 });
        active = target;
      }
      if (!active) return;
      const bounds = active.getBoundingClientRect();
      point.tx = Math.max(-1, Math.min(1, (event.clientX - bounds.left) / bounds.width * 2 - 1));
      point.ty = Math.max(-1, Math.min(1, (event.clientY - bounds.top) / bounds.height * 2 - 1));
      start();
    };
    const leave = () => { point.tx = 0; point.ty = 0; start(); };
    const reset = () => {
      cancelAnimationFrame(frame); frame = 0; last = 0;
      active?.style.removeProperty("--pointer-x"); active?.style.removeProperty("--pointer-y");
      active = null; Object.assign(point, { x: 0, y: 0, vx: 0, vy: 0, tx: 0, ty: 0 });
    };
    element.addEventListener("pointermove", move, { passive: true });
    element.addEventListener("pointerleave", leave);
    preference.addEventListener("change", reset);
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) if (entry.isIntersecting) {
        entry.target.classList.add("home-arrived"); observer.unobserve(entry.target);
      }
    }, { threshold: .12 });
    element.querySelectorAll(".home-story-section").forEach((section) => observer.observe(section));
    return () => {
      reset(); observer.disconnect();
      element.removeEventListener("pointermove", move); element.removeEventListener("pointerleave", leave);
      preference.removeEventListener("change", reset);
    };
  }, [root]);
}
