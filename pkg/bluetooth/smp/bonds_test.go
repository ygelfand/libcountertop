package smp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBondsKeepKeysPrivatelyAcrossOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "bonds.json")
	k := Keys{
		LTK:         [16]byte{9},
		EDIV:        7,
		Size:        16,
		Secure:      true,
		HasIdentity: true,
		Identity:    [6]byte{1, 2},
	}

	if err := NewBonds(path).Put(0xE2F816DD5E68, k); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("bonds file mode %v, want 0600", info.Mode().Perm())
	}

	b := NewBonds(path)
	got, ok := b.Get(0xE2F816DD5E68)
	if !ok || got != k {
		t.Fatalf("got %+v %v, want %+v", got, ok, k)
	}
	if err := b.Remove(0xE2F816DD5E68); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Get(0xE2F816DD5E68); ok {
		t.Fatal("a removed bond is still there")
	}
}

func TestAnUnreadableBondsFileIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bonds.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := NewBonds(path)
	if _, ok := b.Get(1); ok {
		t.Fatal("a bond came out of a broken file")
	}
	if err := b.Put(1, Keys{Size: 16}); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Get(1); !ok {
		t.Fatal("the bond did not survive")
	}
}
