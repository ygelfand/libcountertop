package smp

import (
	"crypto/aes"
	"crypto/subtle"
)

// Core specification Vol 3 Part H 2.2; values travel least significant octet first, AES takes them reversed.

func reversed(b []byte) []byte {
	r := make([]byte, len(b))
	for i, v := range b {
		r[len(b)-1-i] = v
	}
	return r
}

func e(k, r [16]byte) [16]byte {
	block, _ := aes.NewCipher(reversed(k[:]))
	var out [16]byte
	block.Encrypt(out[:], reversed(r[:]))
	copy(out[:], reversed(out[:]))
	return out
}

func xor16(a, b [16]byte) [16]byte {
	var out [16]byte
	subtle.XORBytes(out[:], a[:], b[:])
	return out
}

func c1(k, r [16]byte, preq, pres [7]byte, iat uint8, ia [6]byte, rat uint8, ra [6]byte) [16]byte {
	var p1, p2 [16]byte
	p1[0], p1[1] = iat, rat
	copy(p1[2:], preq[:])
	copy(p1[9:], pres[:])
	copy(p2[0:], ra[:])
	copy(p2[6:], ia[:])
	return e(k, xor16(e(k, xor16(r, p1)), p2))
}

func s1(k, r1, r2 [16]byte) [16]byte {
	var r [16]byte
	copy(r[:8], r2[:8])
	copy(r[8:], r1[:8])
	return e(k, r)
}

// RFC 4493.
func aesCMAC(k [16]byte, m []byte) [16]byte {
	block, _ := aes.NewCipher(reversed(k[:]))
	msg := reversed(m)

	var l [16]byte
	block.Encrypt(l[:], l[:])
	k1 := shift(l)
	k2 := shift(k1)

	n := (len(msg) + 15) / 16
	whole := n > 0 && len(msg)%16 == 0
	if n == 0 {
		n = 1
	}
	var last [16]byte
	if whole {
		copy(last[:], msg[16*(n-1):])
		last = xor16(last, k1)
	} else {
		rest := msg[16*(n-1):]
		copy(last[:], rest)
		last[len(rest)] = 0x80
		last = xor16(last, k2)
	}

	var x [16]byte
	for i := range n - 1 {
		var blk [16]byte
		copy(blk[:], msg[16*i:])
		x = xor16(x, blk)
		block.Encrypt(x[:], x[:])
	}
	x = xor16(x, last)
	block.Encrypt(x[:], x[:])

	var out [16]byte
	copy(out[:], reversed(x[:]))
	return out
}

func shift(b [16]byte) [16]byte {
	var out [16]byte
	carry := byte(0)
	for i := 15; i >= 0; i-- {
		out[i] = b[i]<<1 | carry
		carry = b[i] >> 7
	}
	if b[0]&0x80 != 0 {
		out[15] ^= 0x87
	}
	return out
}

func f4(u, v [32]byte, x [16]byte, z uint8) [16]byte {
	m := make([]byte, 65)
	m[0] = z
	copy(m[1:], v[:])
	copy(m[33:], u[:])
	return aesCMAC(x, m)
}

var f5Salt = [16]byte{
	0xbe,
	0x83,
	0x60,
	0x5a,
	0xdb,
	0x0b,
	0x37,
	0x60,
	0x38,
	0xa5,
	0xf5,
	0xaa,
	0x91,
	0x83,
	0x88,
	0x6c,
}

// Length 256, A2, A1, N2, N1, keyID "btle", counter.
func f5(w [32]byte, n1, n2 [16]byte, a1, a2 [7]byte) (macKey, ltk [16]byte) {
	t := aesCMAC(f5Salt, w[:])
	m := make([]byte, 53)
	m[0], m[1] = 0x00, 0x01
	copy(m[2:], a2[:])
	copy(m[9:], a1[:])
	copy(m[16:], n2[:])
	copy(m[32:], n1[:])
	copy(m[48:], []byte{0x65, 0x6c, 0x74, 0x62})
	m[52] = 0
	macKey = aesCMAC(t, m)
	m[52] = 1
	ltk = aesCMAC(t, m)
	return macKey, ltk
}

func f6(w, n1, n2, r [16]byte, ioCap [3]byte, a1, a2 [7]byte) [16]byte {
	m := make([]byte, 65)
	copy(m[0:], a2[:])
	copy(m[7:], a1[:])
	copy(m[14:], ioCap[:])
	copy(m[17:], r[:])
	copy(m[33:], n2[:])
	copy(m[49:], n1[:])
	return aesCMAC(w, m)
}
