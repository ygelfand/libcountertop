import { NavLink } from "@mantine/core";
import { groupID, type Section } from "./sections";

type Props = {
  section: Section;
  active: boolean;
  expanded: boolean;
  focus: string;
  go: (slug: string, target?: string) => void;
  fold: () => void;
};

export function SettingsLink({ section: s, active, expanded, focus, go, fold }: Props) {
  return <NavLink
    label={s.title}
    leftSection={<s.icon size={18} stroke={1.6} />}
    active={active && s.groups.length < 2}
    opened={expanded}
    onClick={() => { if (s.groups.length < 2) go(s.slug); }}
    onChange={fold}
    childrenOffset={28}
  >
    {s.groups.length > 1 ? s.groups.map((g, i) => <NavLink
      key={g.title}
      label={g.title}
      active={active && focus === groupID(s.slug, i)}
      onClick={() => go(s.slug, groupID(s.slug, i))}
    />) : null}
  </NavLink>;
}
