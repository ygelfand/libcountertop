package pryon

import (
	"archive/tar"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ulikunitz/xz"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/network/releases"
)

const (
	folder   = "pryon/1"
	manifest = "pryon.manifest"
	prefix   = "pryon_"
)

type asset struct {
	Name string `json:"name"`
	Sum  string `json:"sum"`
	Size int64  `json:"size"`
}

type word struct {
	Word    string `json:"word"`
	Locale  string `json:"locale"`
	Keyword string `json:"keyword"`
	Dir     string `json:"dir"`
	Archive asset  `json:"archive"`
}

func (w word) id() string { return ID(w.Locale, w.Word) }

type catalogFile struct {
	Library asset  `json:"library"`
	Words   []word `json:"words"`
	Stop    word   `json:"stop"`
}

//go:embed catalog.json
var catalogJSON []byte

var published = func() catalogFile {
	var c catalogFile
	if err := json.Unmarshal(catalogJSON, &c); err != nil {
		panic("pryon: catalog.json: " + err.Error())
	}
	return c
}()

type Catalog struct {
	dir string
	on  func() bool

	mu sync.Mutex
}

func NewCatalog(dir string, on func() bool) *Catalog { return &Catalog{dir: dir, on: on} }

func (c *Catalog) Library() string { return filepath.Join(c.dir, "lib", "libpryon.so") }

func (c *Catalog) Models() []wake.Model {
	if !c.on() {
		return nil
	}
	out := make([]wake.Model, 0, len(published.Words))
	for _, w := range published.Words {
		out = append(out, c.model(w, label(w.Locale, w.Word)))
	}
	return out
}

func (c *Catalog) Stop(ctx context.Context) (wake.Model, error) {
	w := published.Stop
	if err := c.ensure(ctx, w, func(float32) {}); err != nil {
		return wake.Model{}, err
	}
	m := c.model(w, phrase(w.Word))
	m.ID = prefix + "stop"
	return m, nil
}

func (c *Catalog) model(w word, phrase string) wake.Model {
	return wake.Model{
		ID:        w.id(),
		Phrase:    phrase,
		Languages: languages(w.Locale),
		Kind:      wake.KindPryon,
		Path:      filepath.Join(c.dir, "models", filepath.FromSlash(w.Dir), manifest),
		Keyword:   w.Keyword,
	}
}

func (c *Catalog) Ensure(ctx context.Context, id string, progress func(float32)) error {
	if !c.on() {
		return fmt.Errorf("pryon: no model %q", id)
	}
	for _, w := range published.Words {
		if w.id() == id {
			return c.ensure(ctx, w, progress)
		}
	}
	return fmt.Errorf("pryon: no model %q", id)
}

func (c *Catalog) ensure(ctx context.Context, w word, progress func(float32)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var need []asset
	if !exists(c.Library()) {
		need = append(need, published.Library)
	}
	if !exists(c.model(w, "").Path) {
		need = append(need, w.Archive)
	}
	var total, before int64
	for _, a := range need {
		total += a.Size
	}
	for _, a := range need {
		report := func(done int64) { progress(float32(before+done) / float32(total)) }
		if err := c.unpack(ctx, a, report); err != nil {
			return err
		}
		before += a.Size
	}
	return nil
}

func (c *Catalog) unpack(ctx context.Context, a asset, report func(int64)) error {
	url, err := releases.File(folder, a.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("pryon: %w", err)
	}
	archive := filepath.Join(c.dir, a.Name)
	defer os.Remove(archive)
	if err := releases.DownloadProgress(ctx, url, a.Sum, archive, a.Size, report); err != nil {
		return fmt.Errorf("pryon: %s: %w", a.Name, err)
	}
	return extract(archive, c.dir)
}

func extract(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("pryon: %s: %w", archive, err)
	}
	t := tar.NewReader(r)
	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("pryon: %s: %w", archive, err)
		}
		if !filepath.IsLocal(h.Name) {
			return fmt.Errorf("pryon: %s: entry %q leaves the archive", archive, h.Name)
		}
		dest := filepath.Join(dir, h.Name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := write(dest, t); err != nil {
				return err
			}
		}
	}
}

func write(dest string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		return errors.Join(err, out.Close(), os.Remove(tmp))
	}
	if err := out.Close(); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return os.Rename(tmp, dest)
}

func ID(locale, name string) string {
	id := prefix + slug(name)
	if !strings.EqualFold(locale, "en-US") {
		id += "_" + slug(locale)
	}
	return id
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
			b.WriteByte('_')
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}

func label(locale, name string) string {
	l := "Pryon: " + phrase(name)
	if !strings.EqualFold(locale, "en-US") {
		l += " (" + locale + ")"
	}
	return l
}

func languages(locale string) []string {
	lang, _, _ := strings.Cut(locale, "-")
	if len(lang) < 2 || len(lang) > 3 || lang == "mul" {
		return nil
	}
	return []string{lang}
}

func phrase(name string) string {
	parts := strings.Split(name, ",")
	for i, p := range parts {
		words := strings.Fields(strings.ReplaceAll(strings.ToLower(p), "_", " "))
		for j, w := range words {
			words[j] = strings.ToUpper(w[:1]) + w[1:]
		}
		parts[i] = strings.Join(words, " ")
	}
	return strings.Join(parts, ", ")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
