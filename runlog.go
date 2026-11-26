package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// logsDirName is the folder, next to the configuration file, that holds the
// saved logs of the runs.
const logsDirName = "logs"

// saveRunLog writes the lines a run logged to "logs/GameForge - date
// time.txt" next to the configuration file, in the language they are
// shown in. A run that logged nothing writes no file.
func saveRunLog(configPath string, lines []fmt.Stringer, now time.Time) error {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.String())
		b.WriteString("\n")
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return nil
	}
	dir := filepath.Join(filepath.Dir(configPath), logsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path, err := freePath(dir, "GameForge - "+now.Format("2006-01-02 15-04-05"), ".txt")
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text+"\n"), 0o600)
}
