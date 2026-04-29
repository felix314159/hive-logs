package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCmdQueryWithoutGroupListsGroupsWhenSuiteNotUnique(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discovery.json":
			fmt.Fprint(w, `[{"name":"bal"},{"name":"bal-quick"},{"name":"other"}]`)
		case "/bal/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "eels/consume-engine", Start: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC), Clients: []string{"besu_main"}},
			})
		case "/bal-quick/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "eels/consume-engine", Start: time.Date(2026, 4, 28, 13, 0, 0, 0, time.UTC), Clients: []string{"besu_main"}},
			})
		case "/other/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "engine-api", Start: time.Date(2026, 4, 28, 14, 0, 0, 0, time.UTC), Clients: []string{"besu_main"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := cmdQuery([]string{"--base-url", server.URL, "suite=eels/consume-engine"})
	if err == nil {
		t.Fatal("expected error when suite exists in multiple groups and group= is missing")
	}
	for _, want := range []string{"because", "multiple groups", "available groups", "bal", "bal-quick", "other"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestCmdQueryWithoutGroupErrorsWhenSuiteNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discovery.json":
			fmt.Fprint(w, `[{"name":"generic"}]`)
		case "/generic/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "engine-api", Start: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC), Clients: []string{"besu_main"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := cmdQuery([]string{"--base-url", server.URL, "suite=nope"})
	if err == nil {
		t.Fatal("expected error when suite does not exist in any group")
	}
	if !strings.Contains(err.Error(), `suite "nope" not found in any group`) {
		t.Fatalf("error %q missing suite-not-found wording", err.Error())
	}
}

