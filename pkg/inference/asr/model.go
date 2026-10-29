package asr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygelfand/libcountertop/pkg/network/releases"
	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
)

const (
	repo   = "https://github.com/ygelfand/libcountertop"
	tag    = "kroko-en-1"
	asset  = "kroko-en-q24-v1.bin"
	sum    = "52673e74be9b44948640c6652ce8fd5b2b1b1faa4a66f3f8e43105405a38ce9f"
	size   = 26651392
	prefix = "kroko-"
)

func Weights(dir string) string { return filepath.Join(dir, prefix+sum[:16]+".bin") }

func Have(dir string) bool {
	info, err := os.Stat(Weights(dir))
	return err == nil && info.Size() == size
}

func Fetch(ctx context.Context, dir string) (string, error) {
	path := Weights(dir)
	if Have(dir) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("asr: weights: %w", err)
	}
	if err := releases.Download(
		ctx,
		releases.Asset(repo, tag, asset),
		sum,
		path,
		size,
	); err != nil {
		return "", err
	}
	prune(dir, path)
	return path, nil
}

func Prepare(ctx context.Context, helper string, binary []byte, dir string) (Options, error) {
	if err := payload.Install(helper, binary); err != nil {
		return Options{}, fmt.Errorf("asr: helper: %w", err)
	}
	weights, err := Fetch(ctx, dir)
	if err != nil {
		return Options{}, err
	}
	return Options{Helper: helper, Weights: weights, Threads: Threads()}, nil
}

func prune(dir, keep string) {
	old, _ := filepath.Glob(filepath.Join(dir, prefix+"*.bin"))
	for _, p := range old {
		if p != keep {
			_ = os.Remove(p)
		}
	}
}
