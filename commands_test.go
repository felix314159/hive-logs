package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCmdClientsJSON(t *testing.T) {
	output, err := captureStdout(func() error {
		return cmdClients([]string{"--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var clients []string
	if err := json.Unmarshal([]byte(output), &clients); err != nil {
		t.Fatal(err)
	}
	if len(clients) == 0 || clients[0] != "besu" {
		t.Fatalf("clients = %v", clients)
	}
}

func TestCmdSuitesJSONListsLatestSuitePerGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discovery.json":
			fmt.Fprint(w, `[{"name":"generic"},{"name":"bal"}]`)
		case "/generic/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "suite-b", Start: time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)},
				{Name: "suite-a", Start: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)},
			})
		case "/bal/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "suite-c", Start: time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdSuites([]string{"--base-url", server.URL, "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var suites []SuiteSummary
	if err := json.Unmarshal([]byte(output), &suites); err != nil {
		t.Fatal(err)
	}
	if len(suites) != 3 || suites[0].Group != "bal" || suites[0].Suite != "suite-c" ||
		suites[1].Group != "generic" || suites[1].Suite != "suite-a" {
		t.Fatalf("suites = %+v", suites)
	}
}

func TestListSuiteClientsAddsDurationAndVersionMetadata(t *testing.T) {
	run := ListingRun{
		Name:     "suite-a",
		Passes:   9,
		Fails:    1,
		NTests:   10,
		Clients:  []string{"go-ethereum_main"},
		Start:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		FileName: "run.json",
	}
	suite := commandSuiteFixture()
	server := commandServer(t, run, suite)
	defer server.Close()

	output, err := captureStdout(func() error {
		return listSuiteClients(nilContext(), newClient(server.URL), "generic", "suite-a", true)
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Hive     *HiveVersion         `json:"hive"`
		Fixtures fixturesInfo         `json:"fixtures"`
		Clients  []SuiteClientSummary `json:"clients"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Hive.Commit != "abcdef123456" || decoded.Fixtures.Release != "v1.0.0" {
		t.Fatalf("metadata = %+v", decoded)
	}
	if len(decoded.Clients) != 1 || decoded.Clients[0].Duration != 5*time.Second ||
		decoded.Clients[0].Version != "1.15.0" || decoded.Clients[0].Commit != "abcdef1" {
		t.Fatalf("clients = %+v", decoded.Clients)
	}
}

func TestCmdListJSONListsMatchingRows(t *testing.T) {
	run := ListingRun{
		Name:     "suite-a",
		Clients:  []string{"go-ethereum_main"},
		Start:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		FileName: "run.json",
	}
	server := commandServer(t, run, commandSuiteFixture())
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdList([]string{
			"--base-url", server.URL,
			"--suite", "suite-a",
			"--client", "geth",
			"--test", "engine",
			"--status", "all",
			"--json",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var rows []ListRow
	if err := json.Unmarshal([]byte(output), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Status != "FAIL" || !rows[0].HasHiveLog || rows[0].ClientLogCount != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestCmdFetchJSONWritesBundle(t *testing.T) {
	run := ListingRun{
		Name:     "suite-a",
		Clients:  []string{"go-ethereum_main"},
		Start:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		FileName: "run.json",
	}
	server := commandServer(t, run, commandSuiteFixture())
	defer server.Close()

	outDir := t.TempDir()
	output, err := captureStdout(func() error {
		return cmdFetch([]string{
			"--base-url", server.URL,
			"--suite", "suite-a",
			"--client", "geth",
			"--test", "engine",
			"--out", outDir,
			"--json",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var bundles []BundleSummary
	if err := json.Unmarshal([]byte(output), &bundles); err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 || !strings.HasPrefix(bundles[0].Directory, filepath.Clean(outDir)) {
		t.Fatalf("bundles = %+v", bundles)
	}
	if !strings.HasSuffix(bundles[0].ClientLogPath, "go-ethereum.log") {
		t.Fatalf("client log path = %q", bundles[0].ClientLogPath)
	}
}

func TestSuiteDurationUsesEarliestStartAndLatestEnd(t *testing.T) {
	suite := &SuiteResult{TestCases: map[string]TestCase{
		"1": {
			Start: time.Date(2026, 4, 28, 12, 0, 5, 0, time.UTC),
			End:   time.Date(2026, 4, 28, 12, 0, 7, 0, time.UTC),
		},
		"2": {
			Start: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 4, 28, 12, 0, 10, 0, time.UTC),
		},
	}}
	if got := suiteDuration(suite); got != 10*time.Second {
		t.Fatalf("duration = %s", got)
	}
}

func commandServer(t *testing.T, run ListingRun, suite *SuiteResult) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generic/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{run})
		case "/generic/results/run.json":
			if err := json.NewEncoder(w).Encode(suite); err != nil {
				t.Fatal(err)
			}
		case "/generic/results/details.log":
			w.Write([]byte("hive-log-data"))
		case "/generic/results/client.log":
			w.Write([]byte("client-log-data"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func commandSuiteFixture() *SuiteResult {
	start := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	return &SuiteResult{
		Name:           "suite-a",
		ClientVersions: map[string]string{"go-ethereum_main": "Geth/v1.15.0/linux abcdef123456"},
		TestDetailsLog: "details.log",
		RunMetadata: &RunMetadata{
			HiveCommand: []string{
				"hive",
				"--sim.buildarg", "fixtures=https://github.com/ethereum/execution-spec-tests/releases/download/v1.0.0/fixtures.tar.gz",
			},
			HiveVersion: &HiveVersion{Commit: "abcdef123456", Branch: "main"},
			ClientConfig: &ClientConfig{Content: &ClientConfigContent{Clients: []ClientConfigEntry{
				{Client: "go-ethereum", BuildArgs: map[string]string{"tag": "master"}},
			}}},
		},
		TestCases: map[string]TestCase{
			"1": {
				Name:  "engine failure",
				Start: start,
				End:   start.Add(5 * time.Second),
				SummaryResult: SummaryResult{
					Pass:    false,
					Details: "boom",
					Log:     &LogRange{Begin: 0, End: 4},
				},
				ClientInfo: map[string]ClientInfo{
					"1": {ID: "1", Name: "geth", LogFile: "client.log", LogOffsets: &LogRange{Begin: 0, End: 6}},
				},
			},
		},
	}
}

func writeListingRuns(t *testing.T, w http.ResponseWriter, runs []ListingRun) {
	t.Helper()
	for _, run := range runs {
		if err := json.NewEncoder(w).Encode(run); err != nil {
			t.Fatal(err)
		}
	}
}

func nilContext() context.Context {
	return context.Background()
}
