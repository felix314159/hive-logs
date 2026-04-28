package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeAndBundleDirName(t *testing.T) {
	if got := sanitizeFileName("  Engine API: New/Payload!!  "); got != "Engine-API-New-Payload" {
		t.Fatalf("sanitizeFileName() = %q", got)
	}
	if got := sanitizePathSegments("eels/consume engine"); got != "eels/consume-engine" {
		t.Fatalf("sanitizePathSegments() = %q", got)
	}
	dir := bundleDirName("Generic Group", "eels/consume-engine", "geth", ListingRun{FileName: "run.json"}, TestMatch{
		Test: TestCase{Name: "A Failing Test"},
	})
	if dir != filepath.Join("generic-group", "eels", "consume-engine", "go-ethereum", "a-failing-test-run") {
		t.Fatalf("bundle dir = %q", dir)
	}
}

func TestWriteJSONFileAndReproduceCommands(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "summary.json")
	if err := writeJSONFile(jsonPath, map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]int
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["n"] != 1 || !strings.Contains(string(data), "\n  \"n\"") {
		t.Fatalf("summary json = %s", data)
	}

	reproPath := filepath.Join(dir, "reproduce_commands.md")
	if err := writeReproduceCommands(reproPath, FailureMetadata{
		Group:           "generic",
		Suite:           "suite-a",
		Client:          "go-ethereum",
		RunFile:         "run.json",
		RunStart:        time.Date(2026, 4, 28, 12, 0, 0, 0, time.Local),
		TestID:          "1",
		TestName:        "failing test",
		TestDescription: "description",
		HiveCommand:     []string{"hive", "--sim", "suite-a"},
	}, "hive.log", "go-ethereum.log"); err != nil {
		t.Fatal(err)
	}
	repro, err := os.ReadFile(reproPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Reproduce Commands", "Hive command:", "Analyze `hive.log` and `go-ethereum.log`"} {
		if !strings.Contains(string(repro), want) {
			t.Fatalf("reproduce commands missing %q:\n%s", want, repro)
		}
	}
}

func TestFetchBundleWritesSummaryLogsAndReproduceCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generic/results/details.log":
			w.Write([]byte("hive-log-data"))
		case "/generic/results/client.log":
			w.Write([]byte("abcdef"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	run := ListingRun{
		Name:     "suite-a",
		Clients:  []string{"go-ethereum_main"},
		Start:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		FileName: "run.json",
	}
	suite := &SuiteResult{
		Name:           "suite-a",
		TestDetailsLog: "details.log",
		ClientVersions: map[string]string{"go-ethereum_main": "Geth/v1.0/linux"},
	}
	match := TestMatch{TestID: "1", Test: TestCase{
		Name: "failing test",
		SummaryResult: SummaryResult{
			Details: "boom",
			Log:     &LogRange{Begin: 0, End: 4},
		},
		ClientInfo: map[string]ClientInfo{
			"1": {ID: "1", Name: "geth", LogFile: "client.log", LogOffsets: &LogRange{Begin: 1, End: 4}},
		},
	}}

	outDir := t.TempDir()
	bundle, err := fetchBundle(context.Background(), newClient(server.URL), fetchFlags{
		common: commonFlags{group: "generic", suite: "suite-a", client: "geth"},
		outDir: outDir,
	}, run, suite, match)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.TestID != "1" || bundle.RunFile != "run.json" || len(bundle.ClientLogs) != 1 {
		t.Fatalf("bundle = %+v", bundle)
	}

	hiveLog, err := os.ReadFile(bundle.HiveLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(hiveLog) != "boom\n\nhive" {
		t.Fatalf("hive log = %q", hiveLog)
	}
	clientLog, err := os.ReadFile(bundle.ClientLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clientLog), "bcd") {
		t.Fatalf("client log = %q", clientLog)
	}
	if _, err := os.Stat(bundle.SummaryPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundle.ReproduceCommandsPath); err != nil {
		t.Fatal(err)
	}
}
