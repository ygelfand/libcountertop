import { useEffect, useMemo, useState } from "react";
import {
  Button,
  Code,
  Group,
  NumberInput,
  Paper,
  ScrollArea,
  Select,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Title,
} from "@mantine/core";
import { IconCode, IconDownload, IconFileText, IconPlayerPlay, IconSearch } from "@tabler/icons-react";
import { api, SignedOut, type ActionInfo, type Description, type State } from "./api";
import { readable, shown } from "./entities";
import { notifications } from "./notify";
import { SettingsTransfer } from "./SettingsTransfer";

type Props = {
  name: string;
  entities: Description[];
  states: Map<string, State["state"]>;
  focus: string;
  onSignedOut: (e: unknown) => void;
};

type Choice = { value: string; label: string; group: string; action?: ActionInfo; button?: Description };

function Heading({ children }: { children: React.ReactNode }) {
  return (
    <Text size="xs" fw={700} c="dimmed" tt="uppercase" lts={0.6} px={4}>
      {children}
    </Text>
  );
}

export function Developer({ name, entities, states, focus, onSignedOut }: Props) {
  const [actions, setActions] = useState<ActionInfo[]>([]);
  const fail = (e: Error) => (e instanceof SignedOut ? onSignedOut(e) : notifications.error(e.message));

  useEffect(() => {
    api.actions().then(setActions).catch(fail);
  }, []);

  return (
    <Stack gap="lg" maw={900}>
      <Group gap="sm">
        <ThemeIcon variant="light" size="lg" radius="md">
          <IconCode size={20} stroke={1.6} />
        </ThemeIcon>
        <Title order={3}>Developer</Title>
      </Group>

      <Stack gap={6}>
        <Heading>Downloads</Heading>
        <SimpleGrid cols={{ base: 1, xs: 2 }} spacing="sm">
          <Download href="/api/v1/logs" icon={IconFileText} title="Log" detail="Text" />
          <Download href="/api/v1/diagnostics" icon={IconDownload} title="Diagnostics" detail="JSON" />
        </SimpleGrid>
      </Stack>

      <Stack gap={6}>
        <Heading>Settings</Heading>
        <SettingsTransfer name={name} fail={fail} />
      </Stack>

      <Stack gap={6}>
        <Heading>Run</Heading>
        <Runner actions={actions} buttons={entities.filter((d) => d.kind === "button")} fail={fail} />
      </Stack>

      <Stack gap={6}>
        <Heading>Readings</Heading>
        <Readings entities={entities} states={states} focus={focus} />
      </Stack>
    </Stack>
  );
}

type DownloadProps = { href: string; icon: typeof IconFileText; title: string; detail: string };

function Download({ href, icon: Icon, title, detail }: DownloadProps) {
  return (
    <Paper
      withBorder
      p="md"
      component="a"
      href={href}
      download
      className="ct-shortcut"
      style={{ color: "inherit", textDecoration: "none" }}
    >
      <Group wrap="nowrap" gap="md">
        <ThemeIcon variant="light" size="lg" radius="md">
          <Icon size={20} stroke={1.6} />
        </ThemeIcon>
        <Stack gap={0}>
          <Text fw={600} size="sm">
            {title}
          </Text>
          <Text size="xs" c="dimmed">
            {detail}
          </Text>
        </Stack>
      </Group>
    </Paper>
  );
}

function blank(a?: ActionInfo): Record<string, unknown> {
  if (!a) return {};
  return Object.fromEntries(a.args.map((arg) => [arg.name, arg.type === "bool" ? false : arg.type === "string" ? "" : 0]));
}

type RunnerProps = { actions: ActionInfo[]; buttons: Description[]; fail: (e: Error) => void };