func TestCmdQueryClientWithoutSuiteListsAvailableWhenMultiple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discovery.json":
			fmt.Fprint(w, `[{"name":"generic"}]`)
		case "/generic/listing.jsonl":
			writeListingRuns(t, w, []ListingRun{
				{Name: "suite-a", Start: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC), Clients: []string{"besu_main"}},
				{Name: "suite-b", Start: time.Date(2026, 4, 28, 13, 0, 0, 0, time.UTC), Clients: []string{"besu_main"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := cmdQuery([]string{"--base-url", server.URL, "group=generic", "client=besu"})
	if err == nil {
		t.Fatal("expected error when multiple suites and suite= is missing")
	}
	for _, want := range []string{"multiple suites", "suite-a", "suite-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
}

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

func TestHydrateSuiteSummariesFetchesEachRunFileOnce(t *testing.T) {
	suite := commandSuiteFixture()
	suite.ClientVersions = map[string]string{
		"go-ethereum_main": "Geth/v1.15.0/linux abcdef123456",
		"reth_main":        "Reth Version: 0.2.0 ffeeddc",
	}
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generic/results/run.json":
			atomic.AddInt32(&requests, 1)
			if err := json.NewEncoder(w).Encode(suite); err != nil {
				t.Fatal(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out := []SuiteClientSummary{
		{Client: "go-ethereum", RunFile: "run.json"},
		{Client: "reth", RunFile: "run.json"},
	}
	hydrateSuiteSummaries(nilContext(), newClient(server.URL), "generic", out, false, true)
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("run.json requests = %d, want 1 (clients sharing a run file should dedup)", got)
	}
	if out[0].Version == "" || out[1].Version == "" {
		t.Fatalf("clients missing per-client version after hydrate: %+v", out)
	}
}

func TestHydrateSuiteSummariesSkipsTestCasesWhenNoDurationRequested(t *testing.T) {
	suite := commandSuiteFixture()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generic/results/run.json" {
			http.NotFound(w, r)
			return
		}
		// Encode just the header fields (mirroring Hive's order: testCases is last).
		// The handler closes the connection partway through testCases to prove
		// hydrate doesn't need it.
		header := map[string]any{
			"id":             suite.ID,
			"name":           suite.Name,
			"clientVersions": suite.ClientVersions,
			"runMetadata":    suite.RunMetadata,
		}
		buf, err := json.Marshal(header)
		if err != nil {
			t.Fatal(err)
		}
		// Strip trailing `}` and append a testCases prefix so the stream is
		// well-formed up to (but not past) the testCases value.
		body := append(buf[:len(buf)-1], []byte(`,"testCases":{`)...)
		w.Write(body)
		// Do not close the JSON object — header fetch should bail before this matters.
	}))
	defer server.Close()

	out := []SuiteClientSummary{
		{Client: "go-ethereum", RunFile: "run.json"},
	}
	fixtures, hv := hydrateSuiteSummaries(nilContext(), newClient(server.URL), "generic", out, false, false)
	if out[0].Version == "" || out[0].Branch == "" {
		t.Fatalf("expected version and branch from header fetch, got %+v", out[0])
	}
	if out[0].Duration != 0 {
		t.Fatalf("duration should be zero when --duration is off, got %s", out[0].Duration)
	}
	if hv == nil || hv.Commit == "" {
		t.Fatalf("expected hive version from header, got %+v", hv)
	}
	if fixtures.Release == "" {
		t.Fatalf("expected fixtures from header, got %+v", fixtures)
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
		return listSuiteClients(nilContext(), newClient(server.URL), "generic", "suite-a", true, true)
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
	printBundlesGroupedByFile(&buf, bundles, true)
	out := buf.String()
	if !strings.Contains(out, ansiRed+"tests/foo.py"+ansiReset+"\n  • "+ansiOrange+"test_a[x]"+ansiReset) {
		t.Fatalf("missing red foo.py header with orange-labeled bullet:\n%s", out)
	}
	if !strings.Contains(out, "  • "+ansiOrange+"test_a[y]"+ansiReset) {
		t.Fatalf("missing second foo.py orange-labeled vector:\n%s", out)
	}
	if !strings.Contains(out, ansiRed+"tests/bar.py"+ansiReset+"\n  • "+ansiOrange+"test_z"+ansiReset) {
		t.Fatalf("missing red bar.py header with orange-labeled bullet:\n%s", out)
	}
	if !strings.Contains(out, ansiGrey+strings.Repeat("─", 80)+ansiReset) {
		t.Fatalf("missing grey divider between groups:\n%s", out)
	}
	if !strings.Contains(out, "hive log:") || !strings.Contains(out, "client log:") || !strings.Contains(out, "reproduce:") {
		t.Fatalf("expected log paths when showLogPaths=true:\n%s", out)
	}
	fooIdx := strings.Index(out, "tests/foo.py")
	barIdx := strings.Index(out, "tests/bar.py")
	if fooIdx == -1 || barIdx == -1 || fooIdx > barIdx {
		t.Fatalf("groups out of order:\n%s", out)
	}
}

func TestPrintBundlesGroupedByFileHidesLogPathsByDefault(t *testing.T) {
	bundles := []BundleSummary{
		{
			TestName:              "tests/foo.py::test_a",
			TestFile:              "tests/foo.py",
			TestVector:            "test_a",
			HiveLogPath:           "logs/foo/a/hive.log",
			ClientLogPath:         "logs/foo/a/client.log",
			ReproduceCommandsPath: "logs/foo/a/reproduce_commands.md",
		},
	}
	var buf bytes.Buffer
	printBundlesGroupedByFile(&buf, bundles, false)
	out := buf.String()
	if !strings.Contains(out, ansiRed+"tests/foo.py"+ansiReset+"\n  • "+ansiOrange+"test_a"+ansiReset) {
		t.Fatalf("missing red header + orange bullet:\n%s", out)
	}
	for _, banned := range []string{"hive log:", "client log:", "reproduce:", "logs/foo/a"} {
		if strings.Contains(out, banned) {
			t.Fatalf("output should not contain %q when showLogPaths=false:\n%s", banned, out)
		}
	}
}

func TestPrintBundlesGroupedByFileNoFile(t *testing.T) {
	bundles := []BundleSummary{
		{TestName: "client launch", HiveLogPath: "h", ClientLogPath: "c", ReproduceCommandsPath: "r"},
	}
	var buf bytes.Buffer
	printBundlesGroupedByFile(&buf, bundles, true)
	out := buf.String()
	if strings.Contains(out, "  •") {
		t.Fatalf("unexpected indentation when no file is present:\n%s", out)
	}
	if !strings.HasPrefix(out, "• "+ansiOrange+"client launch"+ansiReset+"\n") {
		t.Fatalf("expected unindented orange-labeled bullet, got:\n%s", out)
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
