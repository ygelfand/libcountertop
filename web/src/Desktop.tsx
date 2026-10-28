import { useCallback, useEffect, useRef, useState, type CSSProperties, type PointerEvent, type ReactNode } from "react";
import { Button, Group, Stack, Text } from "@mantine/core";
import { api, SignedOut } from "./api";

const idleTimeout = 2 * 60 * 1000;

export function videoPoint(bounds: Pick<DOMRect, "left" | "top" | "width" | "height">, width: number, height: number, clientX: number, clientY: number) {
  if (bounds.width <= 0 || bounds.height <= 0 || width <= 0 || height <= 0) return;
  const scale = Math.min(bounds.width / width, bounds.height / height);
  const shownWidth = width * scale;
  const shownHeight = height * scale;
  return {
    x: (clientX - bounds.left - (bounds.width - shownWidth) / 2) / shownWidth,
    y: (clientY - bounds.top - (bounds.height - shownHeight) / 2) / shownHeight,
  };
}

type Connection = {
  pc: RTCPeerConnection;
  input: RTCDataChannel;
  abort: AbortController;
  id?: string;
  heartbeat?: number;
  timeout?: number;
  idle?: number;
  typing?: number;
  pointer?: { id: number; x: number; y: number };
};

async function gather(pc: RTCPeerConnection, signal: AbortSignal) {
  if (pc.iceGatheringState === "complete") return;
  await new Promise<void>((resolve, reject) => {
    const clean = () => {
      window.clearTimeout(timer);
      pc.removeEventListener("icegatheringstatechange", changed);
      signal.removeEventListener("abort", aborted);
    };
    const changed = () => {
      if (pc.iceGatheringState === "complete") { clean(); resolve(); }
    };
    const aborted = () => { clean(); reject(new Error("Connection cancelled")); };
    const timer = window.setTimeout(() => { clean(); reject(new Error("Network discovery timed out")); }, 5000);
    pc.addEventListener("icegatheringstatechange", changed);
    signal.addEventListener("abort", aborted, { once: true });
    if (signal.aborted) aborted();
  });
}

