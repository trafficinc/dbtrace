package main

import (
	"strings"
	"testing"
)

func TestRunHelpDoesNotRequireConfig(t *testing.T) {
	if err := run([]string{"dbtrace", "--help"}); err != nil {
		t.Fatalf("run help returned error: %v", err)
	}
}

func TestRunUnknownCommandDoesNotRequireConfig(t *testing.T) {
	err := run([]string{"dbtrace", "nope"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "nope"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseInitOptions(t *testing.T) {
	opts, err := parseInitOptions([]string{"--force", "--dsn=user:pass@tcp(localhost:3306)/app"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Force {
		t.Fatal("expected force")
	}
	if opts.DSN != "user:pass@tcp(localhost:3306)/app" {
		t.Fatalf("dsn = %q", opts.DSN)
	}
}

func TestParseInitOptionsRequiresDSNValue(t *testing.T) {
	_, err := parseInitOptions([]string{"--dsn"})
	if err == nil {
		t.Fatal("expected --dsn value error")
	}
}

func TestParseRunOptionsVerbose(t *testing.T) {
	opts, err := parseRunOptions([]string{"--verbose"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Verbose {
		t.Fatal("expected verbose")
	}
}

func TestParseRunOptionsRejectsUnknown(t *testing.T) {
	_, err := parseRunOptions([]string{"--loud"})
	if err == nil {
		t.Fatal("expected unknown option error")
	}
}
