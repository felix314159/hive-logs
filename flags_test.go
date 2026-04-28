package main

import (
	"strings"
	"testing"
)

func TestParseGroupsArgsInterleavesFlagsAndPositionals(t *testing.T) {
	gf, rest, err := parseGroupsArgs([]string{
		"generic",
		"--base-url=https://example.test",
		"--limit", "3",
		"--all=false",
		"--files",
		"--json=true",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gf.baseURL != "https://example.test" || gf.limit != 3 || gf.all ||
		!gf.showFiles || !gf.json {
		t.Fatalf("groups flags = %+v", gf)
	}
	if len(rest) != 1 || rest[0] != "generic" {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParseGroupsArgsStopsAtDoubleDash(t *testing.T) {
	gf, rest, err := parseGroupsArgs([]string{"--limit", "2", "--", "--not-a-flag", "suite"})
	if err != nil {
		t.Fatal(err)
	}
	if gf.limit != 2 {
		t.Fatalf("limit = %d", gf.limit)
	}
	if got := strings.Join(rest, ","); got != "--not-a-flag,suite" {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParseGroupsArgsReportsBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--client"},
		{"--client", "geth"},
		{"--limit", "nope"},
		{"--json=maybe"},
		{"--unknown"},
	} {
		if _, _, err := parseGroupsArgs(args); err == nil {
			t.Fatalf("parseGroupsArgs(%v) returned nil error", args)
		}
	}
}
