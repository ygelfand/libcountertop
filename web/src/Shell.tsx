import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AppShell,
  Burger,
  Drawer,
  Group,
  NavLink,
  Notification,
  ScrollArea,
  Stack,
  Text,
  Tooltip,
  ActionIcon,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconAlarm, IconCode, IconLayoutDashboard, IconLogout, IconPhone, IconSpeakerphone } from "@tabler/icons-react";
import { Alarms } from "./Alarms";
import { Broadcast } from "./Broadcast";
import { Call } from "./Call";
import { Developer } from "./Developer";
import { api, follow, SignedOut, type Description, type Device, type Domain, type State } from "./api";
import { Events, type Seen } from "./Events";
import { Heart } from "./Heart";
import { Logo } from "./Logo";
import { notifications } from "./notify";
import { Overview } from "./Overview";
import { Search, SearchButton } from "./Search";
import { sections, type Section } from "./sections";
import { Settings } from "./Settings";
import { SettingsLink } from "./SettingsLink";
import { Theme } from "./Theme";

const seenMost = 500;
const overview = "overview";
const alarms = "alarms";
const calls = "call";
const broadcast = "broadcast";
const developer = "developer";

export const appPages = [
  { slug: alarms, label: "Alarms", icon: IconAlarm },
  { slug: calls, label: "Call", icon: IconPhone },
  { slug: broadcast, label: "Broadcast", icon: IconSpeakerphone },
];

