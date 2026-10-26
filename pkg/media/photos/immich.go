package photos

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/immich"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const deckSize = 250

type immichSource struct{}

func Immich() Provider { return immichSource{} }

func (immichSource) Key() string { return "immich" }

func (immichSource) Name() string { return say.T("photos.immich") }

func (immichSource) Status(cfg schema.Photos) Status {
	switch {
	case !cfg.Immich.Enabled:
		return Off
	case cfg.Immich.Server == "":
		return Unset
	}
	return Connecting
}

func (immichSource) Same(a, b schema.Photos) bool { return a.Immich == b.Immich }

func (immichSource) Open(ctx context.Context, cfg schema.Photos) (Session, string, error) {
	c := immich.New(cfg.Immich.Server, cfg.Immich.Key)
	if granted, err := c.KeyPermissions(ctx); err == nil {
		if lacking := lacks(granted, needs(cfg.Immich)); len(lacking) > 0 {
			return nil, "", &PermissionError{Name: strings.Join(lacking, ", ")}
		}
	} else if errors.Is(err, immich.ErrKey) {
		return nil, "", ErrKey
	}
	about, err := c.About(ctx)
	if err != nil {
		return nil, "", immichErr(err)
	}
	if !immich.Supported(about.Version) {
		return nil, about.Version, &TooOldError{Version: about.Version}
	}
	sel, err := resolve(ctx, c, cfg.Immich)
	if err != nil {
		return nil, about.Version, immichErr(err)
	}
	return &immichSession{
		c:        c,
		sel:      sel,
		memories: cfg.Immich.Memories,
		size:     immichSize(cfg.Immich.Quality),
	}, about.Version, nil
}

func immichSize(q schema.ImmichQuality) string {
	if q == schema.ImmichFullsize {
		return immich.Fullsize
	}
	return immich.Preview
}

type immichSession struct {
	c        *immich.Client
	sel      immich.Selection
	memories bool
	size     string
	day      string
}

func (s *immichSession) Draw(
	ctx context.Context,
	now time.Time,
	low bool,
) ([]Found, []Found, error) {
	var front, back []Found
	if today := now.Format(time.DateOnly); s.memories && s.day != today {
		found, err := s.c.Memories(ctx, now)
		if err != nil {
			return nil, nil, immichErr(err)
		}
		for _, m := range found {
			for _, a := range m.Assets {
				if a.Type == "" || a.Type == "IMAGE" {
					front = append(front, describe(a, m.Data.Year))
				}
			}
		}
		s.day = today
	}
	if low {
		found, err := s.c.Random(ctx, s.sel, deckSize)
		if err != nil {
			return nil, nil, immichErr(err)
		}
		for _, a := range found {
			back = append(back, describe(a, 0))
		}
	}
	return front, back, nil
}

func (s *immichSession) Fetch(ctx context.Context, id string) ([]byte, error) {
	data, err := s.c.Picture(ctx, id, s.size)
	return data, immichErr(err)
}

func needs(cfg schema.Immich) []string {
	out := []string{"server.about", "asset.read", "asset.view"}
	if cfg.Memories {
		out = append(out, "memory.read")
	}
	for _, f := range []struct {
		list, perm string
	}{{cfg.Albums, "album.read"}, {cfg.Tags, "tag.read"}, {cfg.People, "person.read"}} {
		if len(schema.Names(f.list)) > 0 {
			out = append(out, f.perm)
		}
	}
	return out
}

func lacks(granted, wanted []string) []string {
	if slices.Contains(granted, "all") {
		return nil
	}
	var out []string
	for _, w := range wanted {
		if !slices.Contains(granted, w) {
			out = append(out, w)
		}
	}
	return out
}

func immichErr(err error) error {
	var perm *immich.PermissionError
	switch {
	case errors.As(err, &perm):
		return &PermissionError{Name: perm.Permission}
	case errors.Is(err, immich.ErrKey):
		return ErrKey
	}
	return err
}

func resolve(ctx context.Context, c *immich.Client, cfg schema.Immich) (immich.Selection, error) {
	sel := immich.Selection{Favorites: cfg.Favorites}
	if names := schema.Names(cfg.Albums); len(names) > 0 {
		all, err := c.Albums(ctx)
		if err != nil {
			return sel, err
		}
		if sel.Albums, err = match(names, all, func(a immich.Album) (string, []string) {
			return a.ID, []string{a.Name}
		}); err != nil {
			return sel, err
		}
	}
	if names := schema.Names(cfg.Tags); len(names) > 0 {
		all, err := c.Tags(ctx)
		if err != nil {
			return sel, err
		}
		if sel.Tags, err = match(names, all, func(t immich.Tag) (string, []string) {
			return t.ID, []string{t.Name, t.Value}
		}); err != nil {
			return sel, err
		}
	}
	if names := schema.Names(cfg.People); len(names) > 0 {
		all, err := c.People(ctx)
		if err != nil {
			return sel, err
		}
		if sel.People, err = match(names, all, func(p immich.Person) (string, []string) {
			return p.ID, []string{p.Name}
		}); err != nil {
			return sel, err
		}
	}
	return sel, nil
}

func match[T any](wants []string, all []T, of func(T) (string, []string)) ([]string, error) {
	var ids []string
	for _, want := range wants {
		found := ""
		for _, item := range all {
			id, names := of(item)
			if id == want || slices.ContainsFunc(names, func(n string) bool {
				return strings.EqualFold(n, want)
			}) {
				found = id
				break
			}
		}
		if found == "" {
			return nil, &MissingError{Name: want}
		}
		ids = append(ids, found)
	}
	return ids, nil
}

func describe(a immich.Asset, year int) Found {
	f := Found{ID: a.ID, Year: year, Taken: a.Local}
	if a.Exif != nil {
		if a.Exif.Taken != "" {
			f.Taken = a.Exif.Taken
		}
		f.Place = place(a.Exif)
	}
	for _, p := range a.People {
		if p.Name != "" && !p.Hidden {
			f.People = append(f.People, p.Name)
		}
	}
	return f
}

func place(x *immich.Exif) string {
	var parts []string
	for _, s := range []string{x.City, x.Country} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}
