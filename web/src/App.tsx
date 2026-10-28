import { useCallback, useEffect, useState } from "react";
import { Center, Loader } from "@mantine/core";
import { api, SignedOut, type Device } from "./api";
import { Login } from "./Login";
import { Shell } from "./Shell";
import { Control } from "./Control";

export function App() {
  const page = () => {
    const hash = window.location.hash.slice(1);
    return hash === "control" || new URLSearchParams(hash).has("control") ? hash : "admin";
  };
  const [route, setRoute] = useState(page);
  useEffect(() => {
    const changed = () => setRoute(page());
    window.addEventListener("hashchange", changed);
    return () => window.removeEventListener("hashchange", changed);
  }, []);
  return route === "admin" ? <Admin /> : <Control key={route} />;
}

function Admin() {
  const [device, setDevice] = useState<Device | null>();
  const load = useCallback(() => {
    api.device().then(setDevice, (e) => {
      if (e instanceof SignedOut) setDevice(null);
    });
  }, []);
  useEffect(load, [load]);

  useEffect(() => {
    if (device) document.title = device.name;
  }, [device]);

  if (device === undefined) {
    return (
      <Center h="100vh">
        <Loader />
      </Center>
    );
  }
  if (device === null) return <Login onIn={load} />;
  return <Shell device={device} onOut={() => setDevice(null)} />;
}
