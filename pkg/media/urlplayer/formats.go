package urlplayer

import (
	"mime"
	"slices"
	"strconv"
	"strings"
)

type codec int

const (
	codecAVC codec = iota + 1
	codecHEVC
	codecAAC
	codecOpus
	codecMP3
	codecFLAC
	codecPCM
	codecVorbis
)

var containers = map[string][]codec{
	"audio/mpeg":       {codecMP3},
	"audio/mp3":        {codecMP3},
	"audio/aac":        {codecAAC},
	"audio/flac":       {codecFLAC},
	"audio/x-flac":     {codecFLAC},
	"audio/wav":        {codecPCM},
	"audio/wave":       {codecPCM},
	"audio/x-wav":      {codecPCM},
	"audio/mp4":        {codecAAC, codecOpus, codecMP3},
	"audio/x-m4a":      {codecAAC, codecOpus, codecMP3},
	"video/mp4":        {codecAVC, codecHEVC, codecAAC, codecOpus, codecMP3},
	"audio/webm":       {codecOpus, codecVorbis},
	"video/webm":       {codecAVC, codecAAC, codecOpus, codecVorbis},
	"audio/x-matroska": {codecAAC, codecOpus, codecMP3, codecFLAC, codecVorbis},
	"video/x-matroska": {
		codecAVC,
		codecHEVC,
		codecAAC,
		codecOpus,
		codecMP3,
		codecFLAC,
		codecVorbis,
	},
	"application/vnd.apple.mpegurl": {codecAVC, codecAAC},
	"application/x-mpegurl":         {codecAVC, codecAAC},
	"audio/mpegurl":                 {codecAAC},
	"audio/x-mpegurl":               {codecAAC},
}

func (p *Player) Plays(mimeType string, width, height, rate int) bool {
	kind, params, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return false
	}
	allowed, ok := containers[kind]
	if !ok {
		return false
	}
	video := strings.HasPrefix(kind, "video/")
	for name := range strings.SplitSeq(params["codecs"], ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		c, ok := parseCodec(name)
		if !ok || !slices.Contains(allowed, c) {
			return false
		}
		video = video || c == codecAVC || c == codecHEVC
	}
	if !video {
		return true
	}
	if !p.o.Screen {
		return false
	}
	if p.o.Target == nil {
		return true
	}
	limits := p.o.Target()
	if limits.Tallest > 0 && (height > limits.Tallest || width > limits.Tallest*16/9) {
		return false
	}
	return limits.Fastest <= 0 || rate <= limits.Fastest
}

func parseCodec(name string) (codec, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "avc1.") || strings.HasPrefix(lower, "avc3."):
		return codecAVC, avcProfile(lower[5:])
	case lower == "avc1" || lower == "avc3":
		return codecAVC, true
	case strings.HasPrefix(lower, "hvc1.") || strings.HasPrefix(lower, "hev1."):
		return codecHEVC, hevcProfile(lower[5:])
	case lower == "mp4a.40.2" || lower == "mp4a.40":
		return codecAAC, true
	case lower == "mp4a.40.34" || lower == "mp4a.6b" || lower == "mp4a.69" || lower == "mp3":
		return codecMP3, true
	case lower == "vorbis":
		return codecVorbis, true
	case lower == "opus":
		return codecOpus, true
	case lower == "flac":
		return codecFLAC, true
	case lower == "1":
		return codecPCM, true
	}
	return 0, false
}

func hevcProfile(rest string) bool {
	fields := strings.Split(rest, ".")
	if len(fields) < 2 {
		return false
	}
	idc, err := strconv.ParseUint(strings.TrimLeft(fields[0], "abc"), 10, 8)
	if err != nil {
		return false
	}
	flags, err := strconv.ParseUint(fields[1], 16, 32)
	if err != nil {
		return false
	}
	switch {
	case idc == hevcMain:
		return true
	case idc >= 2 && idc <= 11:
		return false
	}
	return flags&0b11 != 0
}

func avcProfile(rest string) bool {
	if len(rest) < 2 {
		return false
	}
	profile, err := strconv.ParseUint(rest[:2], 16, 8)
	if err != nil {
		return false
	}
	switch profile {
	case 66, 77, 88, 100:
		return true
	}
	return false
}
