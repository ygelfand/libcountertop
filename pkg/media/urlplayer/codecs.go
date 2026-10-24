package urlplayer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/jfreymuth/vorbis"
)

const (
	codecOpusName   = "A_OPUS"
	codecAACName    = "A_AAC"
	codecMP3Name    = "A_MPEG/L3"
	codecFLACName   = "A_FLAC"
	codecVorbisName = "A_VORBIS"
	pipedHeld       = 64
)

type frames func(data []byte) ([]int16, error)

func audioFrames(ctx context.Context, codec string, private []byte, rate int) (frames, int, error) {
	switch codec {
	case codecOpusName:
		return opusFrames()
	case codecAACName:
		return aacFrames(private)
	case codecVorbisName:
		return vorbisFrames(private)
	case codecMP3Name:
		return pipedFrames(ctx, nil, mp3Decoder), rate, nil
	case codecFLACName:
		return pipedFrames(
			ctx,
			private,
			func(r io.Reader) (decoder, error) { return flacDecoder(r) },
		), rate, nil
	}
	return nil, 0, fmt.Errorf("%w: sound %s", errUnplayable, codec)
}

func vorbisFrames(private []byte) (frames, int, error) {
	headers, err := xiphPackets(private)
	if err != nil {
		return nil, 0, err
	}
	var dec vorbis.Decoder
	for _, h := range headers {
		if err := dec.ReadHeader(h); err != nil {
			return nil, 0, fmt.Errorf("urlplayer: vorbis: %w", err)
		}
	}
	channels := dec.Channels()
	return func(data []byte) ([]int16, error) {
		samples, err := dec.Decode(data)
		if err != nil {
			return nil, err
		}
		return stereoFloat(samples, channels), nil
	}, dec.SampleRate(), nil
}

func xiphPackets(b []byte) ([][]byte, error) {
	if len(b) < 1 {
		return nil, errors.New("urlplayer: vorbis: no setup headers")
	}
	count, b := int(b[0])+1, b[1:]
	sizes := make([]int, count-1)
	for i := range sizes {
		for {
			if len(b) == 0 {
				return nil, errors.New("urlplayer: vorbis: setup headers cut short")
			}
			sizes[i] += int(b[0])
			last := b[0] < 255
			b = b[1:]
			if last {
				break
			}
		}
	}
	out := make([][]byte, 0, count)
	for _, n := range sizes {
		if n > len(b) {
			return nil, errors.New("urlplayer: vorbis: setup headers cut short")
		}
		out = append(out, b[:n])
		b = b[n:]
	}
	return append(out, b), nil
}

func stereoFloat(samples []float32, channels int) []int16 {
	if channels < 1 {
		return nil
	}
	frames := len(samples) / channels
	out := make([]int16, 2*frames)
	right := min(1, channels-1)
	for i := range frames {
		out[2*i] = clip16(samples[i*channels])
		out[2*i+1] = clip16(samples[i*channels+right])
	}
	return out
}

func clip16(v float32) int16 {
	return int16(math.Max(-1, math.Min(1, float64(v))) * math.MaxInt16)
}

func pipedFrames(ctx context.Context, prefix []byte, open func(io.Reader) (decoder, error)) frames {
	pr, pw := io.Pipe()
	out := make(chan []int16, pipedHeld)
	context.AfterFunc(ctx, func() { _ = pw.Close() })
	go func() {
		defer close(out)
		dec, err := open(pr)
		if err != nil {
			_ = pr.CloseWithError(err)
			return
		}
		for {
			pcm, err := dec.next()
			if err != nil {
				_ = pr.CloseWithError(err)
				return
			}
			if len(pcm) == 0 {
				continue
			}
			select {
			case out <- pcm:
			case <-ctx.Done():
				return
			}
		}
	}()
	started := false
	return func(data []byte) ([]int16, error) {
		if !started && len(prefix) > 0 {
			if _, err := pw.Write(prefix); err != nil {
				return nil, err
			}
		}
		started = true
		if _, err := pw.Write(data); err != nil {
			return nil, err
		}
		var pcm []int16
		for {
			select {
			case p, ok := <-out:
				if !ok {
					return pcm, nil
				}
				pcm = append(pcm, p...)
			default:
				return pcm, nil
			}
		}
	}
}
