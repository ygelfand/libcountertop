package gemini

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

func pack(resources map[uint16][]byte, order ...uint16) []byte {
	head := 12 + 6*(len(order)+1)
	var b bytes.Buffer
	b.Write(binary.LittleEndian.AppendUint32(nil, 5))
	b.Write([]byte{1, 0, 0, 0})
	b.Write(binary.LittleEndian.AppendUint16(nil, uint16(len(order))))
	b.Write([]byte{0, 0})
	at := uint32(head)
	for _, id := range order {
		b.Write(binary.LittleEndian.AppendUint16(nil, id))
		b.Write(binary.LittleEndian.AppendUint32(nil, at))
		at += uint32(len(resources[id]))
	}
	b.Write([]byte{0, 0})
	b.Write(binary.LittleEndian.AppendUint32(nil, at))
	for _, id := range order {
		b.Write(resources[id])
	}
	return b.Bytes()
}

func TestResourceFindsItsEntry(t *testing.T) {
	p := pack(map[uint16][]byte{33800: []byte("hotword"), 33801: []byte("stop")}, 33800, 33801)
	got, err := resource(p, 33801)
	if err != nil || string(got) != "stop" {
		t.Fatalf("resource 33801 = %q, %v", got, err)
	}
	if _, err := resource(p, 1); err == nil {
		t.Error("a missing resource was found")
	}
	if _, err := resource([]byte("not a pack at all"), 33800); err == nil {
		t.Error("garbage parsed as a pack")
	}
}

func TestDetectorIsTheFirstTaggedModel(t *testing.T) {
	model := append([]byte{0x1c, 0, 0, 0}, []byte("TFL3 first model")...)
	second := append([]byte{0x1c, 0, 0, 0}, []byte("TFL3 second")...)
	var res bytes.Buffer
	res.WriteString("header")
	res.Write([]byte{0x02, 0xf8})
	res.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(model))))
	res.Write([]byte{0x00, 0x07, 0, 0})
	res.Write(model)
	res.Write([]byte{0x02, 0xf8})
	res.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(second))))
	res.Write([]byte{0x00, 0x07})
	res.Write(second)

	got, err := detector(res.Bytes())
	if err != nil || !bytes.Equal(got, model) {
		t.Fatalf("detector = %q, %v", got, err)
	}
	if _, err := detector([]byte("no models here")); err == nil {
		t.Error("found a model in plain bytes")
	}
}

func TestCatalogOffersHeyGoogleOnlyWhenOn(t *testing.T) {
	on := false
	c := NewCatalog(t.TempDir(), func() bool { return on })
	if got := c.Models(); len(got) != 0 {
		t.Fatalf("off lists %v", got)
	}
	on = true
	got := c.Models()
	if len(got) != 1 || got[0].ID != ID || got[0].Kind != wake.KindGemini {
		t.Fatalf("on lists %+v", got)
	}
}
