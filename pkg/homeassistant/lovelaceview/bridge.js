(() => {
  const ask = (kind, raw) => window.countertopAuth(JSON.stringify({ kind, callback: JSON.parse(raw).callback }));
  const config = {
    hasSettingsScreen: false,
    canWriteTag: false,
    hasExoPlayer: false,
    canCommissionMatter: false,
    canImportThreadCredentials: false,
    hasAssist: false,
    hasBarCodeScanner: 0,
    canSetupImprov: false,
  };
  try {
    localStorage.setItem("dockedSidebar", JSON.stringify("always_hidden"));
  } catch (e) {}
  const css = ".header{display:none!important}";
  const root = () =>
    document
      .querySelector("home-assistant")
      ?.shadowRoot?.querySelector("home-assistant-main")
      ?.shadowRoot?.querySelector("partial-panel-resolver > *")
      ?.shadowRoot?.querySelector("hui-root")?.shadowRoot;
  setInterval(() => {
    document.documentElement.style.setProperty("--header-height", "0px");
    const r = root();
    if (!r || r.getElementById("countertop-full")) return;
    const s = document.createElement("style");
    s.id = "countertop-full";
    s.textContent = css;
    r.appendChild(s);
  }, 250);
  window.externalApp = {
    getExternalAuth: (raw) => ask("get", raw),
    revokeExternalAuth: (raw) => ask("revoke", raw),
    externalBus: (raw) => {
      const m = JSON.parse(raw);
      if (m.type === "config/get") {
        setTimeout(() => window.externalBus({ id: m.id, type: "result", success: true, result: config }));
      }
    },
  };
})();
