package webreceiver

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
)

var playReady = [16]byte{
	0x9a,
	0x04,
	0xf0,
	0x79,
	0x98,
	0x40,
	0x42,
	0x86,
	0xab,
	0x92,
	0xe6,
	0x5b,
	0xe0,
	0x88,
	0x5f,
	0x95,
}

func psshBox(system [16]byte, version byte, listed [][]byte, data []byte) []byte {
	body := append([]byte{version, 0, 0, 0}, system[:]...)
	if version > 0 {
		body = binary.BigEndian.AppendUint32(body, uint32(len(listed)))
		for _, kid := range listed {
			body = append(body, kid...)
		}
	}
	body = binary.BigEndian.AppendUint32(body, uint32(len(data)))
	body = append(body, data...)
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(out, "pssh"...), body...)
}

func TestTheWidevineBoxIsPickedFromThePagesInitData(t *testing.T) {
	kid := bytes.Repeat([]byte{7}, 16)
	ours := psshBox(surface.Widevine, 0, nil, append([]byte{0x12, 16}, kid...))
	both := append(psshBox(playReady, 0, nil, []byte("playready")), ours...)
	got, err := initData("cenc", both)
	if err != nil || !bytes.Equal(got, ours) {
		t.Fatalf("picked % x, %v; want the widevine box", got, err)
	}
	if kids := keyIDs(got); len(kids) != 1 || !bytes.Equal(kids[0], kid) {
		t.Fatalf("read key ids %x, want %x", kids, kid)
	}
	if _, err := initData("cenc", psshBox(playReady, 0, nil, nil)); err == nil {
		t.Fatal("took init data with no widevine box")
	}
}

func TestKeyIDsBecomeAWidevineBox(t *testing.T) {
	kid := bytes.Repeat([]byte{3}, 16)
	json := `{"kids":["` + base64.RawURLEncoding.EncodeToString(kid) + `"]}`
	for kind, data := range map[string][]byte{"keyids": []byte(json), "webm": kid} {
		box, err := initData(kind, data)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if string(box[4:8]) != "pssh" || !bytes.Equal(box[12:28], surface.Widevine[:]) {
			t.Fatalf("%s became % x, want a widevine pssh", kind, box)
		}
		if kids := keyIDs(box); len(kids) != 1 || !bytes.Equal(kids[0], kid) {
			t.Fatalf("%s carried key ids %x, want %x", kind, kids, kid)
		}
	}
}

func TestAVersionOneBoxListsItsKeyIDs(t *testing.T) {
	a, b := bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 16)
	kids := keyIDs(psshBox(surface.Widevine, 1, [][]byte{a, b}, append([]byte{0x12, 16}, a...)))
	if len(kids) != 2 || !bytes.Equal(kids[0], a) || !bytes.Equal(kids[1], b) {
		t.Fatalf("read key ids %x, want the two listed", kids)
	}
}