function page(): string {
  return location.hash.replace(/^#/, "") || overview;
}

export function Shell({ device, onOut }: { device: Device; onOut: () => void }) {
  const [opened, { toggle, close }] = useDisclosure();
  const [eventsOpen, events] = useDisclosure();
  const [domains, setDomains] = useState<Domain[]>([]);
  const [entities, setEntities] = useState<Description[]>([]);
  const [states, setStates] = useState<Map<string, State["state"]>>(new Map());
  const [open, setOpen] = useState(page);
  const [focus, setFocus] = useState("");
  const [live, setLive] = useState(false);
  const [seen, setSeen] = useState<Seen[]>([]);
  const [beat, setBeat] = useState(0);
  const [unseen, setUnseen] = useState(0);
  const [paused, setPaused] = useState(false);
  const [error, setError] = useState("");
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const eventsOpenRef = useRef(eventsOpen);
  eventsOpenRef.current = eventsOpen;

  const signedOut = useCallback(
    (e: unknown) => {
      if (e instanceof SignedOut) onOut();
      else throw e;
    },
    [onOut],
  );

  useEffect(() => {
    api.settings().then(setDomains).catch(signedOut);
    api.entities().then(setEntities).catch(signedOut);
  }, [signedOut]);

  useEffect(() => notifications.listen(setError), []);

  useEffect(() => {
    const onHash = () => setOpen(page());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  const go = useCallback(
    (slug: string, target = "") => {
      if (location.hash.replace(/^#/, "") !== slug) location.hash = slug;
      setOpen(slug);
      setFocus(target);
      close();
      window.setTimeout(() => {
        const el =
          target &&
          (document.getElementById(target) ??
            document.getElementById(`f-${target}`) ??
            document.getElementById(`e-${target}`));
        if (el) el.scrollIntoView({ block: "center", behavior: "smooth" });
        else window.scrollTo({ top: 0 });
      }, 60);
    },
    [close],
  );

  const byID = useMemo(() => {
    const m = new Map<string, { domain: string; name: string }>();
    for (const d of domains) for (const f of d.fields) m.set(f.id, { domain: d.domain, name: f.name });
    return m;
  }, [domains]);
  const adopted = useMemo(() => {
    const f = domains.find((d) => d.domain === "api")?.fields.find((x) => x.name === "adopted");
    return f ? f.value === "on" : undefined;
  }, [domains]);
  const byIDRef = useRef(byID);
  byIDRef.current = byID;

  const refresh = useCallback(
    (domain: string, name: string) => {
      api
        .field(domain, name)
        .then((f) =>
          setDomains((all) =>
            all.map((d) =>
              d.domain !== domain ? d : { ...d, fields: d.fields.map((x) => (x.name === name ? f : x)) },
            ),
          ),
        )
        .catch(signedOut);
    },
    [signedOut],
  );

  useEffect(
    () =>
      follow((s, snapshot) => {
        setStates((m) => new Map(m).set(s.id, s.state));
        if (snapshot) return;
        setBeat((n) => n + 1);
        if (!pausedRef.current) {
          setSeen((all) => [{ at: new Date(), id: s.id, state: s.state }, ...all].slice(0, seenMost));
          if (!eventsOpenRef.current) setUnseen((n) => n + 1);
        }
        const hit = byIDRef.current.get(s.id);
        if (hit) refresh(hit.domain, hit.name);
      }, setLive),
    [refresh],
  );

  const all = useMemo(() => sections(domains), [domains]);
  const current = all.find((s) => s.slug === open);
  const apps = appPages.filter((a) => device.pages?.includes(a.slug));
  const app = apps.find((a) => a.slug === open)?.slug;

  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const arrived = current?.key;
  useEffect(() => {
    if (arrived) setExpanded((e) => (e.has(arrived) ? e : new Set(e).add(arrived)));
  }, [arrived]);
  const fold = (s: Section) => {
    if (s.groups.length < 2) {
      go(s.slug);
      return;
    }
    if (current?.key !== s.key) {
      go(s.slug);
      setExpanded((e) => new Set(e).add(s.key));
      return;
    }
    setExpanded((e) => {
      const next = new Set(e);
      if (!next.delete(s.key)) next.add(s.key);
      return next;
    });
  };

  const change = async (domain: string, name: string, value: string) => {
    try {
      await api.change(domain, name, value);
      refresh(domain, name);
    } catch (e) {
      signedOut(e);
    }
  };

  const signOut = () => api.logout().finally(onOut);

  const showEvents = () => {
    setUnseen(0);
    events.open();
  };

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{ width: 248, breakpoint: "sm", collapsed: { mobile: !opened } }}
      padding="lg"
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap" gap="sm">
          <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
            <Logo h={24} />
            <Text fw={600} truncate>
              {device.name}
            </Text>
          </Group>
          <Group gap="xs" wrap="nowrap">
            <SearchButton />
            <Heart live={live} beat={beat} unseen={unseen} onClick={showEvents} />
            <Theme />
            <Tooltip label="Sign out">
              <ActionIcon variant="default" size={32} onClick={signOut} aria-label="Sign out">
                <IconLogout size={16} />
              </ActionIcon>
            </Tooltip>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar p="xs">
        <AppShell.Section grow component={ScrollArea} scrollbars="y">
          <Stack gap={2}>
            <NavLink
              label="Overview"
              leftSection={<IconLayoutDashboard size={18} stroke={1.6} />}
              active={!current && !app && open !== developer}
              onClick={() => go(overview)}
            />
            {apps.map((a) => (
              <NavLink
                key={a.slug}
                label={a.label}
                leftSection={<a.icon size={18} stroke={1.6} />}
                active={open === a.slug}
                onClick={() => go(a.slug)}
              />
            ))}
            <Text size="xs" fw={700} c="dimmed" tt="uppercase" lts={0.6} px="sm" pt="md" pb={4}>
              Settings
            </Text>
            {all.map((s) => (
              <SettingsLink
                key={s.key}
                section={s}
                active={current?.key === s.key}
                expanded={expanded.has(s.key)}
                focus={focus}
                go={go}
                fold={() => fold(s)}
              />
            ))}
          </Stack>
        </AppShell.Section>
        <AppShell.Section>
          <NavLink
            label="Developer"
            leftSection={<IconCode size={18} stroke={1.6} />}
            active={open === developer}
            onClick={() => go(developer)}
          />
        </AppShell.Section>
      </AppShell.Navbar>

      <AppShell.Main>
        {current ? (
          <Settings section={current} focus={focus} change={change} />
        ) : app === alarms ? (
          <Alarms onSignedOut={signedOut} />
        ) : app === calls ? (
          <Call device={device} onSignedOut={signedOut} />
        ) : app === broadcast ? (
          <Broadcast device={device} onSignedOut={signedOut} />
        ) : open === developer ? (
          <Developer name={device.name} entities={entities} states={states} focus={focus} onSignedOut={signedOut} />
        ) : (
          <Overview device={device} entities={entities} states={states} adopted={adopted} go={go} />
        )}
        {error && (
          <Notification
            color="red"
            onClose={() => setError("")}
            style={{ position: "fixed", bottom: 16, right: 16, maxWidth: 360, zIndex: 300 }}
          >
            {error}
          </Notification>
        )}
      </AppShell.Main>

      <Drawer
        opened={eventsOpen}
        onClose={events.close}
        position="right"
        size="lg"
        title={<Text fw={600}>Events</Text>}
        styles={{ body: { height: "calc(100% - 60px)" } }}
      >
        <Events seen={seen} paused={paused} setPaused={setPaused} clear={() => setSeen([])} />
      </Drawer>

      <Search sections={all} entities={entities} states={states} apps={apps} go={go} />
    </AppShell>
  );
}
