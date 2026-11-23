// Package lineedit provides the small amount of terminal editing needed by
// the interactive client.
package lineedit

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	previousPage = "\x00previous-page"
	nextPage     = "\x00next-page"
)

// ErrInterrupt is returned by ReadLine when Ctrl-C is typed at the prompt;
// the typed line is discarded.
var ErrInterrupt = errors.New("interrupt")

type Reader struct {
	in      *os.File
	out     io.Writer
	scanner *bufio.Scanner
	history []string
}

func New(in *os.File, out io.Writer) *Reader {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &Reader{in: in, out: out, scanner: scanner}
}

func (r *Reader) ReadLine(prompt string) (string, bool, error) {
	if terminal(r.in) {
		line, ok, err := r.readTerminal(prompt)
		switch line {
		case previousPage:
			return "p", ok, err
		case nextPage:
			return "n", ok, err
		}
		if err == nil && ok && line != "" {
			r.history = append(r.history, line)
		}
		return line, ok, err
	}
	if _, err := fmt.Fprint(r.out, prompt); err != nil {
		return "", false, err
	}
	if !r.scanner.Scan() {
		return "", false, r.scanner.Err()
	}
	line := r.scanner.Text()
	if line != "" {
		r.history = append(r.history, line)
	}
	return line, true, nil
}
