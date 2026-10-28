export type Choice = { value: string; label: string };

export type Field = {
  id: string;
  name: string;
  title: string;
  group: string;
  kind: "toggle" | "number" | "choice" | "pair" | "triple" | "list" | "text" | "secret" | "credential";
  value: string;
  example?: string;
  unit?: string;
  min?: number;
  max?: number;
  choices?: Choice[];
};

export type Domain = { domain: string; section_key: string; section: string; fields: Field[] };

export type Device = {
  id: string;
  name: string;
  model: string;
  board: string;
  version: string;
  pages: string[] | null;
  screen: boolean;
};

export type ControlPair = { url: string; qr: string; expires_at: string };
export type ControlSession = { name: string; expires_at: string; target?: string };
export type KeyboardField = { target: string; text: string; password: boolean; revision: number };

export type ActionInfo = {
  name: string;
  args: { name: string; type: "string" | "int" | "float" | "bool" }[];
  answers: boolean;
};

export type Peer = { id: string; name: string; model: string; board: string; video: boolean };

export type CallNow = {
  id: string;
  peer: Peer;
  state: "idle" | "calling" | "ringing" | "talking";
  since: string;
  video: boolean;
  muted: boolean;
  camera_off: boolean;
  remote_muted: boolean;
  remote_camera_off: boolean;
  reconnecting: boolean;
  browser: boolean;
};

export type CallView = { call: CallNow | null; peers: Peer[]; video: boolean };

export type CallSignal = { type: "answer"; sdp: string } | { type: "end"; reason?: string };

export type Alarm = {
  id: string;
  name: string;
  time: string;
  days: string[];
  enabled: boolean;
  sound: string;
  snooze_minutes: number;
  volume: string;
  ramp_seconds: number;
  light_minutes: number;
};

export type Alarms = {
  alarms: Alarm[] | null;
  ringing?: string;
  next?: string;
  next_at?: string;
  snoozed?: Record<string, number>;
  sounds: { value: string; label: string }[] | null;
  volumes: string[];
  snooze_most: number;
  ramp_least: number;
  ramp_most: number;
  light_most: number;
};

export type State = { id: string; state: Record<string, unknown> };

export type Description = {
  id: string;
  kind: string;
  info: {
    name?: string;
    icon?: string;
    unit_of_measurement?: string;
    accuracy_decimals?: number;
    entity_category?: string;
    disabled_by_default?: boolean;
  };
};

type Event = { type: "states"; states: State[] } | { type: "state"; state: State };

export class SignedOut extends Error {}
export class HTTPError extends Error {
  constructor(public status: number, message: string) { super(message); }
}

const base = "/api/v1";

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(base + path, { credentials: "same-origin", ...init });
  if (r.status === 401) throw new SignedOut();
  if (!r.ok) throw new HTTPError(r.status, (await r.text()).trim() || r.statusText);
  if (r.status === 204) return undefined as T;
  return r.json() as Promise<T>;
}

function json(body: unknown): RequestInit {
  return { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
}

function post(path: string, body: unknown = {}) {
  return call<void>(path, json(body));
}

export const api = {
  controlPair: (url: string) => call<ControlPair>("/control/pairing", json({ url })),
  controlRedeem: (code: string, signal?: AbortSignal) => call<ControlSession>("/control/pairing/redeem", { ...json({ code }), signal }),
  controlSession: (signal?: AbortSignal) => call<ControlSession>("/control/session", { signal }),
  controlEnd: () => call<void>("/control/session", { method: "DELETE" }),
  keyboard: (signal?: AbortSignal) => call<KeyboardField>("/control/keyboard", { signal }),
  keyboardEdit: (field: KeyboardField, action: "replace" | "save" | "cancel", text = field.text) =>
    call<KeyboardField>("/control/keyboard", { ...json({ target: field.target, revision: field.revision, action, text }), signal: AbortSignal.timeout(10000) }),
  desktopStart: (offer: RTCSessionDescriptionInit, signal: AbortSignal) =>
    call<{ id: string; answer: RTCSessionDescriptionInit }>("/desktop", { ...json(offer), signal }),
  desktopAlive: (id: string) => post(`/desktop/${id}`),
  desktopStop: (id: string) => call<void>(`/desktop/${id}`, { method: "DELETE", keepalive: true }),
  device: () => call<Device>("/device"),
  name: () => call<{ name: string }>("/name").then((r) => r.name),
  settings: () => call<Domain[] | null>("/settings").then((d) => d ?? []),
  entities: () => call<Description[] | null>("/entities").then((d) => d ?? []),
  field: (domain: string, name: string) => call<Field>(`/settings/${domain}/${name}`),
  change: (domain: string, name: string, value: string) =>
    call<Field>(`/settings/${domain}/${name}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ value }),
    }),
  actions: () => call<ActionInfo[]>("/actions"),
  runAction: (name: string, args: Record<string, unknown>) =>
    call<{ answer: unknown }>(`/actions/${name}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ args }),
    }),
  callView: () => call<CallView>("/call/"),
  dial: (peer: string, video: boolean) => post("/call/dial", { peer, video }),
  answer: () => post("/call/answer"),
  hangup: () => post("/call/hangup"),
  muteCall: (on: boolean) => post("/call/mute", { on }),
  cameraCall: (on: boolean) => post("/call/camera", { on }),
  offer: (o: { sdp: string; video: boolean; width: number; height: number }) =>
    call<{ session?: string; status: string }>("/call/offer", json(o)),
  signal: (session: string) => call<CallSignal | undefined>(`/call/signal/${session}`),
  endCall: (session: string) => post(`/call/end/${session}`),
  recordBroadcast: () => post("/broadcast/record"),
  press: (id: string) => call<void>(`/buttons/${id}`, { method: "POST" }),
  alarms: () => call<Alarms>("/alarms/"),
  saveAlarm: (a: Alarm) =>
    call<Alarm>(a.id ? `/alarms/${a.id}` : "/alarms/", {
      method: a.id ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(a),
    }),
  enableAlarm: (id: string, enabled: boolean) =>
    call<void>(`/alarms/${id}/enable`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled }),
    }),
  deleteAlarm: (id: string) => call<void>(`/alarms/${id}`, { method: "DELETE" }),
  stopAlarm: () => call<void>("/alarms/stop", { method: "POST" }),
  snoozeAlarm: () => call<void>("/alarms/snooze", { method: "POST" }),
  login: async (password: string) => {
    const r = await fetch(base + "/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    });
    if (r.status === 429) throw new Error("Try again in a few minutes");
    if (!r.ok) throw new Error("Wrong password");
  },
  logout: () => call<void>("/logout", { method: "POST" }),
};

export function follow(onState: (s: State, snapshot: boolean) => void, onLive: (live: boolean) => void): () => void {
  let socket: WebSocket | undefined;
  let retry: number | undefined;
  let stopped = false;
  const open = () => {
    const scheme = location.protocol === "https:" ? "wss" : "ws";
    socket = new WebSocket(`${scheme}://${location.host}${base}/events`);
    socket.onopen = () => onLive(true);
    socket.onmessage = (m) => {
      const e = JSON.parse(m.data) as Event;
      if (e.type === "states") e.states.forEach((s) => onState(s, true));
      else onState(e.state, false);
    };
    socket.onclose = () => {
      onLive(false);
      if (!stopped) retry = window.setTimeout(open, 3000);
    };
  };
  open();
  return () => {
    stopped = true;
    window.clearTimeout(retry);
    socket?.close();
  };
}
