package app

import "os"

// useColor is true when stderr is a terminal and the user hasn't opted out via
// the NO_COLOR convention (https://no-color.org). All result and prompt output
// goes to stderr, so this is the relevant stream.
var useColor = func() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return colorEnabled(os.Getenv, fi.Mode())
}()

func colorEnabled(getenv func(string) string, mode os.FileMode) bool {
	if getenv("NO_COLOR") != "" || getenv("CLICOLOR") == "0" || getenv("TERM") == "dumb" {
		return false
	}
	if getenv("CLICOLOR_FORCE") != "" && getenv("CLICOLOR_FORCE") != "0" {
		return true
	}
	return mode&os.ModeCharDevice != 0
}

// sgr wraps s in an ANSI SGR sequence, or returns it unchanged when color is
// disabled (piped output, NO_COLOR).
func sgr(code, s string) string {
	if !useColor {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func bold(s string) string    { return sgr("1", s) }
func dim(s string) string     { return sgr("2", s) }
func red(s string) string     { return sgr("31", s) }
func green(s string) string   { return sgr("32", s) }
func yellow(s string) string  { return sgr("33", s) }
func cyan(s string) string    { return sgr("36", s) }
func magenta(s string) string { return sgr("35", s) }
