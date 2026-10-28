import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { MantineProvider } from "@mantine/core";
import { SettingsTransfer } from "../src/SettingsTransfer.tsx";
import { exportSettings, importSettings, parseSettings, settingsCount, settingsFilename } from "../src/settingsSnapshot.ts";

test("export filenames contain a safe device name and UTC timestamp", () => {
  const at = new Date("2026-10-27T20:45:12.345Z");
  assert.equal(settingsFilename("Kitchen Show", at), "Kitchen-Show-settings-2026-10-27T20-45-12-345Z.json");
  assert.equal(settingsFilename(" ../Kitchen/Show: ", at), "Kitchen-Show-settings-2026-10-27T20-45-12-345Z.json");
  assert.equal(settingsFilename("...", at), "device-settings-2026-10-27T20-45-12-345Z.json");
});

test("settings files preserve portable values including dotted names and credentials", () => {
  const text = JSON.stringify({ home: { dashboard: "firmware", "tessera.columns": "6" }, photos: { token: "private" } });
  const settings = parseSettings(text);
  assert.equal(settingsCount(settings), 3);
  assert.deepEqual(settings, JSON.parse(text));
});

test("invalid settings files are rejected before import", () => {
  for (const text of ["not json", "null", "[]", "{}", '{"home":[]}', '{"home":{"dashboard":true}}', '{"home":{"dashboard":null}}']) {
    assert.throws(() => parseSettings(text));
  }
  assert.throws(() => parseSettings(JSON.stringify({ photos: { token: "x".repeat(65536) } })), /too large/);
});

test("settings transfer offers both controls and warns about credentials without applying settings", () => {
  const markup = renderToStaticMarkup(<MantineProvider><SettingsTransfer name="Kitchen" fail={() => assert.fail("no request during render")} /></MantineProvider>);
  assert.match(markup, /Export settings/);
  assert.match(markup, /Import settings/);
  assert.match(markup, /passwords and credentials/);
  assert.match(markup, /Device-local settings are excluded/);
});

test("export and import use the existing portable actions and preserve partial results", async (t) => {
  const requests = [];
  t.mock.method(globalThis, "fetch", async (url, init) => {
    requests.push({ url, body: JSON.parse(init.body) });
    const answer = url.endsWith("settings_export")
      ? { settings: [{ domain: "home", name: "dashboard", value: "firmware" }] }
      : { changed: 1, skipped: [{ domain: "camera", name: "turn", why: "local" }], errors: "home.dashboard: unavailable" };
    return new Response(JSON.stringify({ answer }));
  });
  const settings = await exportSettings();
  assert.deepEqual(settings, { home: { dashboard: "firmware" } });
  const result = await importSettings(settings);
  assert.equal(result.changed, 1);
  assert.equal(result.skipped[0].why, "local");
  assert.equal(result.errors, "home.dashboard: unavailable");
  assert.deepEqual(requests, [
    { url: "/api/v1/actions/settings_export", body: { args: {} } },
    { url: "/api/v1/actions/settings_import", body: { args: { settings: JSON.stringify(settings) } } },
  ]);
});
