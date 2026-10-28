package screencmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/display/panel"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
)

type savedFrames struct {
	frames []surface.Encoded
}

func (f *savedFrames) Read() (surface.Encoded, error) {
	if len(f.frames) == 0 {
		return surface.Encoded{}, io.EOF
	}
	frame := f.frames[0]
	f.frames = f.frames[1:]
	return frame, nil
}

func (*savedFrames) Close() error { return nil }

func TestRecordingWritesConfigAndFrames(t *testing.T) {
	stream := &savedFrames{frames: []surface.Encoded{
		{Data: []byte{0, 0, 0, 1, 0x67}, Flags: 2},
		{Data: []byte{0, 0, 0, 1, 0x65}, Flags: 1},
	}}
	var out bytes.Buffer
	n, err := recordTo(context.Background(), &out, stream)
	if n != 1 || !errors.Is(err, io.EOF) {
		t.Fatalf("recording = %d, %v", n, err)
	}
	if !bytes.Equal(out.Bytes(), []byte{0, 0, 0, 1, 0x67, 0, 0, 0, 1, 0x65}) {
		t.Fatalf("H.264 = %x", out.Bytes())
	}
}

func TestRecordingRejectsInvalidDurationBeforeGettingDisplay(t *testing.T) {
	for _, seconds := range []string{"0", "-1", "61", "oops"} {
		t.Run(seconds, func(t *testing.T) {
			cmd := Record(func() *panel.Driver { t.Fatal("display requested"); return nil })
			cmd.SetArgs([]string{seconds, "screen.h264"})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if cmd.Execute() == nil {
				t.Fatal("invalid duration accepted")
			}
		})
	}
}

func TestRecordingHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := recordTo(ctx, io.Discard, &savedFrames{})
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("recording = %d, %v", n, err)
	}
}
