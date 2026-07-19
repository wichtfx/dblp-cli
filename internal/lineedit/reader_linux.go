//go:build linux

package lineedit

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

func terminal(in *os.File) bool {
	_, err := unix.IoctlGetTermios(int(in.Fd()), unix.TCGETS)
	return err == nil
}

func (r *Reader) readTerminal(prompt string) (string, bool, error) {
	fd := int(r.in.Fd())
	original, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", false, err
	}
	raw := *original
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Iflag &^= unix.ICRNL | unix.IXON
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return "", false, err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, original)

	line := ""
	historyAt := len(r.history)
	redraw := func() error {
		_, err := fmt.Fprintf(r.out, "\r\033[2K%s%s", prompt, line)
		return err
	}
	if err := redraw(); err != nil {
		return "", false, err
	}

	var b [1]byte
	for {
		if _, err := r.in.Read(b[:]); err != nil {
			return "", false, err
		}
		switch b[0] {
		case '\r', '\n':
			fmt.Fprintln(r.out)
			return line, true, nil
		case 3: // Ctrl-C
			line = ""
			fmt.Fprintln(r.out, "^C")
			return line, true, nil
		case 4: // Ctrl-D
			if line == "" {
				fmt.Fprintln(r.out)
				return "", false, nil
			}
		case 8, 127:
			if line != "" {
				_, size := utf8.DecodeLastRuneInString(line)
				line = line[:len(line)-size]
				if err := redraw(); err != nil {
					return "", false, err
				}
			}
		case 27:
			key, err := readEscape(r.in)
			if err != nil {
				return "", false, err
			}
			switch key {
			case 'A':
				if historyAt > 0 {
					historyAt--
					line = r.history[historyAt]
				}
			case 'B':
				if historyAt < len(r.history)-1 {
					historyAt++
					line = r.history[historyAt]
				} else {
					historyAt = len(r.history)
					line = ""
				}
			case 'C': // Right: next publication page.
				fmt.Fprintln(r.out)
				return nextPage, true, nil
			case 'D': // Left: previous publication page.
				fmt.Fprintln(r.out)
				return previousPage, true, nil
			}
			if err := redraw(); err != nil {
				return "", false, err
			}
		default:
			if b[0] >= 32 && b[0] != 127 {
				line += string(b[0])
				if _, err := r.out.Write(b[:]); err != nil {
					return "", false, err
				}
			}
		}
	}
}

// readEscape consumes one complete CSI key sequence. Delete, for example,
// arrives as ESC [ 3 ~; consuming only the first two bytes would insert the
// trailing '~' into the command line.
func readEscape(r io.Reader) (byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	if b[0] != '[' && b[0] != 'O' {
		return 0, nil
	}
	for i := 0; i < 8; i++ {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		if (b[0] >= 'A' && b[0] <= 'Z') || b[0] == '~' {
			return b[0], nil
		}
	}
	return 0, nil
}
