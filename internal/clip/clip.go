// Package clip copies text to the system clipboard.
package clip

import "github.com/atotto/clipboard"

// Available reports whether a clipboard backend (wl-copy, xclip/xsel,
// pbcopy, ...) is usable in the current environment.
func Available() bool { return !clipboard.Unsupported }

// Copy places s on the system clipboard.
func Copy(s string) error { return clipboard.WriteAll(s) }
