import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

type HomeTourState = { completed: boolean[]; complete: (step: number) => void; reset: () => void };
const HomeTourContext = createContext<HomeTourState | null>(null);

export function HomeTourProvider({ children }: { children: ReactNode }) {
  const [completed, setCompleted] = useState([false, false, false, false]);
  const complete = useCallback((step: number) => setCompleted(previous => previous[step] ? previous : previous.map((value, index) => index === step || value)), []);
  const reset = useCallback(() => setCompleted([false, false, false, false]), []);
  const value = useMemo(() => ({ completed, complete, reset }), [completed, complete, reset]);
  return <HomeTourContext.Provider value={value}>{children}</HomeTourContext.Provider>;
}

export function useHomeTour() {
  const context = useContext(HomeTourContext);
  if (!context) throw new Error("Home tour requires its page provider");
  return context;
}
