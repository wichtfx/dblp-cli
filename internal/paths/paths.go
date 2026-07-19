// Package paths resolves where dblp-cli keeps its data on disk.
package paths

import (
	"os"
	"path/filepath"
)

// dirName is the sub-directory used under the user's data home.
const dirName = "dblp-cli"

// DataDir returns the directory holding the dump, index and metadata,
// creating it if necessary. It honours $XDG_DATA_HOME and falls back to
// ~/.local/share (or the OS user config dir if HOME is unavailable).
func DataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".local", "share")
		} else {
			// Last resort: OS-specific config dir.
			cfg, cerr := os.UserConfigDir()
			if cerr != nil {
				return "", cerr
			}
			base = cfg
		}
	}
	dir := filepath.Join(base, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Dump returns the path to the compressed XML dump.
func Dump() (string, error) { return inDataDir("dblp.xml.gz") }

// DB returns the path to the SQLite search index.
func DB() (string, error) { return inDataDir("dblp.db") }

// Meta returns the path to the metadata file.
func Meta() (string, error) { return inDataDir("meta.json") }

func inDataDir(name string) (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}
