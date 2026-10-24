package engine

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

func unpack(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	defer f.Close()
	zr, err := xz.NewReader(bufio.NewReader(f))
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	return untar(tar.NewReader(zr), dest)
}

func untar(tr *tar.Reader, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("engine: reading archive: %w", err)
		}
		name := filepath.Clean(h.Name)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("engine: archive entry %q leaves the install directory", h.Name)
		}
		_, rel, _ := strings.Cut(name, string(filepath.Separator))
		if rel == "" {
			continue
		}
		path := filepath.Join(dest, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("engine: %w", err)
			}
		case tar.TypeReg:
			if err := write(path, tr, os.FileMode(h.Mode)&0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if !filepath.IsLocal(filepath.Join(filepath.Dir(rel), h.Linkname)) {
				return fmt.Errorf(
					"engine: archive link %q points outside the install directory",
					h.Name,
				)
			}
			if err := os.Symlink(h.Linkname, path); err != nil {
				return fmt.Errorf("engine: %w", err)
			}
		}
	}
}

func write(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode|0o400)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		return errors.Join(fmt.Errorf("engine: %w", err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	return nil
}
