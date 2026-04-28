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
	case "groups":
		return cmdGroups(args)
	case "suites":
		return cmdSuites(args)
	case "clients":
		return cmdClients(args)
	case "list":
		return cmdList(args)
	case "fetch":
		return cmdFetch(args)
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `hive-logs finds Hive failures and fetches per-test logs.

Usage:
  --version
      Print the current version.
  groups [--json]
      List all result groups with their website URL and the latest run timestamp.
  suites [--json]
      List all suites across result groups with the latest run timestamp.
  clients [--json]
      List known client names (offline; from compiled-in list).
  groups GROUP [--client NAME] [--all] [--files] [--limit N] [--json]
      Print the latest matching Hive runs grouped by suite, then client.
      --all includes older runs; --files prints run file names for --run-file.
  groups GROUP SUITE [--json]
      Per-client pass/fail counts, run start, and duration for the latest SUITE run in GROUP.
  groups GROUP SUITE CLIENT [--json]
      List CLIENT's failing tests in the latest SUITE run and download
      hive.log + <CLIENT>.log bundles for each into ./logs.
  list --suite NAME --client NAME [--group NAME] [--test TEXT] [--regex]
       [--status fail|pass|all] [--limit N] [--run-file FILE] [--json]
      List tests from the latest matching run with pass/fail status and log availability.
  fetch --suite NAME --client NAME --test TEXT [--group NAME] [--regex]
        [--out DIR] [--limit N] [--full-client-log] [--include-pass]
        [--run-file FILE] [--json]
      Download hive.log + <CLIENT>.log bundles for matching failing tests into --out.
`)
}
