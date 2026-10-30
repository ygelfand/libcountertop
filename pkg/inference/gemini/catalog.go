// Package gemini runs Google's "Hey Google" hotword model on the shared wake engine.
package gemini

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/network/releases"
)

const (
	ID = "gemini_hey_google"

	folder   = "gemini/1"
	packSum  = "4aeb53fb7b1872bf588ec20ac7bb7cbe727272120607b59014dfe39426ec7b61"
	packSize = 957360
	packName = "resources_hotword.pak"
)

type Catalog struct {
	dir string
	on  func() bool

	mu sync.Mutex
}

func NewCatalog(dir string, on func() bool) *Catalog { return &Catalog{dir: dir, on: on} }

func (c *Catalog) Models() []wake.Model {
	if !c.on() {
		return nil
	}
	return []wake.Model{{
		ID:        ID,
		Phrase:    "Gemini: Hey Google",
		Languages: []string{"en"},
		Kind:      wake.KindGemini,
		Path:      filepath.Join(c.dir, packName),
	}}
}

func (c *Catalog) Ensure(ctx context.Context, id string, progress func(float32)) error {
	if !c.on() || id != ID {
		return fmt.Errorf("gemini: no model %q", id)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	dest := filepath.Join(c.dir, packName)
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	url, err := releases.File(folder, packName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	report := func(done int64) { progress(float32(done) / packSize) }
	if err := releases.DownloadProgress(ctx, url, packSum, dest, packSize, report); err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	return nil
}
