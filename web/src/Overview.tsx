import { Button, Group, Paper, Stack, Text, Title } from "@mantine/core";
import {
  IconBulb,
  IconHome,
  IconCamera,
  IconCameraOff,
  IconClockHour4,
  IconMicrophone,
  IconMicrophoneOff,
  IconMusic,
  IconPhone,
  IconTemperature,
  type Icon,
} from "@tabler/icons-react";
import type { Description, Device, State } from "./api";
import { Desktop } from "./Desktop";
import { Screen } from "./Screen";
import { ControlPairing } from "./ControlPairing";

type Props = {
  device: Device;
  entities: Description[];
  states: Map<string, State["state"]>;
  adopted?: boolean;
  go: (slug: string, focus?: string) => void;
};

const adoptURL =
  "https://my.home-assistant.io/redirect/config_flow_start/?domain=esphome";

type Line = {
  key: string;
  icon: Icon;
  label: string;
  value: string;
  tone?: "alert" | "live" | "quiet";
};

function value(states: Map<string, State["state"]>, id: string): unknown {
  const s = states.get(id);
  if (!s || s.missing_state === true) return undefined;
  return s.state;
}

function since(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

function lines(
  entities: Description[],
  states: Map<string, State["state"]>,
): Line[] {
  const byID = new Map(entities.map((e) => [e.id, e]));
  const name = (id: string) => byID.get(id)?.info.name || id;
  const unit = (id: string) => byID.get(id)?.info.unit_of_measurement ?? "";
  const yes = (on: boolean) => (on ? "Yes" : "No");
  const out: Line[] = [];
  const call = value(states, "call_state");
  if (typeof call === "string" && call && call !== "idle") {
    const peer = value(states, "call_peer");
    const who = typeof peer === "string" && peer ? ` · ${peer}` : "";
    out.push({
      key: "call",
      icon: IconPhone,
      label: name("call_state"),
      value: call + who,
      tone: "live",
    });
  }
  const playing = value(states, "now_playing");
  if (typeof playing === "string" && playing) {
    out.push({
      key: "playing",
      icon: IconMusic,
      label: name("now_playing"),
      value: playing,
      tone: "live",
    });
  }
  const muted = value(states, "microphone_muted");
  if (typeof muted === "boolean") {
    out.push({
      key: "mic",
      icon: muted ? IconMicrophoneOff : IconMicrophone,
      label: name("microphone_muted"),
      value: yes(muted),
      tone: muted ? "alert" : undefined,
    });
  }
  const covered = value(states, "camera_covered");
  if (typeof covered === "boolean") {
    out.push({
      key: "camera",
      icon: covered ? IconCameraOff : IconCamera,
      label: name("camera_covered"),
      value: yes(covered),
      tone: covered ? "alert" : undefined,
    });
  }
  const light = value(states, "ambient_light");
  if (typeof light === "number") {
    out.push({
      key: "light",
      icon: IconBulb,
      label: name("ambient_light"),
      value: `${Math.round(light)} ${unit("ambient_light")}`.trim(),
    });
  }
  const temp = value(states, "cpu_temperature");
  if (typeof temp === "number") {
    out.push({
      key: "temp",
      icon: IconTemperature,
      label: name("cpu_temperature"),
      value: `${temp.toFixed(1)} ${unit("cpu_temperature")}`.trim(),
      tone: "quiet",
    });
  }
  const up = value(states, "uptime");
  if (typeof up === "number") {
    out.push({
      key: "uptime",
      icon: IconClockHour4,
      label: name("uptime"),
      value: since(up),
      tone: "quiet",
    });
  }
  return out;
}

export function Overview({ device, entities, states, adopted }: Props) {
  const list = lines(entities, states);
  return (
    <div className="ct-page">
      <div className="ct-overview" data-screen={device.screen || undefined}>
        {device.screen && <Desktop enabled={device.pages?.includes("desktop") ?? false} actions={device.pages?.includes("desktop") ? <ControlPairing /> : undefined}><Screen /></Desktop>}
        <Stack gap="lg" className="ct-status">
          <Stack gap={2}>
            <Title order={2} fw={600}>
              {device.name}
            </Title>
            <Text c="dimmed" size="sm">
              {device.model}
            </Text>
          </Stack>
          {adopted === false && (
            <Paper withBorder p="md" radius="md">
              <Group justify="space-between" wrap="wrap" gap="sm">
                <Text fw={600}>Not in Home Assistant yet</Text>
                <Button
                  component="a"
                  href={adoptURL}
                  target="_blank"
                  rel="noreferrer"
                  leftSection={<IconHome size={16} />}
                >
                  Add to Home Assistant
                </Button>
              </Group>
            </Paper>
          )}
          <Stack gap={0}>
            {list.map((l) => (
              <Group
                key={l.key}
                className="ct-line"
                data-tone={l.tone}
                wrap="nowrap"
                gap="sm"
              >
                <l.icon size={18} stroke={1.6} className="ct-line-icon" />
                <Text size="sm" className="ct-line-label">
                  {l.label}
                </Text>
                <Text size="sm" fw={500} className="ct-line-value" truncate>
                  {l.value}
                </Text>
              </Group>
            ))}
          </Stack>
        </Stack>
      </div>
    </div>
  );
}
