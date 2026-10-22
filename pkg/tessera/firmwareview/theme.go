package firmwareview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func prefs(t theme.Theme, themed bool, bottom schema.TesseraBottom) []byte {
	var b strings.Builder
	if themed {
		colors(&b, t)
	}
	dark := 0
	if t.Dark {
		dark = 1
	}
	fmt.Fprintf(&b, "DARK %d\n", dark)
	fmt.Fprintf(&b, "BOTTOM %d\n", max(slices.Index(schema.TesseraBottoms(), bottom), 0))
	return []byte(b.String())
}

func colors(b *strings.Builder, t theme.Theme) {
	pressed := func(c theme.Color) theme.Color { return c.Blend(t.Text, 0.08) }
	line := t.Surface.Blend(t.Text, 0.1)
	table := []struct {
		name  string
		color theme.Color
	}{
		{"PAGE", t.Background},
		{"PAGE_SOFT", t.Background.Blend(t.Surface, 0.5)},
		{"PAGE_PRESSED", pressed(t.Background)},
		{"CARD", t.Surface},
		{"CARD_PRESSED", pressed(t.Surface)},
		{"LINE", line},
		{"RAISED", t.Surface},
		{"RAISED_LINE", line},
		{"TRACK", t.Surface.Blend(t.Text, 0.12)},
		{"INK", t.Text},
		{"INK_SOFT", t.Text.Blend(t.Surface, 0.2)},
		{"MUTED", t.Muted},
		{"SUBTLE", t.Muted.Blend(t.Surface, 0.3)},
		{"SLATE", t.Muted},
		{"ON_ACCENT", t.Contrast(t.Accent)},
		{"ACCENT", t.Accent},
		{"ACCENT_PRESSED", t.Accent.Blend(t.Text, 0.15)},
		{"ACCENT_BRIGHT", t.Accent},
		{"ACCENT_TINT", t.Accent.Blend(t.Surface, 0.75)},
		{"ACCENT_ICON", t.Accent},
	}
	for _, r := range table {
		v := hex(r.color)
		fmt.Fprintf(b, "%s %06x %06x\n", r.name, v, v)
	}
}

func hex(c theme.Color) uint32 { return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B) }
