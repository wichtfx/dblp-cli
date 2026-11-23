//go:build linux

package lineedit

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadEscapeConsumesWholeSequence(t *testing.T) {
	tests := []struct {
		sequence string
		want     byte
	}{
		{"[A", 'A'},
		{"[B", 'B'},
		{"[3~", '~'},
		{"OH", 'H'},
	}
	for _, tt := range tests {
		r := strings.NewReader(tt.sequence)
		got, err := readEscape(r)
		if err != nil {
			t.Fatalf("readEscape(%q): %v", tt.sequence, err)
		}
		if got != tt.want {
			t.Fatalf("readEscape(%q) = %q, want %q", tt.sequence, got, tt.want)
		}
		if r.Len() != 0 {
			t.Fatalf("readEscape(%q) left %d byte(s)", tt.sequence, r.Len())
		}
	}
}

// openPTY returns the master and the slave side of a new pseudo-terminal.
func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	go io.Copy(io.Discard, master)
	return master, slave
}

func TestReadTerminalKeepsUTF8(t *testing.T) {
	master, slave := openPTY(t)
	if _, err := master.WriteString("schmidhüber\r"); err != nil {
		t.Fatal(err)
	}
	line, ok, err := New(slave, io.Discard).ReadLine("> ")
	if err != nil || !ok {
		t.Fatalf("ReadLine: ok=%v err=%v", ok, err)
	}
	if line != "schmidhüber" {
		t.Fatalf("ReadLine = %q, want %q", line, "schmidhüber")
	}
}

func TestReadTerminalIgnoresLoneEscape(t *testing.T) {
	master, slave := openPTY(t)
	go func() {
		master.WriteString("\x1b")
		time.Sleep(4 * escapeWait)
		master.WriteString("x\r")
	}()
	line, ok, err := New(slave, io.Discard).ReadLine("> ")
	if err != nil || !ok {
		t.Fatalf("ReadLine: ok=%v err=%v", ok, err)
	}
	if line != "x" {
		t.Fatalf("ReadLine = %q, want %q", line, "x")
	}
}

func TestReadTerminalReportsCtrlC(t *testing.T) {
	master, slave := openPTY(t)
	// The bytes are written once the reader has set raw mode: in cooked mode
	// the terminal takes Ctrl-C itself.
	go func() {
		time.Sleep(4 * escapeWait)
		master.WriteString("abc\x03")
	}()
	type read struct {
		line string
		ok   bool
		err  error
	}
	done := make(chan read, 1)
	go func() {
		line, ok, err := New(slave, io.Discard).ReadLine("> ")
		done <- read{line, ok, err}
	}()
	select {
	case r := <-done:
		if r.err != ErrInterrupt || !r.ok || r.line != "" {
			t.Fatalf("ReadLine = %q, %v, %v; want \"\", true, ErrInterrupt", r.line, r.ok, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadLine did not return on Ctrl-C")
	}
}
