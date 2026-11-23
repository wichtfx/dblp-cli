package app

import (
	"os/exec"
	"runtime"
)

// openURL opens url in the default browser. It is a variable so that tests
// can record the URLs instead of opening them.
var openURL = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap the process; the browser outlives it
	return nil
}
