package schema

import "maps"

type Voice struct {
	Commands bool            `json:"listen"`
	Actions  bool            `json:"actions"`
	Off      map[string]bool `json:"off,omitempty"`
	Label    string          `json:"label,omitempty"`
}

func DefaultVoice() Voice { return Voice{Commands: true, Actions: true} }

func (v Voice) Action(name string) bool { return v.Actions && !v.Off[ActionKey(name)] }

func (v Voice) Command(name string) bool { return v.Commands && !v.Off[CommandKey(name)] }

func (v Voice) Clone() Voice {
	v.Off = maps.Clone(v.Off)
	return v
}

func ActionKey(name string) string { return "action." + name }

func CommandKey(name string) string { return "command." + name }
