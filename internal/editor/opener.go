package editor

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

var ErrUnsupportedPlatform = errors.New("opening files is unsupported on this platform")

type CommandStarter interface {
	Start(name string, args ...string) error
}

type Opener struct {
	GOOS     string
	Commands CommandStarter
}

func (opener Opener) Open(filename string) error {
	if strings.TrimSpace(filename) == "" {
		return errors.New("filename is empty")
	}
	goos := opener.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	commands := opener.Commands
	if commands == nil {
		commands = execCommandStarter{}
	}

	var name string
	var args []string
	switch goos {
	case "darwin":
		name, args = "open", []string{filename}
	case "linux":
		name, args = "xdg-open", []string{filename}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", filename}
	default:
		return ErrUnsupportedPlatform
	}
	if err := commands.Start(name, args...); err != nil {
		return fmt.Errorf("start system file opener: %w", err)
	}
	return nil
}

type execCommandStarter struct{}

func (execCommandStarter) Start(name string, args ...string) error {
	command := exec.Command(name, args...)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
