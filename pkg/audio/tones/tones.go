package tones

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/audio/resample"
	"github.com/ygelfand/libcountertop/pkg/audio/tone"
	"github.com/ygelfand/libcountertop/pkg/hook"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/system/metrics"
)

// The transport caps a message at 65515 bytes; base64 adds a third.
const Page = 32 * 1024

const (
	Rate    = 48000
	Longest = 30 * time.Second

	reserve = 8 << 20
	header  = 44
	wavExt  = ".wav"
	metaExt = ".json"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Tone struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Options struct {
	Dir      string
	Rate     int
	Channels int
}

type Store struct {
	Changed hook.Hook[struct{}]

	options Options

	mu      sync.Mutex
	tones   []Tone
	clips   map[string][]int16
	pending map[string]*upload
}

type upload struct {
	name  string
	pages [][]byte
}

func New(options Options) *Store {
	s := &Store{options: options, clips: map[string][]int16{}, pending: map[string]*upload{}}
	s.tones = s.load()
	return s
}

func (s *Store) Name() string { return "tones" }

func (s *Store) List() []Tone {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.tones)
}

func (s *Store) Choices() []setting.Option {
	list := s.List()
	out := make([]setting.Option, len(list))
	for i, t := range list {
		out[i] = setting.Option{Value: string(schema.CustomChime(t.Key)), Label: t.Name}
	}
	return out
}

func (s *Store) Sound(c, fallback schema.Chime) tone.Sound {
	if key, ok := c.Custom(); ok {
		if clip, ok := s.clip(key); ok {
			return tone.Sound{
				Clip:     clip,
				Rate:     s.options.Rate,
				Channels: max(s.options.Channels, 1),
			}
		}
		c = fallback
	}
	return tone.Sound{Notes: tone.Wake(c)}
}

func (s *Store) Actions() []*esphome.Action {
	return []*esphome.Action{
		{
			Name:        "tone_write",
			Description: "Upload a custom tone, one page per call; the last page saves it.",
			Args: []esphome.Arg{
				{
					Name:        "key",
					Type:        esphome.ArgString,
					Description: "Lowercase letters, digits, - and _.",
					Example:     "front-door",
				},
				{Name: "name", Type: esphome.ArgString, Example: "Front door"},
				{Name: "page", Type: esphome.ArgInt, Description: "Zero-based.", Example: "0"},
				{Name: "pages", Type: esphome.ArgInt, Example: "4"},
				{
					Name:        "data",
					Type:        esphome.ArgString,
					Description: "Base64 of up to 32 KiB of 48 kHz mono 16-bit little-endian PCM; 30 s at most in all.",
				},
			},
			Answers: true,
			Run: func(c esphome.Call) (any, error) {
				data, err := base64.StdEncoding.DecodeString(c.String("data"))
				if err != nil {
					return nil, fmt.Errorf(
						"tone %q page %d is not base64",
						c.String("key"),
						c.Int("page"),
					)
				}
				key := c.String("key")
				return done(
					key,
					s.Write(key, c.String("name"), c.Int("page"), c.Int("pages"), data),
				)
			},
		},
		{
			Name:        "tone_rename",
			Description: "Rename a custom tone.",
			Args: []esphome.Arg{
				{Name: "key", Type: esphome.ArgString},
				{Name: "name", Type: esphome.ArgString},
			},
			Answers: true,
			Run: func(c esphome.Call) (any, error) {
				key := c.String("key")
				return done(key, s.Rename(key, c.String("name")))
			},
		},
		{
			Name:        "tone_remove",
			Description: "Delete a custom tone.",
			Args:        []esphome.Arg{{Name: "key", Type: esphome.ArgString}},
			Answers:     true,
			Run: func(c esphome.Call) (any, error) {
				key := c.String("key")
				return done(key, s.Remove(key))
			},
		},
	}
}

// Home Assistant fails a return_response call whose answer is not a JSON object.
func done(key string, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]string{"key": key}, nil
}

func (s *Store) Write(key, name string, page, pages int, data []byte) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("tone key %q takes lowercase letters, digits, - and _", key)
	}
	most := (int(Longest.Seconds())*Rate*2 + Page - 1) / Page
	if pages < 1 || pages > most || page < 0 || page >= pages {
		return fmt.Errorf("tone %q: page %d of %d", key, page, pages)
	}

	s.mu.Lock()
	up := s.pending[key]
	if page == 0 || up == nil || len(up.pages) != pages {
		up = &upload{name: name, pages: make([][]byte, pages)}
		s.pending[key] = up
	}
	up.pages[page] = data
	if page != pages-1 {
		s.mu.Unlock()
		return nil
	}
	delete(s.pending, key)
	s.mu.Unlock()

	for i, p := range up.pages {
		if p == nil {
			return fmt.Errorf("tone %q is missing page %d of %d", key, i, pages)
		}
	}
	pcm := decode(slices.Concat(up.pages...))
	if len(pcm) == 0 {
		return fmt.Errorf("tone %q has no sound", key)
	}
	if len(pcm) > int(Longest.Seconds())*Rate {
		return fmt.Errorf("tone %q is longer than %s", key, Longest)
	}

	clip := s.convert(pcm)
	if err := s.room(len(clip) * 2); err != nil {
		return err
	}
	if err := s.save(Tone{Key: key, Name: strings.TrimSpace(up.name)}, clip); err != nil {
		return fmt.Errorf("tone %q: %w", key, err)
	}

	s.mu.Lock()
	s.clips[key] = clip
	s.mu.Unlock()
	s.Changed.Emit(struct{}{})
	return nil
}

