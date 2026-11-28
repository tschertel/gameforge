package retroach

import (
	"bytes"
	"testing"
)

// fillImage fills an image with the pseudo-random data of rcheevos's
// tests (test/hash/hash_data.c), so its expected hashes can be used here.
func fillImage(image []byte) {
	size := uint64(len(image))
	seed := int32(size ^ (size >> 8) ^ ((size - 1) * 25387))
	for p := 0; p < len(image); {
		var count int32
		var value byte
		v := byte(seed >> 16)
		switch seed & 0xff {
		case 0:
			count = int32(uint64((seed>>8)&0x3f) &^ (uint64(len(image)-p) & 0x0f))
			count = max(count, 1)
		case 1:
			count, value = (seed>>8)&0x07+1, v
		case 2:
			count, value = (seed>>8)&0x03+1, v^0xff
		case 3:
			count, value = (seed>>8)&0x03+1, v^0xa5
		case 4:
			count, value = (seed>>8)&0x03+1, v^0xc3
		case 5:
			count, value = (seed>>8)&0x03+1, v^0x96
		case 6, 7:
			count, value = (seed>>8)&0x03+1, v^0x78
		default:
			count, value = 1, byte((seed>>8)^(seed>>16))
		}
		for ; count > 0 && p < len(image); count-- {
			image[p] = value
			p++
		}
		seed = (seed*0x41c64e6d + 12345) & 0x7fffffff
	}
}

// The GameCube image of rcheevos's test_hash_gamecube, with its hash.
func TestHashGameCube(t *testing.T) {
	image := make([]byte, 32<<20)
	fillImage(image)
	copy(image[0x1c:], []byte{0xc2, 0x33, 0x9f, 0x3d})
	for i := range 8 {
		image[0x2440+0x14+i] = 0
		if i%4 == 3 {
			image[0x2440+0x14+i] = 0xff
		}
	}
	copy(image[0x420:], []byte{0, 0, 0x30, 0})
	for i := range 18 * 4 {
		image[0x3000+i], image[0x3000+0x90+i] = 0, 0
		if i%4 == 2 {
			image[0x3000+i] = byte(0x30 + 1 + i/4)
		}
		if i%8 == 3 {
			image[0x3000+0x90+i] = 0xff
		}
	}
	got, err := HashGameCube(bytes.NewReader(image))
	if want := "c7803b704fa43d22d8f6e55f4789cb45"; err != nil || got != want {
		t.Errorf("got %s %v, want %s", got, err, want)
	}
	image[0x1c] = 0
	if _, err := HashGameCube(bytes.NewReader(image)); err == nil {
		t.Error("a disc without the GameCube mark was hashed")
	}
}
