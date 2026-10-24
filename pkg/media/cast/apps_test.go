package cast

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestALaunchedAppIsReportedByTheNameItsConfigGives(t *testing.T) {
	configs := map[string]AppConfig{
		"233637DE": {Name: "YouTube", Icon: "https://example.com/youtube.png"},
		"C35B0678": {Name: "Music Assistant"},
	}
	for id, want := range map[string]string{
		"233637DE": "YouTube",
		"C35B0678": "Music Assistant",
		"DEADBEEF": "DEADBEEF",
	} {
		r := NewReceiver("Kitchen")
		r.Describe = func(app string) AppConfig { return configs[app] }
		r.start(id)
		if got := r.app.DisplayName; got != want {
			t.Errorf("%s runs as %q, want %q", id, got, want)
		}
		if got := r.app.IconURL; got != configs[id].Icon {
			t.Errorf("%s shows icon %q, want %q", id, got, configs[id].Icon)
		}
	}
}

func TestEveryAppIdIsEightUpperHexDigitsAndListedOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range Apps {
		if len(a.ID) != 8 || strings.Trim(a.ID, "0123456789ABCDEF") != "" {
			t.Errorf("%q is not an app id", a.ID)
		}
		if seen[a.ID] {
			t.Errorf("%s is listed twice", a.ID)
		}
		seen[a.ID] = true
		if len(a.Protocols) == 0 {
			t.Errorf("%s is listed without a protocol to route to", a.ID)
		}
	}
}

func names(r *Receiver, a App) []string {
	var out []string
	for _, n := range r.namespaces(a) {
		out = append(out, n.Name)
	}
	return out
}

type fake struct{}

func (fake) Name() string         { return "youtube" }
func (fake) Namespaces() []string { return []string{"urn:x-cast:fake"} }
func (fake) Receive(*Application, Message) ([]Message, error) {
	return nil, ErrUnspoken
}
func (fake) Run(context.Context) {}

func TestAnAppSpeaksMediaAndOnlyTheProtocolsRegistered(t *testing.T) {
	r := NewReceiver("Kitchen")
	for _, id := range []string{DefaultMediaReceiver, "C35B0678", "DEADBEEF", "2DB7CC49"} {
		if got := names(r, Lookup(id)); !slices.Equal(got, []string{NSMedia}) {
			t.Errorf("%s speaks %v with nothing registered", id, got)
		}
	}

	r.Register(fake{})
	if got := names(
		r,
		Lookup("2DB7CC49"),
	); !slices.Equal(
		got,
		[]string{NSMedia, "urn:x-cast:fake"},
	) {
		t.Errorf("YouTube Music speaks %v once its protocol is registered", got)
	}
	if got := names(r, Lookup("C35B0678")); !slices.Equal(got, []string{NSMedia}) {
		t.Errorf("an app that does not name the protocol speaks %v", got)
	}
}

var uuid = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestALaunchedAppIsReportedTheWayARealDeviceReportsIt(t *testing.T) {
	r := NewReceiver("Kitchen")
	r.Describe = func(app string) AppConfig {
		if app == "2DB7CC49" {
			return AppConfig{Name: "YouTube Music", Icon: "https://example.com/music.png"}
		}
		return AppConfig{}
	}
	r.start("2DB7CC49")
	a := r.app

	if !uuid.MatchString(a.SessionID) || a.TransportID != a.SessionID {
		t.Errorf("session %q transport %q", a.SessionID, a.TransportID)
	}
	if a.UniversalAppID != "2DB7CC49" || a.AppType != "WEB" || a.LaunchedFromCloud ||
		a.IsIdleScreen {
		t.Errorf("reported as %+v", a)
	}
	if a.IconURL == "" || a.StatusText != "YouTube Music" {
		t.Errorf("icon %q status %q", a.IconURL, a.StatusText)
	}

	r.start("C35B0678")
	if r.app.IconURL != "" {
		t.Errorf("an app with no icon reports %q", r.app.IconURL)
	}
	if r.app.SessionID == a.SessionID {
		t.Error("two launches share a session id")
	}
}

type bracketed struct {
	fake
	log *[]string
}

func (b bracketed) Name() string  { return "youtube" }
func (b bracketed) Started(a App) { *b.log = append(*b.log, "started "+a.ID) }
func (b bracketed) Ended(a App)   { *b.log = append(*b.log, "ended "+a.ID) }

func TestAProtocolHearsItsAppStartAndEnd(t *testing.T) {
	var log []string
	r := NewReceiver("Kitchen")
	r.Register(bracketed{log: &log})

	r.start("4A6A109A")
	r.release(IdleCancelled)
	r.start("233637DE")
	r.release(IdleCancelled)

	if want := []string{"started 233637DE", "ended 233637DE"}; !slices.Equal(log, want) {
		t.Errorf("heard %v, want %v", log, want)
	}
}
