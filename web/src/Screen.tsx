import { useEffect, useRef, useState, type CSSProperties } from "react";

const screenEvery = 3000;

export function Screen() {
  const [src, setSrc] = useState<string>();
  const [aspect, setAspect] = useState(10 / 16);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => {
    let stopped = false;
    const take = () => {
      window.clearTimeout(timer.current);
      if (stopped || document.visibilityState !== "visible") return;
      const next = new Image();
      const url = `/api/v1/screen?t=${Date.now()}`;
      next.onload = () => {
        if (stopped) return;
        if (next.naturalWidth && next.naturalHeight)
          setAspect(next.naturalWidth / next.naturalHeight);
        setSrc(url);
        timer.current = window.setTimeout(take, screenEvery);
      };
      next.onerror = () => {
        if (!stopped) timer.current = window.setTimeout(take, screenEvery * 2);
      };
      next.src = url;
    };
    const shown = () => document.visibilityState === "visible" && take();
    take();
    document.addEventListener("visibilitychange", shown);
    return () => {
      stopped = true;
      window.clearTimeout(timer.current);
      document.removeEventListener("visibilitychange", shown);
    };
  }, []);

  return (
    <div className="ct-bezel" style={{ "--aspect": aspect } as CSSProperties}>
      {src ? <img src={src} alt="The panel right now" className="ct-screen" draggable={false} /> : <div className="ct-screen" />}
    </div>
  );
}