func (s *Store) Rename(key, name string) error {
	name = strings.TrimSpace(name)
	s.mu.Lock()
	i := slices.IndexFunc(s.tones, func(t Tone) bool { return t.Key == key })
	s.mu.Unlock()
	if i < 0 {
		return fmt.Errorf("no tone %q", key)
	}
	if err := s.writeMeta(Tone{Key: key, Name: name}); err != nil {
		return fmt.Errorf("tone %q: %w", key, err)
	}
	s.mu.Lock()
	s.tones = s.load()
	s.mu.Unlock()
	s.Changed.Emit(struct{}{})
	return nil
}

func (s *Store) Remove(key string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("no tone %q", key)
	}
	s.mu.Lock()
	held := slices.ContainsFunc(s.tones, func(t Tone) bool { return t.Key == key })
	s.mu.Unlock()
	if !held {
		return nil
	}
	for _, path := range []string{s.path(key, wavExt), s.path(key, metaExt)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("tone %q: %w", key, err)
		}
	}
	s.mu.Lock()
	delete(s.clips, key)
	s.tones = s.load()
	s.mu.Unlock()
	s.Changed.Emit(struct{}{})
	return nil
}

func (s *Store) clip(key string) ([]int16, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clips[key]; ok {
		return c, true
	}
	if !slices.ContainsFunc(s.tones, func(t Tone) bool { return t.Key == key }) {
		return nil, false
	}
	raw, err := os.ReadFile(s.path(key, wavExt))
	if err != nil || len(raw) <= header {
		slog.Warn("a tone could not be read", "key", key, "err", err)
		return nil, false
	}
	c := decode(raw[header:])
	s.clips[key] = c
	return c, true
}

func (s *Store) convert(pcm []int16) []int16 {
	if s.options.Rate != Rate {
		pcm = resample.NewRational(Rate, s.options.Rate, 1).Run(pcm)
	}
	channels := max(s.options.Channels, 1)
	if channels == 1 {
		return pcm
	}
	out := make([]int16, len(pcm)*channels)
	for i, v := range pcm {
		for c := range channels {
			out[i*channels+c] = v
		}
	}
	return out
}

func (s *Store) room(bytes int) error {
	if err := os.MkdirAll(s.options.Dir, 0o755); err != nil {
		return err
	}
	free, err := metrics.Free(s.options.Dir)
	if err != nil {
		return err
	}
	if free-int64(bytes) < reserve {
		return fmt.Errorf("not enough free space for a %d KB tone", bytes/1024)
	}
	return nil
}

func (s *Store) save(t Tone, clip []int16) error {
	if err := os.WriteFile(s.path(t.Key, wavExt), s.wav(clip), 0o644); err != nil {
		return err
	}
	if err := s.writeMeta(t); err != nil {
		return err
	}
	s.mu.Lock()
	s.tones = s.load()
	s.mu.Unlock()
	return nil
}

func (s *Store) writeMeta(t Tone) error {
	blob, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(t.Key, metaExt), blob, 0o644)
}

func (s *Store) load() []Tone {
	paths, _ := filepath.Glob(filepath.Join(s.options.Dir, "*"+metaExt))
	out := make([]Tone, 0, len(paths))
	for _, p := range paths {
		blob, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var t Tone
		if json.Unmarshal(blob, &t) != nil || !keyPattern.MatchString(t.Key) {
			continue
		}
		if t.Name == "" {
			t.Name = t.Key
		}
		out = append(out, t)
	}
	slices.SortFunc(
		out,
		func(a, b Tone) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) },
	)
	return out
}

func (s *Store) path(key, ext string) string { return filepath.Join(s.options.Dir, key+ext) }

func (s *Store) wav(clip []int16) []byte {
	channels := max(s.options.Channels, 1)
	data := len(clip) * 2
	out := make([]byte, 0, header+data)
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(header-8+data))
	out = append(out, "WAVEfmt "...)
	out = binary.LittleEndian.AppendUint32(out, 16)
	out = binary.LittleEndian.AppendUint16(out, 1)
	out = binary.LittleEndian.AppendUint16(out, uint16(channels))
	out = binary.LittleEndian.AppendUint32(out, uint32(s.options.Rate))
	out = binary.LittleEndian.AppendUint32(out, uint32(s.options.Rate*channels*2))
	out = binary.LittleEndian.AppendUint16(out, uint16(channels*2))
	out = binary.LittleEndian.AppendUint16(out, 16)
	out = append(out, "data"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(data))
	for _, v := range clip {
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	return out
}

func decode(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}
