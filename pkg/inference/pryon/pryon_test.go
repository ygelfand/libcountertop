package pryon

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

func TestIDsAndPhrasesComeFromTheModelFolder(t *testing.T) {
	cases := []struct {
		locale, name, id, label, lang string
	}{
		{"en-US", "ALEXA", "pryon_alexa", "Pryon: Alexa", "en"},
		{"en-US", "ALEXA,HEY_DISNEY", "pryon_alexa_hey_disney", "Pryon: Alexa, Hey Disney", "en"},
		{"de-DE", "COMPUTER", "pryon_computer_de_de", "Pryon: Computer (de-DE)", "de"},
		{"world", "ALEXA", "pryon_alexa_world", "Pryon: Alexa (world)", ""},
		{"mul-021", "ALEXA", "pryon_alexa_mul_021", "Pryon: Alexa (mul-021)", ""},
	}
	for _, c := range cases {
		if got := ID(c.locale, c.name); got != c.id {
			t.Errorf("ID(%q, %q) = %q, want %q", c.locale, c.name, got, c.id)
		}
		if got := label(c.locale, c.name); got != c.label {
			t.Errorf("label(%q, %q) = %q, want %q", c.locale, c.name, got, c.label)
		}
		if got := strings.Join(languages(c.locale), ","); got != c.lang {
			t.Errorf("languages(%q) = %q, want %q", c.locale, got, c.lang)
		}
	}
}

func TestEveryPublishedWordHasOneIDAndAnArchive(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range published.Words {
		if seen[w.id()] {
			t.Errorf("%s listed twice", w.id())
		}
		seen[w.id()] = true
		if w.Keyword == "" || w.Archive.Name == "" || len(w.Archive.Sum) != 64 ||
			w.Archive.Size <= 0 {
			t.Errorf("incomplete word %+v", w)
		}
	}
	for _, id := range []string{"pryon_alexa", "pryon_alexa_compact", "pryon_hey_shaq", "pryon_alexa_nl_nl"} {
		if !seen[id] {
			t.Errorf("%s missing from the catalog", id)
		}
	}
	if s := published.Stop; s.Keyword != "STOP" || s.Archive.Name == "" ||
		published.Library.Name == "" {
		t.Errorf("stop %+v library %+v", s, published.Library)
	}
}

func TestTheCatalogListsOnlyPublishedWords(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "models", "en-US", "JARVIS")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, manifest), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if models := NewCatalog(dir, func() bool { return false }).Models(); len(models) != 0 {
		t.Fatalf("pryon off lists %d models", len(models))
	}
	models := NewCatalog(dir, func() bool { return true }).Models()
	if m, ok := wake.Find(models, "pryon_jarvis"); ok {
		t.Fatalf("unpublished word listed: %+v", m)
	}
	if len(models) != len(published.Words) {
		t.Errorf("listed %d models, want %d", len(models), len(published.Words))
	}
	computer, ok := wake.Find(models, "pryon_computer")
	if !ok || computer.Phrase != "Pryon: Computer" || computer.Kind != wake.KindPryon ||
		computer.Keyword != "COMPUTER" {
		t.Fatalf("published Computer model %+v found %v", computer, ok)
	}
	if want := filepath.Join(
		dir,
		"models",
		"en-US",
		"AMAZON-COMPUTER-ZIGGY",
		manifest,
	); computer.Path != want {
		t.Errorf("computer path %q, want %q", computer.Path, want)
	}
	dutch, ok := wake.Find(models, "pryon_alexa_nl_nl")
	if !ok || dutch.Path != filepath.Join(dir, "models", "de-DE", "ALEXA", manifest) {
		t.Errorf("Dutch Alexa %+v found %v, want the de-DE set", dutch, ok)
	}
}

func TestAnArchiveUnpacksInsideItsFolder(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.xz")
	write := func(entries map[string]string) {
		var buf bytes.Buffer
		x, err := xz.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		tw := tar.NewWriter(x)
		for name, body := range entries {
			if err := tw.WriteHeader(
				&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg},
			); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := x.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(map[string]string{"lib/libpryon.so": "elf"})
	if err := extract(archive, dir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(
		filepath.Join(dir, "lib", "libpryon.so"),
	); err != nil ||
		string(got) != "elf" {
		t.Errorf("unpacked %q, %v", got, err)
	}

	write(map[string]string{"../escape": "x"})
	if err := extract(archive, dir); err == nil {
		t.Error("an entry outside the folder was accepted")
	}
}

func TestHelperRepliesDecode(t *testing.T) {
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint32(payload[0:], 954061)
	binary.LittleEndian.PutUint32(payload[4:], 4640)
	binary.LittleEndian.PutUint32(payload[8:], 16160)
	d, err := detectedOf(message{kind: msgDetected, payload: append(payload, "pryon_computer"...)})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "pryon_computer" || d.Score < 0.954 || d.Score > 0.955 || d.Start != 4640 ||
		d.End != 16160 {
		t.Errorf("detection %+v", d)
	}

	status := make([]byte, 4)
	binary.LittleEndian.PutUint32(status, uint32(0xfffffffd))
	l, err := loadedOf(message{kind: msgLoaded, payload: append(status, "pryon_alexa"...)})
	if err != nil || l.id != "pryon_alexa" || l.status != -3 {
		t.Errorf("loaded %+v, %v", l, err)
	}

	if err := checkReady(message{kind: msgReady, payload: []byte{1, 0, 0, 0}}); err == nil {
		t.Error("a helper speaking version 1 was accepted")
	}

	p := loadPayload("0/pryon_alexa", "/m/pryon.manifest", Aux("STOP", -3))
	want := "0/pryon_alexa\x00/m/pryon.manifest\x00" +
		`{"keyword":{"enabled-keyword-list":["STOP"],"sensitivityValue":-3}}`
	if string(p) != want {
		t.Errorf("load payload %q, want %q", p, want)
	}
}
