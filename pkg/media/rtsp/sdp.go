package rtsp

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

type media struct {
	kind     string
	payload  int
	encoding string
	rate     int
	control  string
	config   []byte
	send     bool
	channel  int
	depack   *depacketizer

	started bool
	first   uint32
	wraps   int64
	last    uint32
}

func (m *media) codec() Codec {
	switch strings.ToUpper(m.encoding) {
	case "H264":
		return H264
	case "H265", "HEVC":
		return H265
	}
	return 0
}

func (m *media) clock(ts uint32) time.Duration {
	if !m.started {
		m.started, m.first, m.last = true, ts, ts
	}
	if ts < m.last && m.last-ts > 1<<31 {
		m.wraps++
	}
	m.last = ts
	ticks := m.wraps<<32 + int64(ts) - int64(m.first)
	rate := int64(max(m.rate, 1))
	return time.Duration(ticks) * time.Second / time.Duration(rate)
}

func parseSDP(body string) []media {
	var out []media
	var cur *media
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "m="):
			fields := strings.Fields(line[2:])
			out = append(out, media{})
			cur = &out[len(out)-1]
			if len(fields) > 0 {
				cur.kind = fields[0]
			}
			if len(fields) > 3 {
				cur.payload, _ = strconv.Atoi(fields[3])
				cur.encoding, cur.rate = staticPayload(cur.payload)
			}
		case cur == nil:
		case strings.HasPrefix(line, "a=rtpmap:"):
			id, rest, _ := strings.Cut(line[len("a=rtpmap:"):], " ")
			if n, _ := strconv.Atoi(id); n != cur.payload {
				continue
			}
			parts := strings.Split(rest, "/")
			cur.encoding = parts[0]
			if len(parts) > 1 {
				cur.rate, _ = strconv.Atoi(parts[1])
			}
		case strings.HasPrefix(line, "a=control:"):
			cur.control = line[len("a=control:"):]
		case strings.HasPrefix(line, "a=fmtp:"):
			_, params, _ := strings.Cut(line[len("a=fmtp:"):], " ")
			for p := range strings.SplitSeq(params, ";") {
				k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
				if strings.EqualFold(k, "config") {
					cur.config, _ = hex.DecodeString(v)
				}
			}
		case line == "a=sendonly":
			cur.send = true
		}
	}
	return out
}

func staticPayload(pt int) (string, int) {
	switch pt {
	case 0:
		return "PCMU", 8000
	case 8:
		return "PCMA", 8000
	}
	return "", 0
}
