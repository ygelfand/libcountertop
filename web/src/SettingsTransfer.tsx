import { useState } from "react";
import { Button, Code, FileButton, Group, Modal, Paper, ScrollArea, Stack, Text } from "@mantine/core";
import { IconDownload, IconUpload } from "@tabler/icons-react";
import { exportSettings, importSettings, parseSettings, settingsCount, settingsFilename, type SettingsImport, type SettingsSnapshot } from "./settingsSnapshot";

export function SettingsTransfer({ name, fail }: { name: string; fail: (e: Error) => void }) {
  const [busy, setBusy] = useState<"export" | "import">();
  const [pending, setPending] = useState<{ name: string; settings: SettingsSnapshot }>();
  const [result, setResult] = useState<SettingsImport>();

  const download = async () => {
    setBusy("export");
    try {
      const settings = await exportSettings();
      const url = URL.createObjectURL(new Blob([JSON.stringify(settings, null, 2) + "\n"], { type: "application/json" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = settingsFilename(name);
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) { fail(e as Error); }
    finally { setBusy(undefined); }
  };

  const choose = async (file: File | null) => {
    if (!file) return;
    setBusy("import");
    setResult(undefined);
    try {
      if (file.size > 1024 * 1024) throw new Error("Choose a settings file smaller than 1 MB.");
      setPending({ name: file.name, settings: parseSettings(await file.text()) });
    } catch (e) { fail(e as Error); }
    finally { setBusy(undefined); }
  };

  const apply = async () => {
    if (!pending) return;
    setBusy("import");
    try {
      setResult(await importSettings(pending.settings));
      setPending(undefined);
    } catch (e) { fail(e as Error); }
    finally { setBusy(undefined); }
  };

  return <Paper withBorder p="md">
    <Stack gap="sm">
      <Text size="sm">Copy portable settings between devices. Device-local settings are excluded.</Text>
      <Text size="sm" c="orange">Exported files may contain passwords and credentials. Store them privately.</Text>
      <Group>
        <Button variant="light" leftSection={<IconDownload size={16} />} loading={busy === "export"} disabled={!!busy} onClick={() => void download()}>Export settings</Button>
        <FileButton accept="application/json,.json" onChange={(file) => void choose(file)}>
          {(props) => <Button {...props} variant="light" leftSection={<IconUpload size={16} />} loading={busy === "import"} disabled={!!busy}>Import settings</Button>}
        </FileButton>
      </Group>
      {result && <Stack gap="xs" role="status">
        <Text size="sm">{result.changed} changed, {result.skipped.length} skipped{result.errors ? ", with errors" : ""}.</Text>
        {result.skipped.length > 0 && <ScrollArea.Autosize mah={180}><Code block>{result.skipped.map((s) => `${s.domain}.${s.name}: ${s.why}`).join("\n")}</Code></ScrollArea.Autosize>}
        {result.errors && <Text size="sm" c="red" role="alert">{result.errors}</Text>}
      </Stack>}
    </Stack>
    <Modal opened={!!pending} onClose={() => { if (!busy) setPending(undefined); }} title="Import settings" centered closeOnEscape={!busy} closeOnClickOutside={!busy} withCloseButton={!busy}>
      <Stack>
        <Text size="sm">Import {pending ? settingsCount(pending.settings) : 0} settings from {pending?.name}?</Text>
        <Text size="sm">Matching settings on this device will be overwritten. Unsupported and device-local settings will be skipped. Other settings stay unchanged.</Text>
        <Group justify="flex-end">
          <Button variant="default" disabled={!!busy} onClick={() => setPending(undefined)}>Cancel</Button>
          <Button loading={busy === "import"} onClick={() => void apply()}>Import settings</Button>
        </Group>
      </Stack>
    </Modal>
  </Paper>;
}
