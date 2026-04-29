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

func TestBundleDirNameSplitsPytestStyleNames(t *testing.T) {
	run := ListingRun{FileName: "run.json"}
	dirA := bundleDirName("bal", "eels/consume-engine", "nimbus-el", run, TestMatch{
		TestID: "12",
		Test: TestCase{Name: "tests/prague/eip7002_el_triggerable_withdrawals/test_withdrawal_requests.py" +
			"::test_withdrawal_requests[fork_Amsterdam-blockchain_test_engine-single_block_single_withdrawal_request_from_contract_call_depth_3]-nimbus-el_default"},
	})
	dirB := bundleDirName("bal", "eels/consume-engine", "nimbus-el", run, TestMatch{
		TestID: "13",
		Test: TestCase{Name: "tests/prague/eip7002_el_triggerable_withdrawals/test_withdrawal_requests.py" +
			"::test_withdrawal_requests[fork_Amsterdam-blockchain_test_engine-single_block_single_withdrawal_request_from_contract_call_depth_high]-nimbus-el_default"},
	})
	if dirA == dirB {
		t.Fatalf("bundle dirs collide: %q", dirA)
	}
	wantPrefix := filepath.Join("bal", "eels", "consume-engine", "nimbus-el",
		"tests-prague-eip7002-el-triggerable-withdrawals-test-withdrawal-requests-py-run") + string(filepath.Separator)
	if !strings.HasPrefix(dirA, wantPrefix) {
		t.Fatalf("dirA missing file-level prefix %q: %q", wantPrefix, dirA)
	}
	if !strings.HasSuffix(dirA, "-12") {
		t.Fatalf("dirA missing test-id suffix: %q", dirA)
	}
	if !strings.HasSuffix(dirB, "-13") {
		t.Fatalf("dirB missing test-id suffix: %q", dirB)
	}
}

func TestSplitTestName(t *testing.T) {
	cases := []struct {
		name       string
		wantFile   string
		wantVector string
	}{
		{"tests/foo.py::test_bar[x]-geth", "tests/foo.py", "test_bar[x]-geth"},
		{"plain test name", "", "plain test name"},
		{"::leading", "", "leading"},
		{"Blob Transaction Ordering, Multiple Accounts (Cancun) (geth_default)", "Blob Transaction Ordering", "Multiple Accounts (Cancun) (geth_default)"},
		// The consensus simulator's "test file loader" meta-test has no inner
		// vector and must be treated as a file-only entry, not a bullet orphan.
		{"test file loader", "test file loader", ""},
	}
	for _, tc := range cases {
		gotFile, gotVector := splitTestName(tc.name)
		if gotFile != tc.wantFile || gotVector != tc.wantVector {
			t.Fatalf("splitTestName(%q) = (%q, %q), want (%q, %q)", tc.name, gotFile, gotVector, tc.wantFile, tc.wantVector)
		}
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

func TestFetchBundleWritesNoClientLogPlaceholder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generic/results/details.log":
			w.Write([]byte("hive-log-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	run := ListingRun{
		Name:     "suite-a",
		Clients:  []string{"go-ethereum"},
		Start:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		FileName: "run.json",
	}
	suite := &SuiteResult{Name: "suite-a", TestDetailsLog: "details.log"}
	match := TestMatch{TestID: "1", Test: TestCase{
		Name:          "failing test",
		SummaryResult: SummaryResult{Log: &LogRange{Begin: 0, End: 4}},
	}}

	bundle, err := fetchBundle(context.Background(), newClient(server.URL), fetchFlags{
		common: commonFlags{group: "generic", suite: "suite-a", client: "geth"},
		outDir: t.TempDir(),
	}, run, suite, match)
	if err != nil {
		t.Fatal(err)
	}
	clientLog, err := os.ReadFile(bundle.ClientLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(clientLog) != "no client log exists for this test\n" {
		t.Fatalf("client log = %q", clientLog)
	}
}

func TestFetchBundleRPCCompatWritesClientLaunchLogAndReference(t *testing.T) {
	var launchRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generic/results/details.log":
			w.Write([]byte("hive-log-data"))
		case "/generic/results/launch.log":
			launchRequests++
			if got := r.Header.Get("Range"); got != "" {
				t.Fatalf("client launch Range = %q", got)
			}
			w.Write([]byte("launch-log-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	run := ListingRun{
		Name:     "rpc-compat",
		Clients:  []string{"go-ethereum"},
		Start:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		FileName: "run.json",
	}
	suite := &SuiteResult{
		Name:           "rpc-compat",
		TestDetailsLog: "details.log",
		TestCases: map[string]TestCase{
			"0": {
				Name: "client launch (go-ethereum_default)",
				ClientInfo: map[string]ClientInfo{
					"1": {ID: "1", Name: "go-ethereum_default", LogFile: "launch.log", LogOffsets: &LogRange{Begin: 1, End: 3}},
				},
			},
		},
	}
	match := TestMatch{TestID: "1", Test: TestCase{
		Name:          "rpc failing test",
		SummaryResult: SummaryResult{Log: &LogRange{Begin: 0, End: 4}},
	}}

	outDir := t.TempDir()
	bundle, err := fetchBundle(context.Background(), newClient(server.URL), fetchFlags{
		common: commonFlags{group: "generic", suite: "rpc-compat", client: "geth"},
		outDir: outDir,
	}, run, suite, match)
	if err != nil {
		t.Fatal(err)
	}

	launchPath := filepath.Join(outDir, "generic", "rpc-compat", "go-ethereum", "client_launch.log")
	launchLog, err := os.ReadFile(launchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launchLog), "launch-log-data") {
		t.Fatalf("client launch log = %q", launchLog)
	}
	if launchRequests != 1 {
		t.Fatalf("launch requests = %d", launchRequests)
	}

	clientLog, err := os.ReadFile(bundle.ClientLogPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "no client log exists for this test; see ../client_launch.log for the rpc-compat client launch log\n"
	if string(clientLog) != want {
		t.Fatalf("client log = %q, want %q", clientLog, want)
	}
}

func TestFindRPCCompatClientLaunchPrefersRequestedClient(t *testing.T) {
	suite := &SuiteResult{TestCases: map[string]TestCase{
		"1": {
			Name: "client launch (reth_default)",
			ClientInfo: map[string]ClientInfo{
				"1": {Name: "reth_default", LogFile: "reth_default/client.log"},
			},
		},
		"2": {
			Name: "client launch (go-ethereum_default)",
			ClientInfo: map[string]ClientInfo{
				"1": {Name: "go-ethereum_default", LogFile: "go-ethereum_default/client.log"},
			},
		},
	}}

	match, ok := findRPCCompatClientLaunch(suite, "geth")
	if !ok {
		t.Fatal("client launch not found")
	}
	if match.TestID != "2" {
		t.Fatalf("matched test id = %q", match.TestID)
	}
}
