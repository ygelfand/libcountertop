import assert from "node:assert/strict";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { MantineProvider } from "@mantine/core";
import { IconSettings } from "@tabler/icons-react";
import { SettingsLink } from "../src/SettingsLink.tsx";
import { Settings } from "../src/Settings.tsx";
import { sections } from "../src/sections.ts";

const field = (id, kind = "toggle") => ({ id, name: id, title: id, group: "General", kind, value: kind === "toggle" ? "on" : "25" });
const section = (groups = 1) => ({
  key: "settings.system", slug: "system", title: "System", icon: IconSettings, count: groups,
  groups: Array.from({ length: groups }, (_, i) => ({ title: `Group ${i}`, entries: [{ domain: "diag", field: field(`setting-${i}`) }] })),
});

test("single-group settings links navigate on click", () => {
  const visits = [];
  const link = SettingsLink({ section: section(), active: false, expanded: false, focus: "", go: (...args) => visits.push(args), fold: () => assert.fail("leaf cannot expand") });
  link.props.onClick();
  assert.deepEqual(visits, [["system"]]);
});

test("multi-group settings links keep their expansion handler", () => {
  let folded = 0;
  const link = SettingsLink({ section: section(2), active: false, expanded: false, focus: "", go: () => assert.fail("parent navigates through fold"), fold: () => folded++ });
  link.props.onClick();
  link.props.onChange(true);
  assert.equal(folded, 1);
  assert.equal(link.props.children.length, 2);
});

test("home, alerts, cameras and system retain and render their fields", () => {
  const domains = [
    { domain: "home", section_key: "home.title", section: "Home control", fields: [field("home_enabled")] },
    { domain: "alerts", section_key: "alerts.page", section: "System alerts", fields: [field("alerts_ring_for", "number")] },
    { domain: "camera", section_key: "settings.camera", section: "Camera", fields: [field("camera_brightness", "number")] },
    { domain: "diag", section_key: "settings.system", section: "System", fields: [field("diag_low_memory")] },
  ];
  const grouped = sections(domains);
  assert.equal(grouped.length, 4);
  for (const s of grouped) {
    assert.equal(s.count, 1);
    const html = renderToStaticMarkup(<MantineProvider forceColorScheme="light"><Settings section={s} focus="" change={async () => {}} /></MantineProvider>);
    assert.ok(html.includes(`id="f-${s.groups[0].entries[0].field.id}"`));
    assert.ok(html.includes(s.groups[0].entries[0].field.title));
  }
});
