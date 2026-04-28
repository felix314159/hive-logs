// Package main defines the hive-logs CLI surface and routes top-level commands to their handlers.
package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

const (
	timeFormat = "2006-01-02, 15:04:05"
	version    = "1.0"
)

func formatTime(t time.Time) string {
	return t.Local().Format(timeFormat)
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(os.Stderr)
		return nil
	}
	if args[0] == "version" || args[0] == "-v" || args[0] == "--version" {
		fmt.Println(version)
		return nil
	}

	cmd := args[0]
	args = args[1:]

	switch cmd {
	case "list":
		return cmdList(args)
	case "groups":
		return cmdGroups(args)
	case "suites":
		return cmdSuites(args)
	case "clients":
		return cmdClients(args)
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `hive-logs finds Hive failures and fetches per-test logs.

Usage:
  list [--json]
      List result groups, suites, and known clients.
  groups GROUP [--all] [--files] [--limit N] [--json]
      Print the latest matching Hive runs grouped by suite, then client.
      --all includes older runs; --files prints run file names.
  groups GROUP SUITE [--json]
      Per-client pass/fail counts, run start, and duration for the latest SUITE run in GROUP.
  groups GROUP SUITE CLIENT [--json]
      List CLIENT's failing tests in the latest SUITE run and download
      hive.log + <CLIENT>.log bundles for each into ./logs.
`)
}
