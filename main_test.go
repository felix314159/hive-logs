package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGroupsShowsLatestRunForEverySuite(t *testing.T) {
	runs := append(sampleRuns(51), ListingRun{
		Name:     "eels/consume-engine",
		NTests:   40523,
		Passes:   40467,
		Fails:    56,
		Clients:  []string{"go-ethereum"},
		Start:    time.Date(2026, 4, 26, 8, 9, 26, 0, time.UTC),
		FileName: "eels.json",
	}, ListingRun{
		Name:     "eels/consume-engine",
		NTests:   40523,
		Passes:   40469,
		Fails:    54,
		Clients:  []string{"go-ethereum"},
		Start:    time.Date(2026, 4, 25, 8, 3, 50, 0, time.UTC),
		FileName: "older-eels.json",
	})
	server := listingServer(t, runs)
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdGroups([]string{"--base-url", server.URL, "generic"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "eels/consume-engine") {
		t.Fatalf("groups output did not include eels/consume-engine:\n%s", output)
	}
	if strings.Contains(output, "2026-04-25") {
		t.Fatalf("default groups output included older eels run:\n%s", output)
	}
}

func TestGroupsAllShowsOlderRuns(t *testing.T) {
	server := listingServer(t, []ListingRun{
		{
			Name:     "eels/consume-engine",
			NTests:   40523,
			Passes:   40467,
			Fails:    56,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 26, 8, 9, 26, 0, time.UTC),
			FileName: "eels.json",
		},
		{
			Name:     "eels/consume-engine",
			NTests:   40523,
			Passes:   40469,
			Fails:    54,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 25, 8, 3, 50, 0, time.UTC),
			FileName: "older-eels.json",
		},
	})
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdGroups([]string{"--base-url", server.URL, "generic", "--all"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "2026-04-25") {
		t.Fatalf("groups --all output did not include older eels run:\n%s", output)
	}
}

func TestGroupsOutputSortedBySuiteThenClient(t *testing.T) {
	server := listingServer(t, []ListingRun{
		{
			Name:     "rpc-compat",
			Passes:   210,
			Fails:    1,
			Clients:  []string{"reth"},
			Start:    time.Date(2026, 4, 27, 8, 18, 24, 0, time.UTC),
			FileName: "rpc-reth.json",
		},
		{
			Name:     "eels/consume-engine",
			Passes:   40448,
			Fails:    75,
			Clients:  []string{"reth"},
			Start:    time.Date(2026, 4, 26, 8, 18, 6, 0, time.UTC),
			FileName: "eels-reth.json",
		},
		{
			Name:     "graphql",
			Passes:   40,
			Fails:    12,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 27, 7, 32, 49, 0, time.UTC),
			FileName: "graphql-geth.json",
		},
		{
			Name:     "eels/consume-engine",
			Passes:   40467,
			Fails:    56,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 26, 8, 9, 26, 0, time.UTC),
			FileName: "eels-geth.json",
		},
	})
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdGroups([]string{"--base-url", server.URL, "generic"})
	})
	if err != nil {
		t.Fatal(err)
	}

	assertLineBefore(t, output, []string{"eels/consume-engine", "go-ethereum"}, []string{"eels/consume-engine", "reth"})
	assertLineBefore(t, output, []string{"eels/consume-engine", "reth"}, []string{"graphql", "go-ethereum"})
	assertLineBefore(t, output, []string{"graphql", "go-ethereum"}, []string{"rpc-compat", "reth"})
}

func TestGroupsClientFlagFiltersAfterGroupName(t *testing.T) {
	server := listingServer(t, []ListingRun{
		{
			Name:     "suite-a",
			Passes:   1,
			Fails:    0,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 27, 8, 18, 24, 0, time.UTC),
			FileName: "suite-a.json",
		},
		{
			Name:     "suite-b",
			Passes:   1,
			Fails:    0,
			Clients:  []string{"reth"},
			Start:    time.Date(2026, 4, 27, 7, 32, 49, 0, time.UTC),
			FileName: "suite-b.json",
		},
	})
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdGroups([]string{"generic", "--base-url", server.URL, "--client", "go-ethereum"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "suite-a") || strings.Contains(output, "suite-b") {
		t.Fatalf("groups --client did not filter output:\n%s", output)
	}
}

