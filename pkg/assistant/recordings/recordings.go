package recordings

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
)

// The transport caps a message at 65515 bytes; base64 adds a third.
const Page = 32 * 1024

const LongestSeconds = 30

const KeepMost = 10

const (
	wavExt  = ".wav"
	metaExt = ".json"
)

type Options struct {
	Dir      string
	Rate     int
	Slots    int
	DeviceID func(slot int) uint32
	Keeps    func(slot int) int
	Save     func(slot, count int) error
}

type meta struct {
	Slot  int `json:"slot"`
	Bytes int `json:"bytes"`

	At int64 `json:"at"`
}

type Store struct {
	options Options

	mu   sync.Mutex
	open string
	slot int
	buf  []byte

	disk sync.Mutex

	keep []*esphome.Number
}

func New(options Options) *Store {
	s := &Store{options: options}
	for slot := range options.Slots {
		number := &esphome.Number{
			Base: esphome.Base{
				ObjectID: fmt.Sprintf("keep_recordings_%d", slot+1),
				Name:     "Recordings kept",
				Icon:     "mdi:record-rec",
				Category: esphome.CategoryConfig,
			},
			Min: 0, Max: KeepMost, Step: 1,
			Mode: esphome.NumberBox,
		}
		if options.DeviceID != nil {
			number.DeviceID = options.DeviceID(slot)
		}
		number.OnCommand = func(v float32) {
			number.Set(v)
			if err := options.Save(slot, int(v)); err != nil {
				slog.Error("saving how many recordings to keep failed", "slot", slot+1, "err", err)
			}
			s.Prune()
		}
		s.keep = append(s.keep, number)
	}
	return s
}

func (s *Store) Name() string { return "recording" }

func (s *Store) Longest() int { return LongestSeconds * s.options.Rate * 2 }

func (s *Store) Entities() []esphome.Entity {
	out := make([]esphome.Entity, 0, len(s.keep))
	for _, number := range s.keep {
		out = append(out, number)
	}
	return out
}

func (s *Store) Restore() {
	for slot, number := range s.keep {
		number.Set(float32(s.keeps(slot)))
	}
	s.Prune()
}

func (s *Store) Actions() []*esphome.Action {
	return []*esphome.Action{
		{
			Name:        "recordings",
			Description: "List the ids of the assistant turns whose audio is kept.",
			Answers:     true,
			Run: func(esphome.Call) (any, error) {
				return s.available()
			},
		},
		{
			Name:        "turn_audio",
			Description: "One page of a kept turn's WAV recording, base64-encoded.",
			Args: []esphome.Arg{
				{Name: "id", Type: esphome.ArgString, Description: "A turn id from recordings."},
				{
					Name:        "page",
					Type:        esphome.ArgInt,
					Description: "Zero-based; the answer says how many pages there are.",
					Example:     "0",
				},
			},
			Answers: true,
			Run: func(c esphome.Call) (any, error) {
				return s.page(c.String("id"), c.Int("page"))
			},
		},
	}
}

type Available struct {
	Version int      `json:"version"`
	IDs     []string `json:"ids"`
}

func (s *Store) available() (*Available, error) {
	out := &Available{Version: 1, IDs: []string{}}

	entries, err := os.ReadDir(s.options.Dir)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, metaExt) {
			out.IDs = append(out.IDs, strings.TrimSuffix(name, metaExt))
		}
	}
	return out, nil
}

func (s *Store) Opens(id string, slot int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.keeps(slot) <= 0 {
		s.open, s.buf = "", nil
		return
	}
	s.open, s.slot, s.buf = id, slot, nil
}

func (s *Store) Frame(pcm []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.open == "" || len(s.buf) >= s.Longest() {
		return
	}
	s.buf = append(s.buf, pcm...)
}

func (s *Store) Closes() {
	s.mu.Lock()
	id, slot, buf := s.open, s.slot, s.buf
	s.open, s.buf = "", nil
	s.mu.Unlock()

	if id == "" || len(buf) == 0 {
		return
	}

	s.disk.Lock()
	defer s.disk.Unlock()

	if err := s.write(id, slot, buf); err != nil {
		slog.Error("saving a recording failed", "id", id, "err", err)
		return
	}
	s.prune()
}

