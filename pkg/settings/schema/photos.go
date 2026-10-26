package schema

import (
	"encoding/json"
	"strings"
)

type Photos struct {
	AppEvery  Delay           `json:"app_every"`
	AppFit    PhotoFit        `json:"app_fit"`
	AppChange PhotoTransition `json:"app_transition"`
	Immich    Immich          `json:"immich"`
	Google    Google          `json:"google"`
}

const (
	GoogleAlbums      = 4
	GoogleWeightMost  = 10
	GoogleWeightStart = 5
)

type Google struct {
	Enabled bool                      `json:"enabled"`
	Albums  [GoogleAlbums]GoogleAlbum `json:"albums"`
}

type GoogleAlbum struct {
	URL    string `json:"url"`
	Weight int    `json:"weight"`
}

type Immich struct {
	Enabled   bool          `json:"enabled"`
	Server    string        `json:"server"`
	Key       string        `json:"key"`
	Albums    string        `json:"albums"`
	Tags      string        `json:"tags"`
	People    string        `json:"people"`
	Favorites bool          `json:"favorites"`
	Memories  bool          `json:"memories"`
	Quality   ImmichQuality `json:"quality"`
}

func DefaultPhotos() Photos {
	p := Photos{
		AppEvery:  Delay15s,
		AppFit:    FitKenBurns,
		AppChange: TransitionFade,
		Immich:    Immich{Memories: true, Quality: ImmichPreview},
	}
	for i := range p.Google.Albums {
		p.Google.Albums[i].Weight = GoogleWeightStart
	}
	return p
}

func PhotoAppSpans() []Delay {
	return []Delay{Delay5s, Delay10s, Delay15s, Delay30s, Delay1m, Delay2m, Delay5m, DelayNever}
}

func Names(list string) []string {
	var out []string
	for s := range strings.SplitSeq(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func MigratePhotos(data []byte, photos *Photos) error {
	var old struct {
		Photos *struct {
			Immich    *json.RawMessage `json:"immich"`
			Server    string           `json:"server"`
			Key       string           `json:"key"`
			Albums    string           `json:"albums"`
			Tags      string           `json:"tags"`
			People    string           `json:"people"`
			Favorites bool             `json:"favorites"`
			Memories  *bool            `json:"memories"`
		} `json:"photos"`
		Poster struct {
			Server string `json:"server"`
			Key    string `json:"key"`
			Albums string `json:"albums"`
			Tags   string `json:"tags"`
		} `json:"poster"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		return err
	}
	switch p := old.Photos; {
	case p == nil:
		if old.Poster.Server == "" {
			return nil
		}
		photos.Immich.Enabled = true
		photos.Immich.Server, photos.Immich.Key = old.Poster.Server, old.Poster.Key
		photos.Immich.Albums, photos.Immich.Tags = old.Poster.Albums, old.Poster.Tags
	case p.Immich == nil && p.Server != "":
		photos.Immich = Immich{
			Enabled:   true,
			Server:    p.Server,
			Key:       p.Key,
			Albums:    p.Albums,
			Tags:      p.Tags,
			People:    p.People,
			Favorites: p.Favorites,
			Memories:  p.Memories == nil || *p.Memories,
		}
	}
	return nil
}
