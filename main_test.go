package main

import (
	"flag"
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
