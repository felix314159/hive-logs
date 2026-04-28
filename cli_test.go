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
	for _, want := range []string{
		"list [--json]",
		"group=GROUP",
		"group=GROUP suite=SUITE",
		"group=GROUP suite=SUITE client=CLIENT",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("usage does not mention %q:\n%s", want, buf.String())
		}
	}
	for _, removed := range []string{"\n  groups ", "\n  suites ", "\n  clients ", "\n  fetch ", "\n  --version"} {
		if strings.Contains(buf.String(), removed) {
			t.Fatalf("usage mentions removed command %q:\n%s", strings.TrimSpace(removed), buf.String())
		}
	}
}

func TestRunRejectsRemovedCommands(t *testing.T) {
	for _, cmd := range []string{"fetch", "groups", "suites", "clients"} {
		if err := run([]string{cmd}); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("run(%q) err = %v, want unknown command", cmd, err)
		}
	}
}

func TestRunRoutesKeyValueArgsToQuery(t *testing.T) {
	err := run([]string{"group=generic", "--base-url", "http://127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected fetch error against unreachable base URL")
	}
	if strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("key=value args should not be treated as a subcommand: %v", err)
	}
}

func TestFormatTimeUsesConfiguredLayout(t *testing.T) {
	ts := time.Date(2026, 4, 28, 9, 10, 11, 0, time.Local)
	if got := formatTime(ts); got != "2026-04-28, 09:10:11" {
		t.Fatalf("formatTime() = %q", got)
	}
}
