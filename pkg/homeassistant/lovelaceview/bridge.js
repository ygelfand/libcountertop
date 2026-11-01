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
