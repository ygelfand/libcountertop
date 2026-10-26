package schema

import "testing"

func TestPhotosMigrateToImmich(t *testing.T) {
	for _, c := range []struct {
		name string
		data string
		want Photos
	}{
		{
			"old poster settings",
			`{"poster":{"server":"http://immich","key":"k","albums":"Kitchen","tags":"cats"}}`,
			Photos{Immich: Immich{
				Enabled: true, Server: "http://immich", Key: "k", Albums: "Kitchen", Tags: "cats",
			}},
		},
		{
			"flat photos settings",
			`{"photos":{"server":"http://flat","key":"k","people":"Sam","favorites":true,"memories":false}}`,
			Photos{Immich: Immich{
				Enabled: true, Server: "http://flat", Key: "k", People: "Sam", Favorites: true,
			}},
		},
		{
			"flat photos without memories keeps the default",
			`{"photos":{"server":"http://flat"}}`,
			Photos{Immich: Immich{Enabled: true, Server: "http://flat", Memories: true}},
		},
		{
			"already nested",
			`{"photos":{"immich":{"server":"http://new"}},"poster":{"server":"http://old"}}`,
			Photos{},
		},
		{"nothing to move", `{"poster":{"enabled":true}}`, Photos{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got Photos
			if err := MigratePhotos([]byte(c.data), &got); err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
