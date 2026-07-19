//go:build !linux

package lineedit

import "os"

func terminal(_ *os.File) bool { return false }

func (r *Reader) readTerminal(_ string) (string, bool, error) {
	return "", false, nil
}
