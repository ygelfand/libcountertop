package rtsp

const (
	h264STAPA = 24
	h264FUA   = 28
	h265AP    = 48
	h265FU    = 49
	h265IRAPs = 16
	h265IRAPe = 21
	maxUnit   = 8 << 20
)

var startCode = []byte{0, 0, 0, 1}

type depacketizer struct {
	codec Codec
	au    []byte
	key   bool
	frag  []byte
}

func (d *depacketizer) push(payload []byte, marker bool) ([]byte, bool, bool) {
	switch d.codec {
	case H264:
		d.h264(payload)
	case H265:
		d.h265(payload)
	default:
		return nil, false, false
	}
	if len(d.au) > maxUnit {
		d.au, d.key, d.frag = nil, false, nil
	}
	if !marker || len(d.au) == 0 {
		return nil, false, false
	}
	au, key := d.au, d.key
	d.au, d.key = nil, false
	return au, key, true
}

func (d *depacketizer) nal(n []byte) {
	if len(n) == 0 {
		return
	}
	switch d.codec {
	case H264:
		if n[0]&0x1f == nalIDR {
			d.key = true
		}
	case H265:
		if t := (n[0] >> 1) & 0x3f; t >= h265IRAPs && t <= h265IRAPe {
			d.key = true
		}
	}
	d.au = append(d.au, startCode...)
	d.au = append(d.au, n...)
}

func (d *depacketizer) h264(p []byte) {
	if len(p) == 0 {
		return
	}
	switch p[0] & 0x1f {
	case h264STAPA:
		for rest := p[1:]; len(rest) >= 2; {
			n := int(rest[0])<<8 | int(rest[1])
			if len(rest) < 2+n {
				return
			}
			d.nal(rest[2 : 2+n])
			rest = rest[2+n:]
		}
	case h264FUA:
		if len(p) < 2 {
			return
		}
		start, end := p[1]&0x80 != 0, p[1]&0x40 != 0
		if start {
			d.frag = append(d.frag[:0], p[0]&0xe0|p[1]&0x1f)
		}
		if len(d.frag) == 0 {
			return
		}
		d.frag = append(d.frag, p[2:]...)
		if end {
			d.nal(d.frag)
			d.frag = d.frag[:0]
		}
	default:
		d.nal(p)
	}
}

func (d *depacketizer) h265(p []byte) {
	if len(p) < 2 {
		return
	}
	switch (p[0] >> 1) & 0x3f {
	case h265AP:
		for rest := p[2:]; len(rest) >= 2; {
			n := int(rest[0])<<8 | int(rest[1])
			if len(rest) < 2+n {
				return
			}
			d.nal(rest[2 : 2+n])
			rest = rest[2+n:]
		}
	case h265FU:
		if len(p) < 3 {
			return
		}
		start, end := p[2]&0x80 != 0, p[2]&0x40 != 0
		if start {
			t := p[2] & 0x3f
			d.frag = append(d.frag[:0], p[0]&0x81|t<<1, p[1])
		}
		if len(d.frag) == 0 {
			return
		}
		d.frag = append(d.frag, p[3:]...)
		if end {
			d.nal(d.frag)
			d.frag = d.frag[:0]
		}
	default:
		d.nal(p)
	}
}
