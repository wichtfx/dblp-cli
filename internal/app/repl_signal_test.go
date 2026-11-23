//go:build unix

package app

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestCtrlCCancelsCommandAndThenQuits(t *testing.T) {
	st, _ := testStore(t)
	var opened []string
	saved := openURL
	openURL = func(url string) error {
		opened = append(opened, url)
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Error(err)
		}
		time.Sleep(200 * time.Millisecond) // the signal is delivered asynchronously
		return nil
	}
	t.Cleanup(func() { openURL = saved })

	in := script("record", "o all", struct{}{}, "h")
	if err := repl(st, in); err != nil || in.reads != 3 {
		t.Fatalf("repl = %v after %d reads, want quit after 3", err, in.reads)
	}
	if len(opened) != 1 {
		t.Fatalf("opened %v, want the cancel to stop after the first", opened)
	}
}
