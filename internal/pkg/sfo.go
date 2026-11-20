// Package pkg reads PlayStation packages: PS4 PKG files, and the PKG
// files of the PS3, PSP and PS Vita. It names the game they hold (from the
// header and PARAM.SFO) and extracts what can be extracted. It contains no
// keys: the PS3, PSP and Vita keys are the user's own (see Keys).
package pkg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strconv"
)

// SFO is a PARAM.SFO: the name, ID and version of a game.
type SFO map[string]string

// ParseSFO reads a PARAM.SFO. Integer values are given in decimal.
func ParseSFO(b []byte) (SFO, error) {
	if len(b) < 20 || !bytes.Equal(b[:4], []byte("\x00PSF")) {
		return nil, errors.New("not a PARAM.SFO")
	}
	le := binary.LittleEndian
	keys, data, n := le.Uint32(b[8:]), le.Uint32(b[12:]), le.Uint32(b[16:])
	if uint64(20)+uint64(n)*16 > uint64(len(b)) || keys > uint32(len(b)) || data > uint32(len(b)) {
		return nil, errors.New("PARAM.SFO: bad table")
	}
	sfo := SFO{}
	for i := range n {
		e := b[20+i*16:]
		koff := uint64(keys) + uint64(le.Uint16(e[0:]))
		format := le.Uint16(e[2:])
		size := uint64(le.Uint32(e[4:]))
		doff := uint64(data) + uint64(le.Uint32(e[12:]))
		if koff >= uint64(len(b)) || doff+size > uint64(len(b)) {
			continue
		}
		key := b[koff:]
		if j := bytes.IndexByte(key, 0); j >= 0 {
			key = key[:j]
		}
		v := b[doff : doff+size]
		switch format {
		case 0x0404: // int32
			if len(v) >= 4 {
				sfo[string(key)] = strconv.FormatUint(uint64(le.Uint32(v)), 10)
			}
		default: // UTF-8, with or without the final NUL
			if j := bytes.IndexByte(v, 0); j >= 0 {
				v = v[:j]
			}
			sfo[string(key)] = string(v)
		}
	}
	return sfo, nil
}

// Title is the game's name: TITLE, or the short STITLE (Vita).
func (s SFO) Title() string {
	if t := s["TITLE"]; t != "" {
		return t
	}
	return s["STITLE"]
}

// Version is the version of the game or update: APP_VER, else VERSION,
// without leading zeros ("01.05" → "1.05").
func (s SFO) Version() string {
	v := s["APP_VER"]
	if v == "" {
		v = s["VERSION"]
	}
	for len(v) > 1 && v[0] == '0' && v[1] != '.' {
		v = v[1:]
	}
	return v
}
