package doorbell

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestCheckFollowsTheDoorsVideo(t *testing.T) {
	ctx := context.Background()

	pic, err := Check(
		ctx,
		schema.DoorbellDoor{Video: schema.VideoAssistant, Camera: "camera.front"},
	)
	if pic != nil || err != nil {
		t.Errorf("Home Assistant video: picture %d bytes, err %v; want neither", len(pic), err)
	}

	_, err = Check(ctx, schema.DoorbellDoor{Video: schema.VideoONVIF, Password: "secret"})
	if !errors.Is(err, errUnsupported) {
		t.Errorf("ONVIF video without a host: err %v, want %v", err, errUnsupported)
	}

	_, err = Check(ctx, schema.DoorbellDoor{Video: schema.VideoONVIF, Host: "10.0.0.9"})
	if !errors.Is(err, ErrNoPassword) {
		t.Errorf("ONVIF video without a password: err %v, want %v", err, ErrNoPassword)
	}

	_, err = Check(ctx, schema.DoorbellDoor{Video: schema.VideoRTSP})
	if !errors.Is(err, errUnsupported) {
		t.Errorf("RTSP video without a URL: err %v, want %v", err, errUnsupported)
	}
}

func TestAnRTSPURLSkipsONVIF(t *testing.T) {
	door := schema.DoorbellDoor{
		Video: schema.VideoRTSP,
		Host:  "10.0.0.9",
		RTSP:  "rtsp://user:pass@10.0.0.9:554/stream1",
	}
	if got := plannedVideo(door); got != sourceRTSP {
		t.Errorf("RTSP video plans %q, want %q", got, sourceRTSP)
	}
	uri, err := streamURI(context.Background(), door)
	if err != nil || uri != door.RTSP {
		t.Errorf("stream URI %q, err %v; want the door's URL", uri, err)
	}
}

type entities []homeassistant.Entity

func (e entities) ReolinkDevices(context.Context) ([]homeassistant.ReolinkDevice, error) {
	return nil, nil
}

func (e entities) Named(_ context.Context, domain string) ([]homeassistant.Named, error) {
	var out []homeassistant.Named
	for _, x := range e {
		if strings.HasPrefix(x.ID, domain+".") {
			out = append(out, homeassistant.Named{Entity: x.ID, Name: x.Name})
		}
	}
	return out, nil
}

func TestBinarySensorsListEveryCameraBrand(t *testing.T) {
	d := &Doorbell{o: Options{Finder: entities{
		{ID: "binary_sensor.carport_motion", Name: "Carport motion"},
		{ID: "binary_sensor.front_visitor", Name: "Front door visitor"},
		{ID: "binary_sensor.attic_window", Name: "Attic window"},
		{ID: "camera.carport", Name: "Carport"},
	}}}
	cams, err := d.Cameras(context.Background())
	if err != nil || len(cams) != 1 || cams[0].Entity != "camera.carport" {
		t.Errorf("cameras %v, err %v; want only camera.carport", cams, err)
	}
	got, err := d.BinarySensors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"binary_sensor.attic_window",
		"binary_sensor.carport_motion",
		"binary_sensor.front_visitor",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].Entity != w {
			t.Errorf("sensor %d is %s, want %s (sorted by name)", i, got[i].Entity, w)
		}
	}
}
