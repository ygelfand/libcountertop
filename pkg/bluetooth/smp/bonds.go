package smp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

type Bonds struct {
	mu   sync.Mutex
	path string
}

func NewBonds(path string) *Bonds { return &Bonds{path: path} }

func (b *Bonds) Get(address uint64) (Keys, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	all, err := b.load()
	if err != nil {
		return Keys{}, false
	}
	k, ok := all[key(address)]
	return k, ok
}

func (b *Bonds) Put(address uint64, k Keys) error {
	return b.change(func(all map[string]Keys) { all[key(address)] = k })
}

func (b *Bonds) Remove(address uint64) error {
	return b.change(func(all map[string]Keys) { delete(all, key(address)) })
}

func (b *Bonds) load() (map[string]Keys, error) {
	data, err := os.ReadFile(b.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Keys{}, nil
	}
	if err != nil {
		return nil, err
	}
	all := map[string]Keys{}
	return all, json.Unmarshal(data, &all)
}

func (b *Bonds) change(f func(map[string]Keys)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	all, err := b.load()
	if err != nil {
		all = map[string]Keys{}
	}
	f(all)
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

func key(address uint64) string { return fmt.Sprintf("%012X", address) }
