package diag

import (
	"fmt"
	"log/slog"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/android/logd"
)

// The transport caps a message at 65515 bytes.
const logPage = 32 * 1024

type Logs struct {
	Version int      `json:"version"`
	Page    int      `json:"page"`
	Pages   int      `json:"pages"`
	Lines   []string `json:"lines"`
}

func (d *Diag) Actions() []*esphome.Action {
	if d.o.LogTag == "" {
		return nil
	}
	return []*esphome.Action{
		{
			Name:    "logs",
			Args:    []esphome.Arg{{Name: "page", Type: esphome.ArgInt}},
			Answers: true,
			Run: func(c esphome.Call) (any, error) {
				return logs(d.o.LogTag, c.Int("page"))
			},
		},
	}
}

func logs(tag string, page int) (*Logs, error) {
	lines, err := logd.Dump(tag)
	if err != nil {
		if len(lines) == 0 {
			return nil, err
		}
		slog.Warn("reading the log back was cut short", "lines", len(lines), "err", err)
	}

	text := make([]string, len(lines))
	for i, l := range lines {
		text[i] = l.Level.String() + " " + l.Text
	}

	pages := split(text)
	if page < 0 || page >= len(pages) {
		return nil, fmt.Errorf("page %d of %d", page, len(pages))
	}
	return &Logs{Version: 1, Page: page, Pages: len(pages), Lines: pages[page]}, nil
}

func split(lines []string) [][]string {
	pages := [][]string{nil}
	size := 0

	for _, l := range lines {
		n := len(l) + 1
		if size > 0 && size+n > logPage {
			pages = append(pages, nil)
			size = 0
		}

		at := len(pages) - 1
		pages[at] = append(pages[at], l)
		size += n
	}
	return pages
}
