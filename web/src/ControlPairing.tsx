import { useEffect, useRef, useState } from "react";
import { Button, Modal, Stack, Text } from "@mantine/core";
import { api, type ControlPair } from "./api";

export function ControlPairing() {
  const [opened, setOpened] = useState(false);
  const [pair, setPair] = useState<ControlPair>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [now, setNow] = useState(Date.now());
  const pending = useRef<Promise<ControlPair> | undefined>(undefined);

  useEffect(() => {
    if (!opened) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [opened]);

  const refresh = async () => {
    if (pending.current) return;
    setPair(undefined);
    setError("");
    setLoading(true);
    const base = new URL(window.location.href);
    base.hash = "";
    const request = api.controlPair(base.href);
    pending.current = request;
    try {
      setPair(await request);
      setNow(Date.now());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Try again");
    } finally {
      pending.current = undefined;
      setLoading(false);
    }
  };

  const remaining = pair ? Math.max(0, Math.ceil((Date.parse(pair.expires_at) - now) / 1000)) : 0;

  return <>
    <Button color="teal" variant="filled" onClick={() => { setOpened(true); void refresh(); }}>Control on another device</Button>
    <Modal opened={opened} onClose={() => setOpened(false)} title="Control on another device" centered>
      <Stack align="center">
        <Text size="sm">Scan to view and control this screen.</Text>
        {pair && remaining > 0 && <img src={pair.qr} alt="Screen control pairing code" width={280} height={280} style={{ maxWidth: "100%", height: "auto" }} />}
        {pair && <Text size="sm" c="dimmed">{remaining > 0 ? `Expires in ${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, "0")}` : "Code expired"}</Text>}
        {error && <Text size="sm" c="red" role="alert">{error}</Text>}
        <Button variant="light" loading={loading} onClick={() => void refresh()}>Refresh code</Button>
      </Stack>
    </Modal>
  </>;
}
