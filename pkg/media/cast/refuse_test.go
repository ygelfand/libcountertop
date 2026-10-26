package cast

import (
	"encoding/json"
	"testing"
)

func TestAnUnavailableAppIsRefusedAtLaunch(t *testing.T) {
	r := NewReceiver("Kitchen")
	r.Available = func(app string) bool { return app != "CC32E753" }

	got := replyTo(t, r, from("sender-1", NSReceiver,
		`{"type":"LAUNCH","requestId":7,"appId":"CC32E753"}`))
	var refusal struct {
		Type      string `json:"type"`
		RequestID int    `json:"requestId"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(got.Payload), &refusal); err != nil {
		t.Fatal(err)
	}
	if refusal.Type != TypeLaunchFail || refusal.RequestID != 7 ||
		refusal.Reason != ReasonUnknownAppID {
		t.Errorf("refusal %+v", refusal)
	}
	if r.app != nil {
		t.Errorf("a refused launch started %s", r.app.AppID)
	}

	got = replyTo(t, r, from("sender-1", NSReceiver,
		`{"type":"LAUNCH","requestId":8,"appId":"233637DE"}`))
	if r.app == nil || r.app.AppID != "233637DE" {
		t.Errorf("an available app did not launch: %s", got.Payload)
	}
}
