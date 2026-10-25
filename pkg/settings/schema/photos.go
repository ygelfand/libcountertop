package schema

import (
	"encoding/json"
	"strings"
)

type Photos struct {
	Server    string `json:"server"`
	Key       string `json:"key"`
	Google    string `json:"google"`
	Albums    string `json:"albums"`
	Tags      string `json:"tags"`
	People    string `json:"people"`
	Favorites bool   `json:"favorites"`
	Memories  bool   `json:"memories"`
	Interval  Delay  `json:"interval"`
	Motion    Motion `json:"motion"`
}

type Motion struct {
	Dashboard bool `json:"dashboard"`
	Saver     bool `json:"saver"`
	App       bool `json:"app"`
}

func DefaultPhotos() Photos {
	return Photos{
		Memories: true,
		Interval: Delay30s,
		Motion:   Motion{Saver: true, App: true},
	}
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

func PhotoIntervals() []Delay {
	return []Delay{Delay10s, Delay15s, Delay30s, Delay1m, Delay2m, Delay5m}
}

func MigratePhotos(data []byte, photos *Photos) error {
	var old struct {
		Photos *json.RawMessage `json:"photos"`
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
	if old.Photos != nil || old.Poster.Server == "" {
		return nil
	}
	photos.Server, photos.Key = old.Poster.Server, old.Poster.Key
	photos.Albums, photos.Tags = old.Poster.Albums, old.Poster.Tags
	return nil
}
