package app

import (
	"os"
	"testing"
)

func TestColorEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		mode os.FileMode
		want bool
	}{
		{name: "terminal", mode: os.ModeCharDevice, want: true},
		{name: "redirected", mode: 0, want: false},
		{name: "no color", env: map[string]string{"NO_COLOR": "1"}, mode: os.ModeCharDevice, want: false},
		{name: "clicolor disabled", env: map[string]string{"CLICOLOR": "0"}, mode: os.ModeCharDevice, want: false},
		{name: "dumb terminal", env: map[string]string{"TERM": "dumb"}, mode: os.ModeCharDevice, want: false},
		{name: "forced when redirected", env: map[string]string{"CLICOLOR_FORCE": "1"}, mode: 0, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			if got := colorEnabled(getenv, tt.mode); got != tt.want {
				t.Fatalf("colorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
