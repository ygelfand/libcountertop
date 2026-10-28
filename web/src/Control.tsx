import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Center, Loader, Stack, Text, Title } from "@mantine/core";
import { api, SignedOut, type ControlSession } from "./api";
import { Desktop } from "./Desktop";
import { Screen } from "./Screen";
import { Keyboard } from "./Keyboard";

export function Control() {
  const [session, setSession] = useState<ControlSession | null>();
  const [issue, setIssue] = useState<Error>();
  const pending = useRef<Promise<ControlSession> | undefined>(undefined);

  const failed = useCallback((error: unknown) => {
    setIssue(error instanceof Error ? error : new Error("Connection failed"));
    setSession(null);
  }, []);

  const expired = useCallback(() => failed(new SignedOut()), [failed]);

  useEffect(() => {
    let stopped = false;
    if (!pending.current) {
      const code = new URLSearchParams(window.location.hash.slice(1)).get("control");
      const clean = new URL(window.location.href);
      clean.hash = "control";
      window.history.replaceState(window.history.state, "", clean.href);
      const signal = AbortSignal.timeout(10000);
      pending.current = code ? api.controlRedeem(code, signal) : api.controlSession(signal);
    }
    pending.current.then(
      (value) => { if (!stopped) setSession(value); },
      (error) => { if (!stopped) failed(error); },
    );
    return () => { stopped = true; };
  }, [failed]);

  useEffect(() => {
    if (!session) return;
    document.title = `${session.name} — ${session.target ? "Remote keyboard" : "Screen control"}`;
    const expires = Date.parse(session.expires_at);
    if (expires <= 0) return;
    const timer = window.setTimeout(expired, Math.max(0, expires - Date.now()));
    return () => window.clearTimeout(timer);
  }, [session, expired]);

  useEffect(() => {
    if (!session) return;
    let stopped = false;
    let request: AbortController | undefined;
    const check = async () => {
      if (request || document.visibilityState !== "visible") return;
      const controller = new AbortController();
      request = controller;
      const timeout = window.setTimeout(() => controller.abort(), 10000);
      try {
        await api.controlSession(controller.signal);
      } catch (error) {
        if (!stopped) failed(error);
      } finally {
        window.clearTimeout(timeout);
        request = undefined;
      }
    };
    const visible = () => { void check(); };
    const timer = window.setInterval(visible, 15000);
    document.addEventListener("visibilitychange", visible);
    window.addEventListener("online", visible);
    return () => {
      stopped = true;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", visible);
      window.removeEventListener("online", visible);
      request?.abort();
    };
  }, [session, failed]);

  const retry = async () => {
    setIssue(undefined);
    setSession(undefined);
    try {
      setSession(await api.controlSession(AbortSignal.timeout(10000)));
    } catch (error) {
      failed(error);
    }
  };

  const end = async () => {
    setIssue(undefined);
    setSession(null);
    await api.controlEnd().catch(() => {});
  };

  if (session === undefined) return <Center h="100dvh"><Loader /></Center>;
  if (session === null) return <Center h="100dvh" p="md"><Stack align="center">
    <Text role={issue ? "alert" : undefined}>{!issue || issue instanceof SignedOut
      ? "Scan a fresh code from the device web interface."
      : "Check the connection and try again."}</Text>
    {issue && !(issue instanceof SignedOut) && <Button onClick={() => void retry()}>Try again</Button>}
  </Stack></Center>;
  return <Center mih="100dvh" p="md">
    <Stack gap="md" align="center" style={{ width: "100%", maxWidth: 800 }}>
      <Title order={2}>{session.name}</Title>
      <Text size="sm" c="dimmed">{session.target ? "Remote keyboard" : "Screen control"}</Text>
      {session.target ? <Keyboard onAuthLost={expired} /> : <Desktop enabled onAuthLost={expired}><Screen /></Desktop>}
      <Button size="xs" variant="subtle" onClick={() => void end()}>End access</Button>
    </Stack>
  </Center>;
}
