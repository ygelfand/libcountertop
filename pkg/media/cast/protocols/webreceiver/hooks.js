(() => {
  const values = __VALUES__;
  const port = __PORT__;
  const base = "http://127.0.0.1:" + port;

  const answers = new Map();
  const ask = (type, how) => {
    type = String(type);
    const key = how + ":" + type;
    if (answers.has(key)) return answers.get(key);
    let ok = false;
    try {
      const x = new XMLHttpRequest();
      x.open("GET", base + "/plays?type=" + encodeURIComponent(type) + (how ? "&" + how + "=1" : ""), false);
      x.send();
      ok = x.responseText === "true";
    } catch (e) {
      ok = false;
    }
    answers.set(key, ok);
    return ok;
  };
  const plays = (type) => ask(type, "");
  const feeds = (type) => ask(type, "feed");
  const seals = (type) => ask(type, "sealed");

  window.cast = window.cast || {};
  window.cast.__platform__ = {
    queryPlatformValue(k) { return values[k]; },
    canDisplayType(t) { return plays(t); },
    setTouchInputSupport() { return Promise.resolve(); },
    display: { getHdcpVersion: () => Promise.resolve("1.4"), updateOutputMode: () => Promise.resolve() },
    metrics: { logEventToUma() {}, logBoolToUma() {}, logIntToUma() {}, logHistogramValueToUma() {} },
  };

  const proto = HTMLMediaElement.prototype;
  const vproto = HTMLVideoElement.prototype;
  const desc = (p, k) => Object.getOwnPropertyDescriptor(p, k);
  proto.canPlayType = function (t) { return plays(t) ? "probably" : ""; };

  const states = new WeakMap();
  const lanes = new Map();
  const sources = new Map();
  let link = null;
  let owner = null;
  const queue = [];
  const send = (m) => {
    if (link && link.readyState === 1) link.send(m);
    else queue.push(m);
  };
  const order = (m) => send(JSON.stringify(m));
  const append = (m, bytes) => {
    const head = new TextEncoder().encode(JSON.stringify(m));
    const out = new Uint8Array(4 + head.length + bytes.length);
    new DataView(out.buffer).setUint32(0, head.length);
    out.set(head, 4);
    out.set(bytes, 4 + head.length);
    send(out);
  };
  const connect = () => {
    link = new WebSocket("ws://127.0.0.1:" + port + "/media");
    link.binaryType = "arraybuffer";
    link.onopen = () => { for (const m of queue.splice(0)) link.send(m); };
    link.onmessage = (e) => {
      const r = JSON.parse(e.data);
      if (r.ev === "done") {
        const b = lanes.get(r.lane);
        if (b) b.done(r);
        return;
      }
      if (r.ev === "keydone") {
        const settle = keyCalls.get(r.seq);
        keyCalls.delete(r.seq);
        if (settle) settle(r);
        return;
      }
      if (r.ev === "encrypted") {
        if (owner) encrypted(owner, fromBase64(r.data));
        return;
      }
      if (owner) report(owner, r);
    };
    link.onclose = () => setTimeout(connect, 1000);
  };
  connect();

  window.close = () => order({ op: "close" });

  const lounge = (url, body) => {
    if (typeof body !== "string" || !String(url).includes("/api/lounge/bc/bind")) return;
    const form = new URLSearchParams(body);
    for (let i = 0; form.has("req" + i + "__sc"); i++) {
      const sc = form.get("req" + i + "__sc");
      const prefix = "req" + i + "_";
      const fields = {};
      for (const [k, v] of form) if (k.startsWith(prefix) && k !== prefix + "_sc") fields[k.slice(prefix.length)] = v;
      order({ op: "lounge", name: sc, fields });
    }
  };
  const xopen = XMLHttpRequest.prototype.open;
  const xsend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function (method, url, ...rest) {
    this.__url = url;
    return xopen.call(this, method, url, ...rest);
  };
  XMLHttpRequest.prototype.send = function (body) {
    lounge(this.__url, body);
    return xsend.call(this, body);
  };
  const xfetch = window.fetch;
  window.fetch = function (input, init) {
    lounge(typeof input === "string" ? input : input && input.url, init && init.body);
    return xfetch.apply(this, arguments);
  };

  const where = () => order({ op: "page", url: location.href });
  addEventListener("hashchange", where);
  addEventListener("popstate", where);
  for (const k of ["pushState", "replaceState"]) {
    const real = history[k];
    history[k] = function (...a) {
      const out = real.apply(this, a);
      where();
      return out;
    };
  }
  where();

  if (navigator.mediaSession) {
    let shown = null;
    Object.defineProperty(navigator.mediaSession, "metadata", {
      configurable: true,
      get() { return shown; },
      set(v) {
        shown = v;
        if (!v) return;
        const art = v.artwork && v.artwork.length ? v.artwork[v.artwork.length - 1].src : "";
        order({ op: "describe", title: v.title || "", artist: v.artist || "", album: v.album || "", url: art });
      },
    });
  }

  const fire = (el, ...names) => { for (const n of names) el.dispatchEvent(new Event(n)); };
  const now = (s) => (s.paused || s.waiting || s.starting ? s.t : s.t + ((performance.now() - s.at) / 1000) * s.rate);
  const mark = (s, t) => { s.t = t; s.at = performance.now(); };
  const timeRanges = (list) => ({ length: list.length, start: (i) => list[i][0], end: (i) => list[i][1] });
  const whole = (s) => {
    const d = s.ms ? s.ms.dur : s.duration;
    return isFinite(d) && d > 0 ? [[0, d]] : [];
  };
  const shared = (s) => {
    let out = null;
    for (const b of s.ms.list) {
      if (out === null) { out = b.ranges.map((r) => r.slice()); continue; }
      const next = [];
      for (const [a0, a1] of out) {
        for (const [b0, b1] of b.ranges) {
          const lo = Math.max(a0, b0), hi = Math.min(a1, b1);
          if (lo < hi) next.push([lo, hi]);
        }
      }
      out = next;
    }
    return out || [];
  };

  class Emitter extends EventTarget {
    emit(name) { this.emitEvent(new Event(name)); }
    emitEvent(e) {
      const h = this["on" + e.type];
      if (typeof h === "function") h.call(this, e);
      this.dispatchEvent(e);
    }
  }

  const fromBase64 = (s) => Uint8Array.from(atob(s || ""), (c) => c.charCodeAt(0));
  const bytesOf = (d) => (d instanceof ArrayBuffer
    ? new Uint8Array(d)
    : new Uint8Array(d.buffer, d.byteOffset, d.byteLength));
  const hex = (u) => Array.from(u, (b) => b.toString(16).padStart(2, "0")).join("");
  const event = (name, fields) => {
    const e = new Event(name);
    for (const [k, v] of Object.entries(fields)) Object.defineProperty(e, k, { value: v });
    return e;
  };
  const encrypted = (el, data) => {
    const e = event("encrypted", { initDataType: "cenc", initData: data.buffer });
    if (typeof el.onencrypted === "function") el.onencrypted(e);
    el.dispatchEvent(e);
  };

  const keyCalls = new Map();
  let keyCount = 0;
  const keyCall = (m, data) => new Promise((resolve) => {
    const seq = ++keyCount;
    keyCalls.set(seq, resolve);
    append(Object.assign({ seq }, m), data || new Uint8Array(0));
  });
  let keysHere = null;
  const keysAvailable = () => {
    if (keysHere !== null) return keysHere;
    try {
      const x = new XMLHttpRequest();
      x.open("GET", base + "/keys", false);
      x.send();
      keysHere = x.responseText === "true";
    } catch (e) {
      keysHere = false;
    }
    return keysHere;
  };
  const widevine = "com.widevine.alpha";
  const initTypes = ["cenc", "keyids", "webm"];

  class FedKeyStatusMap {
    constructor() { this.m = new Map(); }
    get size() { return this.m.size; }
    set(id, status) { this.m.set(hex(id), { id: id.slice().buffer, status }); }
    get(k) { const v = this.m.get(hex(bytesOf(k))); return v && v.status; }
    has(k) { return this.m.has(hex(bytesOf(k))); }
    forEach(cb, self) { for (const v of this.m.values()) cb.call(self, v.status, v.id, this); }
    entries() { return Array.from(this.m.values(), (v) => [v.id, v.status])[Symbol.iterator](); }
    keys() { return Array.from(this.m.values(), (v) => v.id)[Symbol.iterator](); }
    values() { return Array.from(this.m.values(), (v) => v.status)[Symbol.iterator](); }
    [Symbol.iterator]() { return this.entries(); }
  }

  let sessionCount = 0;
  class FedKeySession extends Emitter {
    constructor(type) {
      super();
      this.sessionType = type || "temporary";
      this.sessionId = "";
      this.expiration = NaN;
      this.keyStatuses = new FedKeyStatusMap();
      this.closed = new Promise((r) => { this.finish = r; });
      this.kids = [];
    }
    generateRequest(type, data) {
      if (!initTypes.includes(type)) return Promise.reject(new DOMException(type, "NotSupportedError"));
      this.sessionId = "fed-" + ++sessionCount;
      return keyCall({ op: "request", session: this.sessionId, init: type }, bytesOf(data)).then((r) => {
        if (r.message) throw new DOMException(r.message, "NotSupportedError");
        this.kids = (r.kids || []).map(fromBase64);
        const message = fromBase64(r.data).buffer;
        setTimeout(() => this.emitEvent(event("message", { messageType: "license-request", message })), 0);
      });
    }
    update(response) {
      return keyCall({ op: "update", session: this.sessionId }, bytesOf(response)).then((r) => {
        if (r.message) throw new TypeError(r.message);
        const kids = (r.kids || []).map(fromBase64);
        for (const kid of kids.length ? kids : this.kids) this.keyStatuses.set(kid, "usable");
        setTimeout(() => this.emit("keystatuseschange"), 0);
      });
    }
    load() { return Promise.resolve(false); }
    remove() { return Promise.resolve(); }
    close() {
      if (this.sessionId) order({ op: "release", session: this.sessionId });
      this.finish("closed-by-application");
      return Promise.resolve();
    }
  }

  class FedMediaKeys {
    createSession(type) { return new FedKeySession(type); }
    setServerCertificate(cert) { return keyCall({ op: "cert" }, bytesOf(cert)).then(() => true); }
    getStatusForPolicy() { return Promise.resolve("usable"); }
  }

  class FedKeySystemAccess {
    constructor(config) { this.keySystem = widevine; this.config = config; }
    getConfiguration() { return this.config; }
    createMediaKeys() { return Promise.resolve(new FedMediaKeys()); }
  }

  const choose = (configs) => {
    for (const c of configs || []) {
      const pick = (caps) => (caps || []).filter((x) => seals(x.contentType));
      const video = pick(c.videoCapabilities), audio = pick(c.audioCapabilities);
      if ((c.videoCapabilities || []).length && !video.length) continue;
      if ((c.audioCapabilities || []).length && !audio.length) continue;
      const types = (c.initDataTypes || ["cenc"]).filter((t) => initTypes.includes(t));
      if (!types.length) continue;
      return Object.assign({}, c, {
        initDataTypes: types,
        videoCapabilities: video,
        audioCapabilities: audio,
        distinctiveIdentifier: c.distinctiveIdentifier === "required" ? "required" : "not-allowed",
        persistentState: c.persistentState === "required" ? "required" : "not-allowed",
        sessionTypes: ["temporary"],
      });
    }
    return null;
  };
  navigator.requestMediaKeySystemAccess = (system, configs) => {
    const config = system === widevine && keysAvailable() ? choose(configs) : null;
    if (!config) return Promise.reject(new DOMException(system, "NotSupportedError"));
    return Promise.resolve(new FedKeySystemAccess(config));
  };
  window.MediaKeySystemAccess = FedKeySystemAccess;
  window.MediaKeys = FedMediaKeys;
  window.MediaKeySession = FedKeySession;
  delete window.WebKitMediaKeys;

  let laneCount = 0;
  let seqCount = 0;

  class FedSourceBuffer extends Emitter {
    constructor(ms, type) {
      super();
      this.ms = ms;
      this.type = type;
      this.id = ++laneCount;
      this.mode = "segments";
      this.updating = false;
      this.timestampOffset = 0;
      this.appendWindowStart = 0;
      this.appendWindowEnd = Infinity;
      this.ranges = [];
      this.seq = 0;
      this.audioTracks = [];
      this.videoTracks = [];
      this.textTracks = [];
      lanes.set(this.id, this);
      order({ op: "add", lane: this.id, type });
    }
    get buffered() { return timeRanges(this.ranges); }
    begin() {
      if (this.updating || this.ms.readyState === "closed") throw new DOMException("busy", "InvalidStateError");
      if (this.ms.readyState === "ended") { this.ms.readyState = "open"; this.ms.emit("sourceopen"); }
      this.updating = true;
      this.seq = ++seqCount;
      const seq = this.seq;
      Promise.resolve().then(() => { if (this.seq === seq) this.emit("updatestart"); });
      return seq;
    }
    appendBuffer(data) {
      const bytes = data instanceof ArrayBuffer
        ? new Uint8Array(data)
        : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
      const seq = this.begin();
      append({ op: "append", lane: this.id, seq, offset: this.timestampOffset }, bytes);
    }
    remove(start, end) {
      const seq = this.begin();
      order({ op: "remove", lane: this.id, seq, start, until: isFinite(end) ? end : 1e9 });
    }
    abort() {
      order({ op: "abort", lane: this.id });
      if (!this.updating) return;
      this.updating = false;
      this.seq = 0;
      this.emit("abort");
      this.emit("updateend");
    }
    changeType(type) { this.type = type; }
    done(r) {
      if (r.seq !== this.seq || !this.updating) return;
      this.ranges = r.buffered || [];
      this.updating = false;
      if (r.message) {
        this.emit("error");
        this.emit("updateend");
        this.ms.endOfStream("decode");
        return;
      }
      this.emit("update");
      this.emit("updateend");
    }
  }

  class FedMediaSource extends Emitter {
    constructor() {
      super();
      this.readyState = "closed";
      this.list = [];
      this.dur = NaN;
      this.el = null;
    }
    static isTypeSupported(t) { return feeds(t); }
    get sourceBuffers() { return this.list; }
    get activeSourceBuffers() { return this.list; }
    get duration() { return this.dur; }
    set duration(v) {
      this.dur = Number(v);
      order({ op: "duration", t: isFinite(this.dur) ? this.dur : 0 });
      if (this.el) fire(this.el, "durationchange");
    }
    addSourceBuffer(type) {
      if (!feeds(type)) throw new DOMException(type, "NotSupportedError");
      if (this.readyState !== "open") throw new DOMException("closed", "InvalidStateError");
      const b = new FedSourceBuffer(this, type);
      this.list.push(b);
      return b;
    }
    removeSourceBuffer(b) {
      const i = this.list.indexOf(b);
      if (i < 0) throw new DOMException("unknown", "NotFoundError");
      this.list.splice(i, 1);
      lanes.delete(b.id);
      order({ op: "drop", lane: b.id });
    }
    endOfStream(err) {
      if (this.readyState !== "open") return;
      this.readyState = "ended";
      if (!err) order({ op: "end" });
      this.emit("sourceended");
    }
    setLiveSeekableRange(start, end) {
      this.live = [start, end];
      order({ op: "window", start, t: end });
    }
    clearLiveSeekableRange() {
      this.live = null;
      order({ op: "window" });
    }
    attach(el) {
      this.el = el;
      this.readyState = "open";
      setTimeout(() => { if (this.el === el) this.emit("sourceopen"); }, 0);
    }
    detach() {
      for (const b of this.list) lanes.delete(b.id);
      this.el = null;
      this.readyState = "closed";
      this.emit("sourceclose");
    }
  }
  FedMediaSource.canConstructInDedicatedWorker = false;
  window.MediaSource = FedMediaSource;
  window.SourceBuffer = FedSourceBuffer;
  delete window.ManagedMediaSource;
  delete window.WebKitMediaSource;

  let blobCount = 0;
  const realCreate = URL.createObjectURL;
  URL.createObjectURL = function (o) {
    if (!(o instanceof FedMediaSource)) return realCreate.call(URL, o);
    const u = "blob:" + location.origin + "/fed-" + ++blobCount + "-" + Date.now();
    sources.set(u, o);
    return u;
  };
  const realRevoke = URL.revokeObjectURL;
  URL.revokeObjectURL = function (u) {
    if (sources.delete(u)) return;
    return realRevoke.call(URL, u);
  };

  const announced = () => {
    try {
      const info = cast.framework.CastReceiverContext.getInstance().getPlayerManager().getMediaInformation();
      return info && info.duration > 0 ? info.duration : 0;
    } catch (e) {
      return 0;
    }
  };

  const report = (el, r) => {
    const s = states.get(el);
    if (!s) return;
    if (typeof r.t === "number" && !s.seeking) mark(s, r.t);
    if (typeof r.frames === "number") s.frames = r.frames;
    switch (r.ev) {
      case "meta":
        if (!s.ms) s.duration = r.duration > 0 ? r.duration : announced() || Infinity;
        s.w = r.w || 0; s.h = r.h || 0; s.ready = 4;
        fire(el, "durationchange", "loadedmetadata", "loadeddata", "canplay", "canplaythrough");
        if (s.pending !== null) { order({ op: "seek", t: s.pending }); s.pending = null; }
        if (el.autoplay && s.paused) el.play();
        break;
      case "time":
        if (!s.paused && !s.waiting && !s.starting) fire(el, "timeupdate");
        break;
      case "playing":
        if (s.paused) { s.paused = false; fire(el, "play"); }
        s.waiting = false; s.starting = false; fire(el, "playing");
        break;
      case "pause":
        if (!s.paused) { s.paused = true; fire(el, "pause"); }
        break;
      case "seeked":
        if (s.seeking) { s.seeking = false; fire(el, "seeked"); }
        fire(el, "timeupdate");
        if (s.paused) break;
        if (s.ms && shared(s).some(([a, b]) => s.t >= a && s.t < b)) { s.starting = true; break; }
        s.waiting = true; fire(el, "waiting");
        break;
      case "waiting":
        s.waiting = true;
        fire(el, "timeupdate", "waiting");
        break;
      case "ended":
        s.paused = true; s.ended = true;
        fire(el, "timeupdate", "pause", "ended");
        break;
      case "error":
        s.error = { code: 4, message: r.message || "" };
        fire(el, "error");
        break;
    }
  };

  const fresh = (url) => ({ url, t: 0, at: performance.now(), rate: 1, duration: NaN, paused: true, ended: false,
    ready: 0, seeking: false, waiting: false, starting: false, volume: 1, muted: false, error: null, w: 0, h: 0, pending: null, ms: null,
    frames: 0 });
  const take = (el, s) => {
    const had = states.has(el);
    if (had) stop(el);
    states.set(el, s);
    owner = el;
    if (had) fire(el, "loadstart"); else fire(el, "emptied", "loadstart");
  };
  const begin = (el, url) => {
    take(el, fresh(url));
    order({ op: "load", url, start: 0 });
  };
  const feed = (el, url, ms) => {
    const s = fresh(url);
    s.ms = ms;
    take(el, s);
    order({ op: "source", start: 0 });
    ms.attach(el);
  };
  const stop = (el) => {
    const s = states.get(el);
    states.delete(el);
    if (s && s.ms) s.ms.detach();
    if (owner === el) { owner = null; order({ op: "stop" }); }
    if (!s) return;
    if (!s.paused) fire(el, "pause");
    fire(el, "abort", "emptied");
  };

  const realSrc = desc(proto, "src");
  Object.defineProperty(proto, "src", {
    configurable: true,
    get() { const s = states.get(this); return s ? s.url : realSrc.get.call(this); },
    set(v) {
      v = String(v);
      if (sources.has(v)) { feed(this, v, sources.get(v)); return; }
      if (/^https?:/.test(v)) { begin(this, v); return; }
      if (states.has(this)) stop(this);
      realSrc.set.call(this, v);
    },
  });
  const realObject = desc(proto, "srcObject");
  Object.defineProperty(proto, "srcObject", {
    configurable: true,
    get() { const s = states.get(this); return s && s.ms ? s.ms : realObject.get.call(this); },
    set(v) {
      if (v instanceof FedMediaSource) { feed(this, "", v); return; }
      if (states.has(this)) stop(this);
      realObject.set.call(this, v);
    },
  });
  const sproto = HTMLSourceElement.prototype;
  const realSourceSrc = desc(sproto, "src");
  const sourced = (el, url) => {
    if (!(el instanceof HTMLMediaElement) || states.has(el)) return;
    if (sources.has(url)) feed(el, url, sources.get(url));
    else if (/^https?:/.test(url)) begin(el, url);
  };
  Object.defineProperty(sproto, "src", {
    configurable: true,
    get() { return realSourceSrc.get.call(this); },
    set(v) {
      realSourceSrc.set.call(this, v);
      sourced(this.parentNode, String(v));
    },
  });
  const adopted = (el, child) => {
    if (child instanceof HTMLSourceElement && child.hasAttribute("src")) sourced(el, child.src);
  };
  const realAppend = Node.prototype.appendChild;
  proto.appendChild = function (child) {
    const out = realAppend.call(this, child);
    adopted(this, child);
    return out;
  };
  const realInsert = Node.prototype.insertBefore;
  proto.insertBefore = function (child, ref) {
    const out = realInsert.call(this, child, ref);
    adopted(this, child);
    return out;
  };
  const realRemove = Node.prototype.removeChild;
  proto.removeChild = function (child) {
    const s = states.get(this);
    if (s && child instanceof HTMLSourceElement && child.src === s.url) stop(this);
    return realRemove.call(this, child);
  };

  if (navigator.mediaCapabilities) {
    const plain = (c) => {
      if (!c || !c.contentType) return true;
      const params = [];
      if (c.width) params.push("width=" + c.width);
      if (c.height) params.push("height=" + c.height);
      if (c.framerate) params.push("framerate=" + c.framerate);
      return feeds(c.contentType + (params.length ? "; " + params.join("; ") : ""));
    };
    navigator.mediaCapabilities.decodingInfo = (config) => {
      const supported = !!config && plain(config.video) && plain(config.audio);
      const answer = { supported, smooth: supported, powerEfficient: supported, keySystemAccess: null };
      const ks = config && config.keySystemConfiguration;
      if (!supported || !ks) return Promise.resolve(answer);
      const caps = (kind) => (config[kind] ? [{
        contentType: config[kind].contentType,
        robustness: (ks[kind] && ks[kind].robustness) || "",
      }] : undefined);
      return navigator.requestMediaKeySystemAccess(ks.keySystem, [{
        initDataTypes: ks.initDataType ? [ks.initDataType] : ["cenc"],
        videoCapabilities: caps("video"),
        audioCapabilities: caps("audio"),
        distinctiveIdentifier: ks.distinctiveIdentifier,
        persistentState: ks.persistentState,
      }]).then((access) => Object.assign(answer, { keySystemAccess: access }),
        () => Object.assign(answer, { supported: false, smooth: false, powerEfficient: false }));
    };
  }

  const removeAttr = Element.prototype.removeAttribute;
  proto.removeAttribute = function (n) {
    if (String(n).toLowerCase() === "src" && states.has(this)) stop(this);
    return removeAttr.call(this, n);
  };
  const setAttr = Element.prototype.setAttribute;
  proto.setAttribute = function (n, v) {
    if (String(n).toLowerCase() === "src") { this.src = v; return; }
    return setAttr.call(this, n, v);
  };
  const mediaKeys = new WeakMap();
  proto.setMediaKeys = function (k) { mediaKeys.set(this, k); return Promise.resolve(); };
  Object.defineProperty(proto, "mediaKeys", {
    configurable: true,
    get() { return mediaKeys.get(this) || null; },
  });
  const realLoad = proto.load;
  proto.load = function () { if (!states.has(this)) return realLoad.call(this); };
  const realPlay = proto.play;
  proto.play = function () {
    const s = states.get(this);
    if (!s) return realPlay.call(this);
    if (s.paused) {
      s.paused = false; s.ended = false;
      if (s.ready < 3) { s.waiting = true; fire(this, "play", "waiting"); } else { s.starting = true; fire(this, "play"); }
    }
    order({ op: "play" });
    return Promise.resolve();
  };
  const realPause = proto.pause;
  proto.pause = function () {
    const s = states.get(this);
    if (!s) return realPause.call(this);
    if (!s.paused) { mark(s, now(s)); s.paused = true; s.starting = false; fire(this, "pause"); }
    order({ op: "pause" });
  };

  const virtual = (p, k, get, set) => {
    const d = desc(p, k);
    Object.defineProperty(p, k, {
      configurable: true,
      get() { const s = states.get(this); return s ? get(s, this) : d.get.call(this); },
      set(v) { const s = states.get(this); if (!s || !set) return d.set && d.set.call(this, v); set(s, this, v); },
    });
  };
  virtual(proto, "currentSrc", (s) => s.url);
  virtual(proto, "currentTime", (s) => now(s), (s, el, v) => {
    v = Number(v);
    if (s.ready < 1) { s.pending = v; mark(s, v); return; }
    const from = now(s);
    mark(s, v); s.seeking = true; s.ended = false;
    fire(el, "seeking");
    order({ op: "seek", t: v, from });
  });
  virtual(proto, "duration", (s) => (s.ms ? s.ms.dur : s.duration));
  virtual(proto, "paused", (s) => s.paused);
  virtual(proto, "ended", (s) => s.ended);
  virtual(proto, "seeking", (s) => s.seeking);
  virtual(proto, "readyState", (s) => (s.waiting && !s.paused ? Math.min(s.ready, 2) : s.ready));
  virtual(proto, "networkState", () => 2);
  virtual(proto, "error", (s) => s.error);
  virtual(proto, "buffered", (s) => timeRanges(s.ms ? shared(s) : whole(s)));
  virtual(proto, "seekable", (s) => {
    if (!s.askedSeekable) { s.askedSeekable = true; order({ op: "seekable" }); }
    return timeRanges(whole(s));
  });
  virtual(proto, "played", (s) => timeRanges(whole(s)));
  virtual(proto, "playbackRate", (s) => s.rate, (s, el, v) => {
    mark(s, now(s)); s.rate = Number(v);
    fire(el, "ratechange");
  });
  virtual(proto, "volume", (s) => s.volume, (s, el, v) => {
    s.volume = Number(v);
    fire(el, "volumechange");
  });
  virtual(proto, "muted", (s) => s.muted, (s, el, v) => {
    s.muted = !!v;
    fire(el, "volumechange");
  });
  virtual(vproto, "videoWidth", (s) => s.w);
  virtual(vproto, "videoHeight", (s) => s.h);
  const realQuality = vproto.getVideoPlaybackQuality;
  vproto.getVideoPlaybackQuality = function () {
    const s = states.get(this);
    if (!s) return realQuality.call(this);
    return { creationTime: performance.now(), totalVideoFrames: s.frames, droppedVideoFrames: 0, corruptedVideoFrames: 0 };
  };
  if (desc(vproto, "webkitDecodedFrameCount")) virtual(vproto, "webkitDecodedFrameCount", (s) => s.frames);
  if (desc(vproto, "webkitDroppedFrameCount")) virtual(vproto, "webkitDroppedFrameCount", () => 0);
})();
