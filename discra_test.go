package main

import (
	"encoding/binary"
	"testing"

	"gameforge/internal/retroach"
)

// A Dreamcast CD whose data track starts at sector 11702 (after audio
// tracks or a first session), with an ISO 9660 file system counting from
// the start of the disc: read from the track's own sectors, the boot file
// is found and the hash is the one read by disc address.
func TestFileSystemDisc(t *testing.T) {
	const start = 11702
	abs := map[int64][]byte{}
	sector := func(lba int64) []byte {
		if abs[lba] == nil {
			abs[lba] = make([]byte, 2048)
		}
		return abs[lba]
	}
	record := func(b []byte, name string, lba, size uint32, dir bool) int {
		n := 33 + len(name) + (len(name)+1)%2
		b[0] = byte(n)
		binary.LittleEndian.PutUint32(b[2:], lba)
		binary.LittleEndian.PutUint32(b[10:], size)
		if dir {
			b[25] = 2
		}
		b[32] = byte(len(name))
		copy(b[33:], name)
		return n
	}
	ip := sector(start)
	copy(ip, "SEGA SEGAKATANA SEGA ENTERPRISES")
	copy(ip[96:], "1ST_READ.BIN    ")
	pvd := sector(start + 16)
	copy(pvd, "\x01CD001")
	binary.LittleEndian.PutUint16(pvd[128:], 2048)
	const root, boot = start + 30, start + 40
	record(pvd[156:], "\x00", root, 2048, true)
	dir := sector(root)
	p := record(dir, "\x00", root, 2048, true)
	p += record(dir[p:], "\x01", root, 2048, true)
	record(dir[p:], "1ST_READ.BIN;1", boot, 4096, false)
	copy(sector(boot), "boot file")
	copy(sector(boot+1), "more of it")

	byAddress := retroach.Disc{Start: start, Read: func(lba int64) ([]byte, error) { return sector(lba), nil }}
	want, err := retroach.HashDisc(retroach.Dreamcast, byAddress)
	if err != nil {
		t.Fatal(err)
	}
	d := fileSystemDisc(func(rel int64) ([]byte, error) { return sector(start + rel), nil })
	if d.Start != start {
		t.Errorf("start %d, want %d", d.Start, start)
	}
	if got, err := retroach.HashDisc(retroach.Dreamcast, d); err != nil || got != want {
		t.Errorf("hash %s %v, want %s", got, err, want)
	}

	// A disc whose file system counts from the track's start is read as is.
	d = fileSystemDisc(func(rel int64) ([]byte, error) { return sector(rel), nil })
	if d.Start != 0 {
		t.Errorf("start %d for a track at 0", d.Start)
	}
}
