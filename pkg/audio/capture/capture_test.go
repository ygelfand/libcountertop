package capture

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestClosedSourceFinishesAndCancellationPropagates(t *testing.T) {
	frames := make(chan []int16)
	close(frames)
	if err := Listen(
		t.Context(),
		time.Hour,
		frames,
		func([]int16) { t.Fatal("closed source delivered a frame") },
	); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Listen(
		ctx,
		time.Hour,
		make(chan []int16),
		func([]int16) {},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled capture=%v", err)
	}
}

func TestEventsOwnBorrowedBuffers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	borrowed := []int16{1, 2}
	frames, err := Events(
		ctx,
		time.Hour,
		func(receive func([]int16)) func() { receive(borrowed); borrowed[0] = 9; cancel(); return func() {} },
		func(s []int16) []int16 { return slices.Clone(s) },
	)
	if !errors.Is(err, context.Canceled) || len(frames) != 1 || frames[0][0] != 1 {
		t.Fatalf("frames=%v err=%v", frames, err)
	}
}

func TestWAVKeepsExplicitChannelOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "five.wav")
	samples := []int16{1, 2, 3, 4, 5, -1, -2, -3, -4, -5}
	if err := Write(path, samples, 16000, 5); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(data[22:]) != 5 ||
		binary.LittleEndian.Uint32(data[24:]) != 16000 {
		t.Fatal("WAV geometry changed")
	}
	for i, want := range samples {
		if got := int16(binary.LittleEndian.Uint16(data[44+2*i:])); got != want {
			t.Fatalf("channel sample %d=%d, want %d", i, got, want)
		}
	}
}

func TestReadGivesBackWhatWriteWrote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two.wav")
	samples := []int16{7, -7, 32767, -32768}
	if err := Write(path, samples, 16000, 2); err != nil {
		t.Fatal(err)
	}
	got, rate, channels, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 16000 || channels != 2 || len(got) != len(samples) {
		t.Fatalf("rate %d channels %d samples %d", rate, channels, len(got))
	}
	for i := range samples {
		if got[i] != samples[i] {
			t.Fatalf("sample %d = %d, want %d", i, got[i], samples[i])
		}
	}
	if err := os.WriteFile(path, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Read(path); err == nil {
		t.Error("read a file that is not WAV")
	}
}
