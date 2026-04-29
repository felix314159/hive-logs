package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchHiveLogFallsBackToSummaryDetails(t *testing.T) {
	data, err := fetchHiveLog(context.Background(), newClient("http://unused"), "generic", &SuiteResult{}, TestMatch{
		Test: TestCase{SummaryResult: SummaryResult{Details: "summary details"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "summary details" {
		t.Fatalf("data = %q", data)
	}
}

func TestFetchHiveLogPrependsDetailsToRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generic/results/details.log" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got, want := r.Header.Get("Range"), "bytes=2-5"; got != want {
			t.Fatalf("Range = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("cdef"))
	}))
	defer server.Close()

	data, err := fetchHiveLog(context.Background(), newClient(server.URL), "generic", &SuiteResult{TestDetailsLog: "details.log"}, TestMatch{
		Test: TestCase{SummaryResult: SummaryResult{
			Details: "boom",
			Log:     &LogRange{Begin: 2, End: 6},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "boom\n\ncdef" {
		t.Fatalf("data = %q", data)
	}
}

func TestFetchClientLogsSortsClientsAndUsesOffsets(t *testing.T) {
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		w.WriteHeader(http.StatusPartialContent)
		switch r.URL.Path {
		case "/generic/results/a.log":
			w.Write([]byte("aaa"))
		case "/generic/results/b.log":
			w.Write([]byte("bbb"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	match := TestMatch{Test: TestCase{ClientInfo: map[string]ClientInfo{
		"b": {ID: "2", Name: "second", IP: "10.0.0.2", LogFile: "b.log", LogOffsets: &LogRange{Begin: 200, End: 300}},
		"a": {ID: "1", Name: "first", IP: "10.0.0.1", LogFile: "a.log", LogOffsets: &LogRange{Begin: 100, End: 200}},
	}}}
	data, files, err := fetchClientLogs(context.Background(), newClient(server.URL), fetchFlags{common: commonFlags{group: "generic"}}, match)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(data), "id=1") > strings.Index(string(data), "id=2") {
		t.Fatalf("client logs not sorted:\n%s", data)
	}
	if strings.Join(files, ",") != "a.log,b.log" {
		t.Fatalf("files = %v", files)
	}
	if strings.Join(ranges, ",") != "bytes=100-199,bytes=200-299" {
		t.Fatalf("ranges = %v", ranges)
	}
}

func TestFetchClientLogsFullClientSkipsRangeHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "" {
			t.Fatalf("Range = %q", got)
		}
		w.Write([]byte("full log"))
	}))
	defer server.Close()

	_, _, err := fetchClientLogs(context.Background(), newClient(server.URL), fetchFlags{
		common:     commonFlags{group: "generic"},
		fullClient: true,
	}, TestMatch{Test: TestCase{ClientInfo: map[string]ClientInfo{
		"1": {ID: "1", LogFile: "client.log", LogOffsets: &LogRange{Begin: 1, End: 2}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
}

// When Hive records a near-empty LogOffsets slice (e.g. begin=37034 end=37035
// for a test that never actually ran because the consensus simulator's
// "test file loader" meta-test failed earlier), the slice is not useful for
// diagnosis. fetchClientLogs must ignore the offsets and download the full
// client log so the bundle has the actual content.
func TestFetchClientLogsFallsBackToFullLogForDegenerateOffsets(t *testing.T) {
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		w.Write([]byte("full client log content from start to finish"))
	}))
	defer server.Close()

	match := TestMatch{Test: TestCase{ClientInfo: map[string]ClientInfo{
		"1": {ID: "1", Name: "first", IP: "10.0.0.1", LogFile: "client.log", LogOffsets: &LogRange{Begin: 37034, End: 37035}},
	}}}
	data, _, err := fetchClientLogs(context.Background(), newClient(server.URL), fetchFlags{common: commonFlags{group: "generic"}}, match)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 || ranges[0] != "" {
		t.Fatalf("expected a single full-log fetch with no Range header, got %v", ranges)
	}
	if !strings.Contains(string(data), "full client log content") {
		t.Fatalf("expected full log content in bundle, got:\n%s", data)
	}
}

// A zero-byte LogOffsets slice (begin == end) is also degenerate and must
// trigger the same full-log fallback rather than producing an empty bundle.
func TestFetchClientLogsFallsBackToFullLogForZeroByteOffsets(t *testing.T) {
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		w.Write([]byte("complete client log"))
	}))
	defer server.Close()

	match := TestMatch{Test: TestCase{ClientInfo: map[string]ClientInfo{
		"1": {ID: "1", Name: "first", IP: "10.0.0.1", LogFile: "client.log", LogOffsets: &LogRange{Begin: 1234, End: 1234}},
	}}}
	data, _, err := fetchClientLogs(context.Background(), newClient(server.URL), fetchFlags{common: commonFlags{group: "generic"}}, match)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 || ranges[0] != "" {
		t.Fatalf("expected a single full-log fetch with no Range header, got %v", ranges)
	}
	if !strings.Contains(string(data), "complete client log") {
		t.Fatalf("expected full log content in bundle, got:\n%s", data)
	}
}

func TestFetchClientLogsReportsNoFiles(t *testing.T) {
	_, _, err := fetchClientLogs(context.Background(), newClient("http://unused"), fetchFlags{}, TestMatch{
		Test: TestCase{ClientInfo: map[string]ClientInfo{"1": {ID: "1"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "no client log exists for this test") {
		t.Fatalf("err = %v", err)
	}
}
