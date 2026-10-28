import { api } from "./api";

export type SettingsSnapshot = Record<string, Record<string, string>>;

export function settingsFilename(name: string, at = new Date()): string {
  const device = name.trim().replace(/[^\p{L}\p{N}_-]+/gu, "-").replace(/^-+|-+$/g, "") || "device";
  return `${device}-settings-${at.toISOString().replace(/[:.]/g, "-")}.json`;
}
export type SettingsImport = {
  changed: number;
  skipped: { domain: string; name: string; why: string }[];
  errors?: string;
};

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

export function settingsCount(settings: SettingsSnapshot): number {
  return Object.values(settings).reduce((count, fields) => count + Object.keys(fields).length, 0);
}

export function parseSettings(text: string): SettingsSnapshot {
  let value: unknown;
  try { value = JSON.parse(text); }
  catch { throw new Error("Choose a valid settings JSON file."); }
  if (!record(value) || !Object.values(value).every((fields) => record(fields) && Object.values(fields).every((field) => typeof field === "string"))) {
    throw new Error("Choose a settings export with domains and string values.");
  }
  const settings = value as SettingsSnapshot;
  if (!settingsCount(settings)) throw new Error("The file contains no settings.");
  const body = JSON.stringify({ args: { settings: JSON.stringify(settings) } });
  if (new TextEncoder().encode(body).length > 64 * 1024) throw new Error("The settings file is too large. Import fewer settings.");
  return settings;
}

export async function exportSettings(): Promise<SettingsSnapshot> {
  const { answer } = await api.runAction("settings_export", {});
  const rows = (answer as { settings: { domain: string; name: string; value: string }[] }).settings;
  const domains = new Map<string, [string, string][]>();
  for (const row of rows) {
    const fields = domains.get(row.domain) ?? [];
    fields.push([row.name, row.value]);
    domains.set(row.domain, fields);
  }
  return Object.fromEntries([...domains].map(([domain, fields]) => [domain, Object.fromEntries(fields)]));
}

export async function importSettings(settings: SettingsSnapshot): Promise<SettingsImport> {
  const { answer } = await api.runAction("settings_import", { settings: JSON.stringify(settings) });
  return answer as SettingsImport;
}
