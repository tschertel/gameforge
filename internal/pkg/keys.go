package pkg

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// KeyNames are the keys a pkg.keys file may hold: the PS3 PKG key, the
// PSP PKG key and the three PS Vita PKG keys (32 hexadecimal digits each),
// and the two PS4 RSA keysets that unwrap the PFS key of fake packages,
// each as a modulus n and a private exponent d (512 hexadecimal digits
// each).
var KeyNames = []string{"ps3", "psp", "vita2", "vita3", "vita4",
	"ps4_fake_n", "ps4_fake_d", "ps4_dk3_n", "ps4_dk3_d"}

// keySize is the size of a key in bytes.
func keySize(name string) int {
	if strings.HasPrefix(name, "ps4_") {
		return 256
	}
	return 16
}

// Keys are the user's own PKG keys. GameForge contains none.
type Keys struct {
	k map[string][]byte
}

// LoadKeys reads a pkg.keys file: lines "name = hex", with # comments.
func LoadKeys(path string) (*Keys, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	keys := &Keys{k: map[string][]byte{}}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(s, '#'); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		if s == "" {
			continue
		}
		name, value, ok := strings.Cut(s, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		// Hex may be split by spaces, commas or "0x" (as copied from C).
		value = strings.NewReplacer(" ", "", "\t", "", ",", "", "0x", "", "0X", "").Replace(strings.TrimSpace(value))
		b, err := hex.DecodeString(value)
		if !ok || err != nil || len(b) != keySize(name) {
			return nil, fmt.Errorf("pkg.keys line %d: expected %s = %d hexadecimal digits", line, name, 2*keySize(name))
		}
		keys.k[name] = b
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

// Get returns a key, or nil.
func (k *Keys) Get(name string) []byte {
	if k == nil {
		return nil
	}
	return k.k[name]
}

// Names lists the keys loaded, in the order of KeyNames; the four parts
// of the PS4 keysets count as one, "ps4", when all are there.
func (k *Keys) Names() []string {
	var out []string
	for _, n := range KeyNames {
		if k.Get(n) != nil && !strings.HasPrefix(n, "ps4_") {
			out = append(out, n)
		}
	}
	if k.ps4() {
		out = append(out, "ps4")
	}
	return out
}

// ps4 reports whether both PS4 keysets are loaded.
func (k *Keys) ps4() bool {
	for _, n := range []string{"ps4_fake_n", "ps4_fake_d", "ps4_dk3_n", "ps4_dk3_d"} {
		if k.Get(n) == nil {
			return false
		}
	}
	return true
}

// NewKeys makes Keys from a map, for tests.
func NewKeys(m map[string][]byte) *Keys { return &Keys{k: m} }
