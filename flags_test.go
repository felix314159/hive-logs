package main

import (
	"flag"
	"strings"
	"testing"
)

func TestAddCommonFlagsDefaultsAndParse(t *testing.T) {
	var cf commonFlags
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	addCommonFlags(fs, &cf)

	if err := fs.Parse([]string{
		"--base-url", "https://example.test/",
		"--group", "bal",
		"--suite", "suite-a",
		"--client", "geth",
		"--test", "sync",
		"--run-file", "run.json",
		"--regex",
		"--json",
	}); err != nil {
		t.Fatal(err)
	}

	if cf.baseURL != "https://example.test/" || cf.group != "bal" ||
		cf.suite != "suite-a" || cf.client != "geth" || cf.test != "sync" ||
		cf.runFile != "run.json" || !cf.regex || !cf.json {
		t.Fatalf("parsed common flags = %+v", cf)
	}
}

func TestParseGroupsArgsInterleavesFlagsAndPositionals(t *testing.T) {
	gf, rest, err := parseGroupsArgs([]string{
		"generic",
		"--base-url=https://example.test",
		"--client", "geth",
		"--limit", "3",
		"--all=false",
		"--files",
		"--json=true",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gf.baseURL != "https://example.test" || gf.client != "geth" ||
		gf.limit != 3 || gf.all || !gf.showFiles || !gf.json {
		t.Fatalf("groups flags = %+v", gf)
	}
	if len(rest) != 1 || rest[0] != "generic" {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParseGroupsArgsStopsAtDoubleDash(t *testing.T) {
	gf, rest, err := parseGroupsArgs([]string{"--client", "reth", "--", "--not-a-flag", "suite"})
	if err != nil {
		t.Fatal(err)
	}
	if gf.client != "reth" {
		t.Fatalf("client = %q", gf.client)
	}
	if got := strings.Join(rest, ","); got != "--not-a-flag,suite" {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParseGroupsArgsReportsBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--client"},
		{"--limit", "nope"},
		{"--json=maybe"},
		{"--unknown"},
	} {
		if _, _, err := parseGroupsArgs(args); err == nil {
			t.Fatalf("parseGroupsArgs(%v) returned nil error", args)
		}
	}
}
