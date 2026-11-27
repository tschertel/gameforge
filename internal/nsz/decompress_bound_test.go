package nsz

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"runtime"
	"testing"
)

// TestBlockCountBeyondFile checks that a damaged block header whose count
// does not fit in the file is refused before its list is allocated.
func TestBlockCountBeyondFile(t *testing.T) {
	var hdr [24]byte
	copy(hdr[:], "NCZBLOCK")
	hdr[11] = 14
	n := uint32(0xffff_fff0)
	binary.LittleEndian.PutUint32(hdr[12:], n)
	binary.LittleEndian.PutUint64(hdr[16:], uint64(n)<<14)
	r := bytes.NewReader(hdr[:])
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := decompressBlocks(context.Background(), r, 0, int64(len(hdr)), nil, io.Discard)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("no error")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("allocated %d bytes", grew)
	}
}