function Runner({ actions, buttons, fail }: RunnerProps) {
  const choices = useMemo<Choice[]>(
    () => [
      ...actions.map((a) => ({ value: `a:${a.name}`, label: a.name, group: "Actions", action: a })),
      ...buttons.map((b) => ({ value: `b:${b.id}`, label: `Press ${b.info.name || b.id}`, group: "Buttons", button: b })),
    ],
    [actions, buttons],
  );
  const [picked, setPicked] = useState<string | null>(null);
  const choice = choices.find((c) => c.value === picked);
  const [args, setArgs] = useState<Record<string, unknown>>({});
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string>();
  const set = (name: string, v: unknown) => setArgs((x) => ({ ...x, [name]: v }));

  const pick = (v: string | null) => {
    setPicked(v);
    setArgs(blank(choices.find((c) => c.value === v)?.action));
    setResult(undefined);
  };

  const run = () => {
    if (!choice) return;
    setBusy(true);
    setResult(undefined);
    const work = choice.button
      ? api.press(choice.button.id).then(() => "Pressed")
      : api
          .runAction(choice.action!.name, args)
          .then((r) => (r.answer === null || r.answer === undefined ? "Done" : JSON.stringify(r.answer, null, 2)));
    work
      .then(setResult)
      .catch(fail)
      .finally(() => setBusy(false));
  };

  const data = ["Actions", "Buttons"]
    .map((group) => ({ group, items: choices.filter((c) => c.group === group).map(({ value, label }) => ({ value, label })) }))
    .filter((g) => g.items.length > 0);

  return (
    <Paper withBorder p="md">
      <Stack gap="md">
        <Group align="flex-end" wrap="nowrap">
          <Select
            style={{ flex: 1 }}
            placeholder="Pick or type an action"
            data={data}
            value={picked}
            onChange={pick}
            searchable
            clearable
            nothingFoundMessage="No such action"
            ff="monospace"
            comboboxProps={{ withinPortal: true }}
            maxDropdownHeight={360}
          />
          <Button leftSection={<IconPlayerPlay size={16} />} disabled={!choice} loading={busy} onClick={run}>
            Run
          </Button>
        </Group>
        {choice?.action && choice.action.args.length > 0 && (
          <SimpleGrid cols={{ base: 1, xs: 2 }} spacing="sm">
            {choice.action.args.map((arg) =>
              arg.type === "bool" ? (
                <Switch
                  key={arg.name}
                  label={arg.name}
                  checked={Boolean(args[arg.name])}
                  onChange={(e) => set(arg.name, e.currentTarget.checked)}
                  mt="lg"
                />
              ) : arg.type === "string" ? (
                <TextInput
                  key={arg.name}
                  label={arg.name}
                  ff="monospace"
                  value={String(args[arg.name] ?? "")}
                  onChange={(e) => set(arg.name, e.currentTarget.value)}
                />
              ) : (
                <NumberInput
                  key={arg.name}
                  label={arg.name}
                  allowDecimal={arg.type === "float"}
                  value={Number(args[arg.name] ?? 0)}
                  onChange={(v) => set(arg.name, Number(v) || 0)}
                />
              ),
            )}
          </SimpleGrid>
        )}
        {result !== undefined && (
          <ScrollArea.Autosize mah={360}>
            <Code block>{result}</Code>
          </ScrollArea.Autosize>
        )}
      </Stack>
    </Paper>
  );
}

type ReadingsProps = { entities: Description[]; states: Map<string, State["state"]>; focus: string };

function Readings({ entities, states, focus }: ReadingsProps) {
  const [filter, setFilter] = useState("");
  const rows = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return entities
      .filter((d) => readable.has(d.kind))
      .filter((d) => !f || d.id.includes(f) || (d.info.name ?? "").toLowerCase().includes(f))
      .sort((a, b) => (a.info.name || a.id).localeCompare(b.info.name || b.id));
  }, [entities, filter]);

  return (
    <Paper withBorder>
      <Stack gap={0}>
        <TextInput
          m="sm"
          size="xs"
          placeholder="Filter"
          leftSection={<IconSearch size={14} />}
          value={filter}
          onChange={(e) => setFilter(e.currentTarget.value)}
        />
        <Table verticalSpacing={6} fz="sm" highlightOnHover>
          <Table.Tbody>
            {rows.map((d) => {
              const v = shown(d, states.get(d.id));
              const unit = d.info.unit_of_measurement;
              return (
                <Table.Tr key={d.id} id={`e-${d.id}`} className="ct-row" data-focus={focus === d.id || undefined}>
                  <Table.Td>
                    <Text size="sm">{d.info.name || d.id}</Text>
                    <Text size="xs" c="dimmed" ff="monospace">
                      {d.id}
                    </Text>
                  </Table.Td>
                  <Table.Td ta="right" ff="monospace" style={{ wordBreak: "break-word" }}>
                    {v}
                    {unit && v !== "—" ? ` ${unit}` : ""}
                  </Table.Td>
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>
      </Stack>
    </Paper>
  );
}
