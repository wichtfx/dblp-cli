//go:build linux

package lineedit

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

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

func TestReadTerminalKeepsUTF8(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()
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
	defer slave.Close()
	go io.Copy(io.Discard, master)
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