export function Desktop({ enabled, children, actions, onAuthLost }: { enabled: boolean; children: ReactNode; actions?: ReactNode; onAuthLost?: () => void }) {
  const connection = useRef<Connection | undefined>(undefined);
  const video = useRef<HTMLVideoElement | null>(null);
  const box = useRef<HTMLDivElement | null>(null);
  const keyboard = useRef<HTMLInputElement | null>(null);
  const [phase, setPhase] = useState<"idle" | "connecting" | "live">("idle");
  const [error, setError] = useState("");
  const [aspect, setAspect] = useState(16 / 10);
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    const changed = () => {
      setFullscreen(document.fullscreenElement === box.current);
      if (connection.current) keyboard.current?.focus({ preventScroll: true });
    };
    document.addEventListener("fullscreenchange", changed);
    return () => document.removeEventListener("fullscreenchange", changed);
  }, []);

  useEffect(() => {
    if (phase === "live") keyboard.current?.focus({ preventScroll: true });
  }, [phase]);

  const release = useCallback(() => {
    const c = connection.current;
    if (c?.pointer && c.input.readyState === "open") {
      try {
        c.input.send(JSON.stringify({ phase: "up", x: c.pointer.x, y: c.pointer.y }));
      } catch {}
      c.pointer = undefined;
    }
  }, []);

  const stop = useCallback(() => {
    const c = connection.current;
    if (!c) return;
    release();
    connection.current = undefined;
    c.abort.abort();
    window.clearInterval(c.heartbeat);
    window.clearTimeout(c.timeout);
    window.clearTimeout(c.idle);
    window.clearTimeout(c.typing);
    c.pc.close();
    if (c.id) void api.desktopStop(c.id).catch(() => {});
    if (video.current) video.current.srcObject = null;
    if (document.fullscreenElement === box.current) void document.exitFullscreen().catch(() => {});
    setPhase("idle");
  }, [release]);

  useEffect(() => {
    const hidden = () => { if (document.visibilityState !== "visible") stop(); };
    window.addEventListener("blur", release);
    window.addEventListener("pagehide", stop);
    document.addEventListener("visibilitychange", hidden);
    return () => {
      window.removeEventListener("blur", release);
      window.removeEventListener("pagehide", stop);
      document.removeEventListener("visibilitychange", hidden);
      stop();
    };
  }, [stop, release]);

  const start = async () => {
    if (connection.current) return;
    setError("");
    if (!window.RTCPeerConnection) { setError("WebRTC is unavailable in this browser"); return; }
    const pc = new RTCPeerConnection({ iceServers: [] });
    const c: Connection = { pc, input: pc.createDataChannel("input", { ordered: true }), abort: new AbortController() };
    connection.current = c;
    setPhase("connecting");
    const fail = (message: string) => {
      if (connection.current !== c) return;
      stop();
      setError(message);
    };
    const requestFailed = (e: unknown) => {
      if (connection.current !== c) return;
      if (e instanceof SignedOut && onAuthLost) {
        stop();
        onAuthLost();
        return;
      }
      fail(e instanceof Error ? e.message : "Connection failed");
    };
    c.timeout = window.setTimeout(() => fail("Connection timed out"), 15000);
    c.idle = window.setTimeout(stop, idleTimeout);
    pc.onconnectionstatechange = () => {
      if (connection.current !== c) return;
      if (["disconnected", "failed", "closed"].includes(pc.connectionState)) fail("Remote desktop disconnected");
    };
    c.input.onclose = () => fail("Remote desktop disconnected");
    c.input.onmessage = (event) => {
      if (connection.current !== c) return;
      try {
        const message = JSON.parse(event.data);
        if (typeof message.error === "string") setError(message.error);
      } catch { fail("Invalid input response"); }
    };
    pc.ontrack = (event) => {
      if (connection.current === c && video.current) video.current.srcObject = new MediaStream([event.track]);
    };
    try {
      const receiver = pc.addTransceiver("video", { direction: "recvonly" });
      const h264 = RTCRtpReceiver.getCapabilities("video")?.codecs.filter((codec) => codec.mimeType.toLowerCase() === "video/h264") ?? [];
      if (h264.length && receiver.setCodecPreferences) receiver.setCodecPreferences(h264);
      await pc.setLocalDescription(await pc.createOffer());
      await gather(pc, c.abort.signal);
      const answer = await api.desktopStart(pc.localDescription!, c.abort.signal);
      c.id = answer.id;
      if (connection.current !== c) { void api.desktopStop(answer.id).catch(() => {}); return; }
      await pc.setRemoteDescription(answer.answer);
      c.heartbeat = window.setInterval(() => {
        void api.desktopAlive(answer.id).catch(requestFailed);
      }, 10000);
    } catch (e) {
      requestFailed(e);
    }
  };

  const send = (event: { phase: "text"; text: string } | { phase: "key"; key: string }) => {
    const c = connection.current;
    if (!c || phase !== "live") return false;
    if (c.input.readyState !== "open") { setError("Wait for the input connection"); return false; }
    if (event.phase === "text" && new TextEncoder().encode(event.text).length > 16384) { setError("Send less text at once"); return false; }
    if (c.input.bufferedAmount > 65536) { stop(); setError("Input connection is busy"); return false; }
    try { c.input.send(JSON.stringify(event)); }
    catch { stop(); setError("Input connection failed"); return false; }
    setError("");
    window.clearTimeout(c.idle);
    c.idle = window.setTimeout(stop, idleTimeout);
    return true;
  };

  const flushText = () => {
    window.clearTimeout(connection.current?.typing);
    const input = keyboard.current;
    if (!input?.value) return;
    if (send({ phase: "text", text: input.value })) input.value = "";
  };

  const queueText = () => {
    const c = connection.current;
    if (!c) return;
    window.clearTimeout(c.typing);
    c.typing = window.setTimeout(flushText, 0);
  };

  const resized = () => {
    const v = video.current;
    if (v?.videoWidth && v.videoHeight) setAspect(v.videoWidth / v.videoHeight);
  };

  const toggleFullscreen = () => {
    if (document.fullscreenElement === box.current) void document.exitFullscreen().catch(() => {});
    else void box.current?.requestFullscreen().catch(() => {});
  };

  const pointer = (event: PointerEvent<HTMLVideoElement>, action: "down" | "move" | "up") => {
    const c = connection.current;
    if (!c || phase !== "live" || c.input.readyState !== "open" || !event.isPrimary) return;
    if (action === "down" && (c.pointer || event.button !== 0)) return;
    if (action !== "down" && c.pointer?.id !== event.pointerId) return;
    event.preventDefault();
    const bounds = event.currentTarget.getBoundingClientRect();
    const point = videoPoint(bounds, event.currentTarget.videoWidth, event.currentTarget.videoHeight, event.clientX, event.clientY);
    if (!point || (action === "down" && (point.x < 0 || point.x > 1 || point.y < 0 || point.y > 1))) return;
    const x = Math.max(0, Math.min(1, point.x));
    const y = Math.max(0, Math.min(1, point.y));
    if (action === "down") event.currentTarget.setPointerCapture(event.pointerId);
    if (action === "down" && event.pointerType === "mouse") keyboard.current?.focus({ preventScroll: true });
    c.pointer = action === "up" ? undefined : { id: event.pointerId, x, y };
    if (action === "move" && c.input.bufferedAmount > 4096) return;
    if (c.input.bufferedAmount > 65536) { stop(); return; }
    try {
      c.input.send(JSON.stringify({ phase: action, x, y }));
    } catch {
      stop();
      return;
    }
    window.clearTimeout(c.idle);
    c.idle = window.setTimeout(stop, idleTimeout);
    if (action === "up" && event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  };

  return (
    <Stack gap="xs" ref={box} className="ct-desktop">
      {phase === "idle" ? (
        <div className="ct-desktop-preview">
          {children}
          {enabled && <div className="ct-desktop-overlay">
            <Stack align="center" gap="sm">
              <Button variant="filled" onClick={() => void start()}>Take control</Button>
              {actions}
            </Stack>
          </div>}
        </div>
      ) : (
        <div className="ct-bezel" style={{ "--aspect": aspect } as CSSProperties}>
          <video
            ref={video}
            className="ct-screen"
            autoPlay muted playsInline
            draggable={false}
            aria-label="Remote device screen"
            style={{ touchAction: "none", cursor: phase === "live" ? "pointer" : "wait" }}
            onLoadedMetadata={resized}
            onResize={resized}
            onPlaying={() => {
              if (!connection.current) return;
              resized();
              window.clearTimeout(connection.current?.timeout);
              setPhase("live");
            }}
            onPointerDown={(e) => pointer(e, "down")}
            onPointerMove={(e) => pointer(e, "move")}
            onPointerUp={(e) => pointer(e, "up")}
            onPointerCancel={release}
            onLostPointerCapture={release}
            onContextMenu={(e) => e.preventDefault()}
            onDragStart={(e) => e.preventDefault()}
          />
        </div>
      )}
      {enabled && phase !== "idle" && <Group justify="center" gap="xs" className="ct-desktop-controls">
        <Button size="xs" variant="light" onClick={stop}>Release control</Button>
        {phase === "live" && <Button size="xs" variant="subtle" onClick={toggleFullscreen}>{fullscreen ? "Exit fullscreen" : "Fullscreen"}</Button>}
      </Group>}
      {phase === "live" && <input
        ref={keyboard}
        className="ct-desktop-keyboard"
        aria-label="Remote keyboard"
        tabIndex={-1}
        inputMode="none"
        autoComplete="off" autoCapitalize="off" spellCheck={false}
        onInput={(event) => {
          if ((event.nativeEvent as InputEvent).isComposing) return;
          queueText();
        }}
        onCompositionEnd={queueText}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing || event.keyCode === 229) return;
          if (event.key === "Escape" && document.fullscreenElement) return;
          if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === "a") {
            event.preventDefault();
            flushText();
            send({ phase: "key", key: "SelectAll" });
            return;
          }
          if (event.ctrlKey || event.metaKey || event.altKey) return;
          if (["Backspace", "Delete", "Enter", "Escape", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"].includes(event.key)) {
            event.preventDefault();
            flushText();
            send({ phase: "key", key: event.key });
          }
        }}
      />}
      {phase === "connecting" && <Text className="ct-desktop-status" size="xs" ta="center" c="dimmed">Connecting…</Text>}
      {error && <Text className="ct-desktop-status" size="xs" ta="center" c="red" role="alert">{error}</Text>}
    </Stack>
  );
}
