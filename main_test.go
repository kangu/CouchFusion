package main

import (
	"flag"
	"os"
	"testing"
)

func TestAddYesFlagsAcceptsLongAndShortForms(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"-y"}} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		yes := addYesFlags(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatalf("Parse(%v) error = %v", args, err)
		}
		if !*yes {
			t.Fatalf("yes flag for args %v = false, want true", args)
		}
	}
}

func TestWaitForEnterReturnsAfterNewline(t *testing.T) {
	original := os.Stdin
	defer func() { os.Stdin = original }()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	os.Stdin = reader

	if _, err := writer.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := waitForEnter(""); err != nil {
		t.Fatalf("waitForEnter() error = %v", err)
	}
}
