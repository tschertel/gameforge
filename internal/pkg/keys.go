package pkg

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// KeyNames are the keys a pkg.keys file may hold, 32 hexadecimal digits
// each: the PS3 PKG key, the PSP PKG key and the three PS Vita PKG keys.
var KeyNames = []string{"ps3", "psp", "vita2", "vita3", "vita4"}

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
		value = strings.TrimSpace(value)
		b, err := hex.DecodeString(value)
		if !ok || err != nil || len(b) != 16 {
			return nil, fmt.Errorf("pkg.keys line %d: expected name = 32 hexadecimal digits", line)
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

// Names lists the keys loaded, in the order of KeyNames.
func (k *Keys) Names() []string {
	var out []string
	for _, n := range KeyNames {
		if k.Get(n) != nil {
			out = append(out, n)
		}
	}
	return out
}

// NewKeys makes Keys from a map, for tests.
func NewKeys(m map[string][]byte) *Keys { return &Keys{k: m} }
