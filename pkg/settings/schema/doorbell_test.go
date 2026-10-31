package schema

import "testing"

func TestMigrateDoorbellPicksAVideoForEveryDoor(t *testing.T) {
	cases := []struct {
		name string
		door DoorbellDoor
		want DoorbellVideo
	}{
		{"unset stays unset", DoorbellDoor{}, VideoAuto},
		{"old rtsp was ONVIF", DoorbellDoor{Host: "10.0.0.9", Video: VideoRTSP}, VideoONVIF},
		{"rtsp with a URL stays", DoorbellDoor{RTSP: "rtsp://h/s", Video: VideoRTSP}, VideoRTSP},
		{"automatic with a URL", DoorbellDoor{RTSP: "rtsp://h/s"}, VideoRTSP},
		{"automatic with a password", DoorbellDoor{UID: "95270", Password: "pw"}, VideoReolink},
		{
			"automatic without one",
			DoorbellDoor{Device: "abc", Camera: "camera.front"},
			VideoAssistant,
		},
		{"a choice stays", DoorbellDoor{UID: "95270", Video: VideoAssistant}, VideoAssistant},
	}
	for _, c := range cases {
		var bell Doorbell
		bell.Doors[0] = c.door
		if err := MigrateDoorbell(nil, &bell); err != nil {
			t.Fatal(err)
		}
		if got := bell.Doors[0].Video; got != c.want {
			t.Errorf("%s: video %q, want %q", c.name, got, c.want)
		}
	}
}
