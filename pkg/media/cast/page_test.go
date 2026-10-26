package cast

import "testing"

func TestPagesFillTheShellsPlaceholders(t *testing.T) {
	for _, c := range []struct {
		url, post, want string
	}{
		{
			"https://r.example/app?display_name=${URL_ENCODED_FRIENDLY_NAME}",
			"",
			"https://r.example/app?display_name=Living+Room+%26+Den",
		},
		{"https://r.example/${FRIENDLY_NAME}", "", "https://r.example/Living Room & Den"},
		{
			"https://r.example/error?code=${POST_DATA}&u=${URL_ENCODED_POST_DATA}",
			"a b",
			"https://r.example/error?code=a b&u=a+b",
		},
		{"https://r.example/plain", "", "https://r.example/plain"},
	} {
		got := AppConfig{URL: c.url}.Page("Living Room & Den", c.post)
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.url, got, c.want)
		}
	}
}
