package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCmdListCombinesGroupsSuitesAndClients(t *testing.T) {
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
		return cmdList([]string{"--base-url", server.URL})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"GROUPS",
		"generic",
		"SUITES",
		"suite-a",
		"CLIENTS",
		"besu",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("list output does not contain %q:\n%s", want, output)
		}
	}
}

func TestCmdListJSONCombinesGroupsSuitesAndClients(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discovery.json":
			fmt.Fprint(w, `[{"name":"generic"}]`)
		case "/generic/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "suite-a", Start: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output, err := captureStdout(func() error {
		return cmdList([]string{"--base-url", server.URL, "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var summary ListSummary
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Groups) != 1 || summary.Groups[0].Name != "generic" ||
		len(summary.Suites) != 1 || summary.Suites[0].Suite != "suite-a" ||
		len(summary.Clients) == 0 || summary.Clients[0] != "besu" {
		t.Fatalf("summary = %+v", summary)
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

func TestFormatVectorFileCount(t *testing.T) {
	cases := []struct {
		vectors, files int
		want           string
	}{
		{14, 1, "14 failing test vectors in 1 test file"},
		{1, 1, "1 failing test vector in 1 test file"},
		{5, 3, "5 failing test vectors in 3 test files"},
		{2, 0, "2 failing test vectors"},
	}
	for _, tc := range cases {
		if got := formatVectorFileCount(tc.vectors, tc.files); got != tc.want {
			t.Fatalf("formatVectorFileCount(%d, %d) = %q, want %q", tc.vectors, tc.files, got, tc.want)
		}
	}
}

func TestCountTestFilesIgnoresEmpty(t *testing.T) {
	bundles := []BundleSummary{
		{TestFile: "tests/foo.py"},
		{TestFile: "tests/foo.py"},
		{TestFile: "tests/bar.py"},
		{TestFile: ""},
	}
	if got := countTestFiles(bundles); got != 2 {
		t.Fatalf("countTestFiles = %d, want 2", got)
	}
}

func TestPrintBundlesGroupedByFile(t *testing.T) {
	bundles := []BundleSummary{
		{
			TestName:              "tests/foo.py::test_a[x]",
			TestFile:              "tests/foo.py",
			TestVector:            "test_a[x]",
			HiveLogPath:           "logs/foo/a/hive.log",
			ClientLogPath:         "logs/foo/a/client.log",
			ReproduceCommandsPath: "logs/foo/a/reproduce_commands.md",
		},
		{
			TestName:              "tests/foo.py::test_a[y]",
			TestFile:              "tests/foo.py",
			TestVector:            "test_a[y]",
			HiveLogPath:           "logs/foo/b/hive.log",
			ClientLogPath:         "logs/foo/b/client.log",
			ReproduceCommandsPath: "logs/foo/b/reproduce_commands.md",
		},
		{
			TestName:              "tests/bar.py::test_z",
			TestFile:              "tests/bar.py",
			TestVector:            "test_z",
			HiveLogPath:           "logs/bar/z/hive.log",
			ClientLogPath:         "logs/bar/z/client.log",
			ReproduceCommandsPath: "logs/bar/z/reproduce_commands.md",
		},
	}
	var buf bytes.Buffer
	printBundlesGroupedByFile(&buf, bundles)
	out := buf.String()
	if !strings.Contains(out, "tests/foo.py\n  • test_a[x]") {
		t.Fatalf("missing foo.py header with indented bullet:\n%s", out)
	}
	if !strings.Contains(out, "  • test_a[y]") {
		t.Fatalf("missing second foo.py vector:\n%s", out)
	}
	if !strings.Contains(out, "tests/bar.py\n  • test_z") {
		t.Fatalf("missing bar.py header with indented bullet:\n%s", out)
	}
	fooIdx := strings.Index(out, "tests/foo.py")
	barIdx := strings.Index(out, "tests/bar.py")
	if fooIdx == -1 || barIdx == -1 || fooIdx > barIdx {
		t.Fatalf("groups out of order:\n%s", out)
	}
}

func TestPrintBundlesGroupedByFileNoFile(t *testing.T) {
	bundles := []BundleSummary{
		{TestName: "client launch", HiveLogPath: "h", ClientLogPath: "c", ReproduceCommandsPath: "r"},
	}
	var buf bytes.Buffer
	printBundlesGroupedByFile(&buf, bundles)
	out := buf.String()
	if strings.Contains(out, "  •") {
		t.Fatalf("unexpected indentation when no file is present:\n%s", out)
	}
	if !strings.HasPrefix(out, "• client launch\n") {
		t.Fatalf("expected unindented bullet, got:\n%s", out)
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
