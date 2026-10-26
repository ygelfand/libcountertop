package rtsp

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestTheClientPlaysWhatTheServerSends(t *testing.T) {
	_, st, addr := serve(t)
	st.Write(annexB(sps, pps, slice(nalIDR, 100)), 0)

	units := make(chan Unit, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, "rtsp://"+addr+"/main", Options{Video: func(u Unit) { units <- u }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go func() { _ = c.Run(ctx) }()

	big := slice(nalIDR, 4000)
	go func() {
		for i := 1; ctx.Err() == nil; i++ {
			st.Write(annexB(big), time.Duration(i)*40*time.Millisecond)
			time.Sleep(20 * time.Millisecond)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("no fragmented keyframe arrived")
		case u := <-units:
			if u.Codec != H264 || !u.Key {
				continue
			}
			if bytes.Contains(u.Data, big) {
				return
			}
		}
	}
}

func TestH265FragmentsReassemble(t *testing.T) {
	idr := []byte{19 << 1, 1, 0xaa, 0xbb, 0xcc, 0xdd}
	d := &depacketizer{codec: H265}
	fu := func(start, end bool, body ...byte) []byte {
		h := byte(19)
		if start {
			h |= 0x80
		}
		if end {
			h |= 0x40
		}
		return append([]byte{h265FU << 1, 1, h}, body...)
	}
	if _, _, done := d.push(fu(true, false, 0xaa, 0xbb), false); done {
		t.Fatal("a unit finished before its last fragment")
	}
	au, key, done := d.push(fu(false, true, 0xcc, 0xdd), true)
	if !done || !key {
		t.Fatalf("done %v key %v, want a finished keyframe", done, key)
	}
	if want := append([]byte{0, 0, 0, 1}, idr...); !bytes.Equal(au, want) {
		t.Fatalf("got % x, want % x", au, want)
	}
}

func TestDescriptionFindsTheBackchannel(t *testing.T) {
	m := parseSDP("v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\na=control:track1\r\n" +
		"m=audio 0 RTP/AVP 0\r\na=control:track3\r\na=sendonly\r\n")
	if len(m) != 2 || m[0].codec() != H265 || m[0].control != "track1" {
		t.Fatalf("video %+v", m)
	}
	if !m[1].send || m[1].encoding != "PCMU" || m[1].rate != 8000 {
		t.Fatalf("backchannel %+v", m[1])
	}
}
