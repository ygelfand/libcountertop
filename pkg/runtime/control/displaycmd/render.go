package displaycmd

import (
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/analysis"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/runtime/control/screencmd"
)

const (
	beatEvery = 500 * time.Millisecond
	thumbLong = 384
)

func (d Dependencies) landscape() (w, h int, err error) {
	w, h = d.Panel().Native()
	if w == 0 || h == 0 {
		return 0, 0, errors.New("there is no panel to size the picture by")
	}
	return max(w, h), min(w, h), nil
}

func (d Dependencies) thumbs(args []string) (string, error) {
	dir := d.Thumbs
	if len(args) > 0 {
		dir = args[0]
	}
	w, h, err := d.landscape()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	shapes := []struct {
		suffix       string
		w, h, tw, th int
	}{
		{"", w, h, thumbLong, thumbLong * h / w},
		{"-portrait", h, w, thumbLong * h / w, thumbLong},
	}
	n := 0
	for _, k := range visual.Built() {
		for _, sh := range shapes {
			img, err := screencmd.Render(d.GPU(), k, sh.w, sh.h, screencmd.ThumbFrames, d.Label)
			if err != nil {
				return "", fmt.Errorf("%s %dx%d: %w", k, sh.w, sh.h, err)
			}
			f, err := os.Create(filepath.Join(dir, string(k)+sh.suffix+".jpg"))
			if err != nil {
				return "", err
			}
			err = jpeg.Encode(f, screencmd.Shrink(img, sh.tw, sh.th), &jpeg.Options{Quality: 82})
			f.Close()
			if err != nil {
				return "", err
			}
			n++
		}
	}
	return fmt.Sprintf("wrote %d thumbnails to %s", n, dir), nil
}

func (d Dependencies) render(args []string) (string, error) {
	w, h := d.Panel().Size()
	frames := screencmd.ThumbFrames
	var err error
	if len(args) >= 4 {
		if w, err = strconv.Atoi(args[2]); err != nil {
			return "", fmt.Errorf("render: %q is not a width", args[2])
		}
		if h, err = strconv.Atoi(args[3]); err != nil {
			return "", fmt.Errorf("render: %q is not a height", args[3])
		}
	}
	if len(args) >= 5 {
		if frames, err = strconv.Atoi(args[4]); err != nil {
			return "", fmt.Errorf("render: %q is not a number of frames", args[4])
		}
	}
	img, err := screencmd.Render(d.GPU(), visual.Kind(args[0]), w, h, frames, d.Label)
	if err != nil {
		return "", err
	}
	f, err := os.Create(args[1])
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return fmt.Sprintf("rendered %s at %dx%d to %s", args[0], w, h, args[1]), nil
}

func (d Dependencies) wake(args []string) (string, error) {
	share, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return "", fmt.Errorf("wake: %q is not a share from 0 to 1", args[1])
	}
	w, h := d.Panel().Size()
	img, err := screencmd.RenderWake(
		d.GPU(),
		visual.Kind("wake-"+args[0]),
		w,
		h,
		share,
		time.Now().Format("3:04"),
	)
	if err != nil {
		return "", err
	}
	f, err := os.Create(args[2])
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return fmt.Sprintf("rendered wake %s at %.2f, %dx%d, to %s", args[0], share, w, h, args[2]), nil
}

func (d Dependencies) clip(args []string) (string, error) {
	w, h, seconds, fps := 480, 300, 3, 12
	nums := []*int{&w, &h, &seconds, &fps}
	for i, a := range args[2:] {
		n, err := strconv.Atoi(a)
		if err != nil || n <= 0 {
			return "", fmt.Errorf("clip: %q is not a positive number", a)
		}
		*nums[i] = n
	}
	rw, rh, err := d.landscape()
	if err != nil {
		return "", err
	}
	if h > w {
		rw, rh = rh, rw
	}
	if err := os.MkdirAll(args[1], 0o755); err != nil {
		return "", err
	}
	l, err := d.GPU().OpenOffscreen(rw, rh)
	if err != nil {
		return "", err
	}
	defer l.Close()
	v := visual.New(visual.Kind(args[0]))
	step := time.Second / time.Duration(fps)
	frames := seconds * fps
	x := visual.Input{Dt: step, Label: d.Label}
	for i := range screencmd.ThumbFrames + frames {
		x.Now = time.Duration(i+1) * step
		x.Mic, x.Speaker = beat(x.Now), beat(x.Now+step/2)
		x.Replying = i >= screencmd.ThumbFrames+frames/2
		if err := v.Shade(l, i == 0, ui.Rect{W: rw, H: rh}, x); err != nil {
			return "", err
		}
		if i < screencmd.ThumbFrames {
			continue
		}
		img, err := l.Read()
		if err != nil {
			return "", err
		}
		f, err := os.Create(
			filepath.Join(args[1], fmt.Sprintf("%03d.png", i-screencmd.ThumbFrames)),
		)
		if err != nil {
			return "", err
		}
		err = png.Encode(f, screencmd.Shrink(img, w, h))
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("wrote %d frames of %s at %dx%d to %s", frames, args[0], w, h, args[1]), nil
}

func beat(at time.Duration) analysis.Analysis {
	t := at.Seconds()
	phase := math.Mod(t, beatEvery.Seconds())
	kick := math.Exp(-phase / 0.12)
	var a analysis.Analysis
	a.Level = float32(0.12 + 0.55*kick)
	a.Peak = float32(min(1, 0.3+0.7*kick))
	if phase < 0.05 {
		a.Onsets = 1
	}
	for b := range a.Bands {
		low := math.Exp(-float64(b) / 6)
		a.Bands[b] = float32(0.15 + 0.6*kick*low + 0.25*(0.5+0.5*math.Sin(t*2.1+float64(b)*0.5)))
	}
	for i := range a.Wave {
		p := 2 * math.Pi * float64(i) / float64(len(a.Wave))
		a.Wave[i] = float32((0.25 + 0.6*kick) * (0.6*math.Sin(3*p+t*4) + 0.3*math.Sin(7*p+t*9)))
	}
	return a
}
