package photos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Provider interface {
	Key() string
	Name() string
	Status(cfg schema.Photos) Status
	Same(a, b schema.Photos) bool
	Open(ctx context.Context, cfg schema.Photos) (Session, string, error)
}

type Session interface {
	Draw(ctx context.Context, now time.Time, low bool) (front, back []Found, err error)
	Fetch(ctx context.Context, id string) ([]byte, error)
}

type Found struct {
	ID     string
	Taken  string
	Place  string
	People []string
	Year   int
}

var ErrKey = errors.New("photos: the key was refused")

type TooOldError struct{ Version string }

func (e *TooOldError) Error() string { return fmt.Sprintf("photos: server %s is too old", e.Version) }

type MissingError struct{ Name string }

func (e *MissingError) Error() string { return fmt.Sprintf("photos: nothing named %q", e.Name) }

type PermissionError struct{ Name string }

func (e *PermissionError) Error() string { return fmt.Sprintf("photos: the key lacks %s", e.Name) }
