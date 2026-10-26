package wire

import (
	"bytes"
	"encoding/binary"
	"io"
)

func putWord(b *bytes.Buffer, v uint32) {
	var w [4]byte
	binary.NativeEndian.PutUint32(w[:], v)
	b.Write(w[:])
}

func readWord(r io.Reader) (uint32, error) {
	var w [4]byte
	if _, err := io.ReadFull(r, w[:]); err != nil {
		return 0, err
	}
	return binary.NativeEndian.Uint32(w[:]), nil
}
