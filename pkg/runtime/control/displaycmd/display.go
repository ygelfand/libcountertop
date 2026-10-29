package displaycmd

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/display/gpu"
	"github.com/ygelfand/libcountertop/pkg/display/message"
	"github.com/ygelfand/libcountertop/pkg/display/panel"
	"github.com/ygelfand/libcountertop/pkg/runtime/control"
	"github.com/ygelfand/libcountertop/pkg/runtime/control/screencmd"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	benchFrames = 60
	benchLimit  = 5 * time.Second
)

type Dependencies struct {
	Panel    func() *panel.Driver
	GPU      func() *gpu.Renderer
	Messages func() *message.Messages
	Label    string
	Shot     string
	Thumbs   string
}

func Commands(d Dependencies) []*cobra.Command {
	return []*cobra.Command{
		control.Says(&cobra.Command{
			Use:   "shot [PATH]",
			Short: "Write what the panel is showing to a PNG",
			Args:  cobra.MaximumNArgs(1),
		}, d.shot),
		control.Says(&cobra.Command{
			Use:   "size",
			Short: "Say how big the picture is",
			Args:  cobra.NoArgs,
		}, func([]string) (string, error) {
			w, h := d.Panel().Size()
			return fmt.Sprintf("%d %d", w, h), nil
		}),
		control.Says(&cobra.Command{
			Use:   "stats",
			Short: "How much has been drawn since the start",
			Args:  cobra.NoArgs,
		}, func([]string) (string, error) {
			frames, whole, painted := d.Panel().Stats()
			return fmt.Sprintf("frames %d whole %d painted %d megapixels %.1f",
				frames, whole, painted, float64(painted)/1e6), nil
		}),
		control.Says(&cobra.Command{
			Use:   "fps [FRAMES]",
			Short: "Measure the frame rate over a run of frames",
			Args:  cobra.MaximumNArgs(1),
		}, d.fps),
		control.Says(&cobra.Command{
			Use:   "gltime FILE [PASSES AMOUNT RADIUS GLOWPASSES SECONDS]",
			Short: "Run a shader file on a full-screen GPU layer for timing",
			Args:  cobra.RangeArgs(1, 6),
		}, d.gltime),
		control.Says(&cobra.Command{
			Use:   "render KIND FILE [W H [FRAMES]]",
			Short: "Render one visual offscreen after loud frames into a PNG",
			Args:  cobra.RangeArgs(2, 5),
		}, d.render),
		control.Says(&cobra.Command{
			Use:   "wake sunrise|ember|white SHARE FILE",
			Short: "Render a wake light look offscreen at SHARE (0 to 1) of its window into a PNG",
			Args:  cobra.ExactArgs(3),
		}, d.wake),
		control.Says(&cobra.Command{
			Use:   "clip KIND DIR [W H [SECONDS [FPS]]]",
			Short: "Render a few seconds of one visual offscreen, to a beat, as numbered PNG frames",
			Args:  cobra.RangeArgs(2, 6),
		}, d.clip),
		control.Says(&cobra.Command{
			Use:   "thumbs [DIR]",
			Short: "Render every visual offscreen into picker thumbnails, landscape and portrait",
			Args:  cobra.MaximumNArgs(1),
		}, d.thumbs),
		control.Does(&cobra.Command{
			Use:   "message TONE SECONDS TITLE : BODY",
			Short: "Put a message on the panel",
			Args:  cobra.MinimumNArgs(3),
		}, d.say),
		control.Does(&cobra.Command{
			Use:   "clear",
			Short: "Take the message away again",
			Args:  cobra.NoArgs,
		}, func([]string) error {
			d.Messages().Hide()
			return nil
		}),
	}
}

func (d Dependencies) shot(args []string) (string, error) {
	path := d.Shot
	if len(args) > 0 {
		path = args[0]
	}
	if err := screencmd.Shot(d.Panel(), path); err != nil {
		return "", err
	}
	return path, nil
}

func (d Dependencies) fps(args []string) (string, error) {
	frames := benchFrames
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return "", fmt.Errorf("fps: %q is not a number of frames", args[0])
		}
		frames = n
	}
	ctx, cancel := context.WithTimeout(context.Background(), benchLimit)
	defer cancel()
	b, err := d.Panel().Bench(ctx, frames)
	if err != nil {
		return "", err
	}
	return b.String(), nil
}

func (d Dependencies) say(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("message: want TONE SECONDS TITLE : BODY")
	}
	tone := schema.Tone(args[0])
	if !slices.Contains(schema.Tones(), tone) {
		return fmt.Errorf("message: no tone %q, want one of %s", args[0], tones())
	}
	secs, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("message: %q is not a number of seconds", args[1])
	}
	title, body := split(args[2:])
	if body == "" {
		return fmt.Errorf("message: the message has no body")
	}
	d.Messages().Show(message.Message{Title: title, Body: body, Tone: tone}, message.Hold(secs))
	return nil
}

func split(args []string) (title, body string) {
	for i, a := range args {
		if a == ":" {
			return join(args[:i]), join(args[i+1:])
		}
	}
	return "", join(args)
}

func join(words []string) string { return strings.TrimSpace(strings.Join(words, " ")) }

func tones() string {
	out := make([]string, 0, len(schema.Tones()))
	for _, t := range schema.Tones() {
		out = append(out, string(t))
	}
	return strings.Join(out, ", ")
}