func (s *Store) Seconds(id string) float64 {
	m, err := s.readMeta(id)
	if err != nil {
		return 0
	}
	return float64(m.Bytes) / float64(s.options.Rate*2)
}

// The frontend rejects an answer without Version.
type Answer struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Page    int    `json:"page"`
	Pages   int    `json:"pages"`
	MIME    string `json:"mime"`
	Data    string `json:"data"`
}

func (s *Store) page(id string, page int) (*Answer, error) {
	whole, err := os.ReadFile(s.wavPath(id))
	if err != nil {
		return nil, fmt.Errorf("no recording for turn %s", id)
	}

	pages := (len(whole) + Page - 1) / Page
	if page < 0 || page >= pages {
		return nil, fmt.Errorf("page %d of %d", page, pages)
	}

	end := min((page+1)*Page, len(whole))
	return &Answer{
		Version: 1,
		ID:      id,
		Page:    page,
		Pages:   pages,
		MIME:    "audio/wav",
		Data:    base64.StdEncoding.EncodeToString(whole[page*Page : end]),
	}, nil
}

func (s *Store) Prune() {
	s.disk.Lock()
	defer s.disk.Unlock()
	s.prune()
}

func (s *Store) prune() {
	entries, err := os.ReadDir(s.options.Dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		slog.Error("reading the recordings failed", "dir", s.options.Dir, "err", err)
		return
	}

	type rec struct {
		id string
		at int64
	}
	bySlot := map[int][]rec{}
	complete := map[string]bool{}
	wavs := []string{}

	for _, e := range entries {
		switch name := e.Name(); {
		case strings.HasSuffix(name, metaExt):
			id := strings.TrimSuffix(name, metaExt)
			m, err := s.readMeta(id)
			if err != nil {
				continue
			}
			complete[id] = true
			bySlot[m.Slot] = append(bySlot[m.Slot], rec{id, m.At})
		case strings.HasSuffix(name, wavExt):
			wavs = append(wavs, strings.TrimSuffix(name, wavExt))
		}
	}

	for slot, recs := range bySlot {
		sort.Slice(recs, func(i, j int) bool {
			if recs[i].at != recs[j].at {
				return recs[i].at > recs[j].at
			}
			return recs[i].id > recs[j].id
		})
		for i, r := range recs {
			if i >= s.keeps(slot) {
				s.remove(r.id)
			}
		}
	}

	for _, id := range wavs {
		if !complete[id] {
			s.remove(id)
		}
	}
}

func (s *Store) keeps(slot int) int {
	if s.options.Keeps == nil {
		return 0
	}
	return max(s.options.Keeps(slot), 0)
}

func (s *Store) write(id string, slot int, pcm []byte) error {
	if err := os.MkdirAll(s.options.Dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.wavPath(id), wav(pcm, s.options.Rate), 0o644); err != nil {
		return err
	}

	blob, err := json.Marshal(meta{Slot: slot, Bytes: len(pcm), At: time.Now().UnixNano()})
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(id), blob, 0o644)
}

func (s *Store) remove(id string) {
	for _, path := range []string{s.wavPath(id), s.metaPath(id)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Error("removing a recording failed", "path", path, "err", err)
		}
	}
}

func (s *Store) readMeta(id string) (meta, error) {
	blob, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return meta{}, err
	}
	var m meta
	return m, json.Unmarshal(blob, &m)
}

func (s *Store) wavPath(id string) string  { return filepath.Join(s.options.Dir, id+wavExt) }
func (s *Store) metaPath(id string) string { return filepath.Join(s.options.Dir, id+metaExt) }

const header = 44

func wav(pcm []byte, rate int) []byte {
	out := make([]byte, 0, header+len(pcm))

	put32 := func(v uint32) {
		out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	put16 := func(v uint16) { out = append(out, byte(v), byte(v>>8)) }

	out = append(out, "RIFF"...)
	put32(uint32(header - 8 + len(pcm)))
	out = append(out, "WAVEfmt "...)
	put32(16)
	put16(1)
	put16(1)
	put32(uint32(rate))
	put32(uint32(rate * 2))
	put16(2)
	put16(16)
	out = append(out, "data"...)
	put32(uint32(len(pcm)))

	return append(out, pcm...)
}