func TestGroupsColorsPassFailCounts(t *testing.T) {
	server := listingServer(t, []ListingRun{
		{
			Name:     "suite-a",
			Passes:   1,
			Fails:    1,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 27, 8, 18, 24, 0, time.UTC),
			FileName: "suite-a.json",
		},
		{
			Name:     "suite-b",
			Passes:   0,
			Fails:    0,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 27, 7, 32, 49, 0, time.UTC),
			FileName: "suite-b.json",
		},
	})
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdGroups([]string{"--base-url", server.URL, "generic"})
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		ansiGreen + "1" + ansiReset,
		ansiRed + "1" + ansiReset,
		ansiRed + "0" + ansiReset,
		ansiGreen + "0" + ansiReset,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("groups output does not contain colored count %q:\n%s", want, output)
		}
	}
}

func TestGroupsOmitsFilesByDefaultAndShowsThemWithFlag(t *testing.T) {
	server := listingServer(t, []ListingRun{
		{
			Name:     "suite-a",
			Passes:   1,
			Fails:    0,
			Clients:  []string{"go-ethereum"},
			Start:    time.Date(2026, 4, 27, 8, 18, 24, 0, time.UTC),
			FileName: "suite-a.json",
		},
	})
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdGroups([]string{"--base-url", server.URL, "generic"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "FILE") || strings.Contains(output, "suite-a.json") {
		t.Fatalf("groups output included file column by default:\n%s", output)
	}

	output, err = captureStdout(func() error {
		return cmdGroups([]string{"--base-url", server.URL, "generic", "--files"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "FILE") || !strings.Contains(output, "suite-a.json") {
		t.Fatalf("groups --files output did not include file column:\n%s", output)
	}
}

func TestGroupsLimitStillCapsRows(t *testing.T) {
	server := listingServer(t, sampleRuns(3))
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdGroups([]string{"--base-url", server.URL, "generic", "--limit", "2"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := dataLineCount(output); got != 2 {
		t.Fatalf("expected 2 run rows, got %d:\n%s", got, output)
	}
}

func TestRunsCommandRemoved(t *testing.T) {
	if err := run([]string{"runs"}); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("runs command should be removed, got err=%v", err)
	}
}

func sampleRuns(n int) []ListingRun {
	start := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	runs := make([]ListingRun, 0, n)
	for i := range n {
		runs = append(runs, ListingRun{
			Name:     fmt.Sprintf("suite-%02d", i),
			NTests:   10,
			Passes:   9,
			Fails:    1,
			Clients:  []string{"go-ethereum"},
			Start:    start.Add(-time.Duration(i) * time.Minute),
			FileName: fmt.Sprintf("run-%02d.json", i),
		})
	}
	return runs
}

func assertLineBefore(t *testing.T, output string, beforeTerms, afterTerms []string) {
	t.Helper()
	beforeIdx := lineIndex(output, beforeTerms)
	if beforeIdx == -1 {
		t.Fatalf("output does not contain line with %q:\n%s", beforeTerms, output)
	}
	afterIdx := lineIndex(output, afterTerms)
	if afterIdx == -1 {
		t.Fatalf("output does not contain line with %q:\n%s", afterTerms, output)
	}
	if beforeIdx > afterIdx {
		t.Fatalf("expected %q before %q:\n%s", beforeTerms, afterTerms, output)
	}
}

func lineIndex(output string, terms []string) int {
	for i, line := range strings.Split(output, "\n") {
		found := true
		for _, term := range terms {
			if !strings.Contains(line, term) {
				found = false
				break
			}
		}
		if found {
			return i
		}
	}
	return -1
}

func dataLineCount(output string) int {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return 0
	}
	return len(lines) - 1
}

func listingServer(t *testing.T, runs []ListingRun) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generic/listing.jsonl" {
			http.NotFound(w, r)
			return
		}
		for _, run := range runs {
			data, err := json.Marshal(run)
			if err != nil {
				t.Fatalf("marshal run: %v", err)
			}
			fmt.Fprintln(w, string(data))
		}
	}))
}

func captureStdout(fn func() error) (string, error) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	defer func() {
		os.Stdout = orig
	}()

	runErr := fn()
	if err := w.Close(); err != nil && runErr == nil {
		runErr = err
	}
	data, readErr := io.ReadAll(r)
	if err := r.Close(); err != nil && readErr == nil {
		readErr = err
	}
	if runErr != nil {
		return string(data), runErr
	}
	return string(data), readErr
}
