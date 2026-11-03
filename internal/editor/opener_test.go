package editor

import (
	"errors"
	"reflect"
	"testing"
)

func TestOpenerUsesDirectPlatformArguments(t *testing.T) {
	filename := "/workspace/apps/shop/app/components/content/Hero.vue"
	tests := []struct {
		goos string
		name string
		args []string
	}{
		{goos: "darwin", name: "open", args: []string{filename}},
		{goos: "linux", name: "xdg-open", args: []string{filename}},
		{goos: "windows", name: "rundll32", args: []string{"url.dll,FileProtocolHandler", filename}},
	}

	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			commands := &commandStarterFake{}

			err := (Opener{GOOS: test.goos, Commands: commands}).Open(filename)

			if err != nil || commands.name != test.name || !reflect.DeepEqual(commands.args, test.args) {
				t.Fatalf("command = %q %#v, error = %v", commands.name, commands.args, err)
			}
		})
	}
}

func TestOpenerRejectsInvalidRequestsAndPreservesStarterErrors(t *testing.T) {
	commands := &commandStarterFake{}
	if err := (Opener{GOOS: "darwin", Commands: commands}).Open(""); err == nil {
		t.Fatal("Open() accepted an empty filename")
	}
	if commands.calls != 0 {
		t.Fatalf("empty filename started %d commands", commands.calls)
	}

	if err := (Opener{GOOS: "plan9", Commands: commands}).Open("Hero.vue"); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform error = %v", err)
	}

	want := errors.New("starter failed")
	commands.err = want
	if err := (Opener{GOOS: "linux", Commands: commands}).Open("Hero.vue"); !errors.Is(err, want) {
		t.Fatalf("starter error = %v", err)
	}
}

type commandStarterFake struct {
	name  string
	args  []string
	err   error
	calls int
}

func (f *commandStarterFake) Start(name string, args ...string) error {
	f.calls++
	f.name = name
	f.args = append([]string{}, args...)
	return f.err
}
