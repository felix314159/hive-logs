package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRunVersionPrintsVersion(t *testing.T) {
	output, err := captureStdout(func() error {
		return run([]string{"--version"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != version {
		t.Fatalf("version output = %q, want %q", output, version)
	}
}

func TestPrintUsageMentionsAllCommands(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	for _, want := range []string{"groups", "suites", "clients", "list", "fetch"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("usage does not mention %q:\n%s", want, buf.String())
		}
	}
}

func TestFormatTimeUsesConfiguredLayout(t *testing.T) {
	ts := time.Date(2026, 4, 28, 9, 10, 11, 0, time.Local)
	if got := formatTime(ts); got != "2026-04-28, 09:10:11" {
		t.Fatalf("formatTime() = %q", got)
	}
}
