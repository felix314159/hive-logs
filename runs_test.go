package main

import (
	"reflect"
	"testing"
	"time"
)

func TestFilterRunsMatchesSimulatorNameClientAliasAndLatest(t *testing.T) {
	runs := []ListingRun{
		{Name: "eels/consume-engine", Clients: []string{"go-ethereum_main"}, Start: time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC), FileName: "old.json"},
		{Name: "eels/consume-engine", Clients: []string{"go-ethereum_main"}, Start: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC), FileName: "new.json"},
		{Name: "rpc", Clients: []string{"reth"}, Start: time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC), FileName: "rpc.json"},
	}

	got := filterRuns(runs, "consume-engine", "geth", "latest")
	if len(got) != 1 || got[0].FileName != "new.json" {
		t.Fatalf("filtered runs = %+v", got)
	}
}

func TestLatestBySuiteClientKeepsOneRunPerSuiteClient(t *testing.T) {
	runs := []ListingRun{
		{Name: "suite-a", Clients: []string{"go-ethereum"}, FileName: "first.json"},
		{Name: "suite-a", Clients: []string{"go-ethereum"}, FileName: "second.json"},
		{Name: "suite-a", Clients: []string{"reth"}, FileName: "third.json"},
	}
	got := latestBySuiteClient(runs)
	if names := []string{got[0].FileName, got[1].FileName}; !reflect.DeepEqual(names, []string{"first.json", "third.json"}) {
		t.Fatalf("latest files = %v", names)
	}
}

func TestSortRunsForDisplayOrdersSuiteClientTimeFile(t *testing.T) {
	runs := []ListingRun{
		{Name: "suite-b", Clients: []string{"reth"}, Start: time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC), FileName: "b.json"},
		{Name: "suite-a", Clients: []string{"reth"}, Start: time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC), FileName: "a-reth.json"},
		{Name: "suite-a", Clients: []string{"go-ethereum"}, Start: time.Date(2026, 4, 28, 7, 0, 0, 0, time.UTC), FileName: "a-geth.json"},
		{Name: "suite-a", Clients: []string{"go-ethereum"}, Start: time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC), FileName: "a-geth-new.json"},
	}
	sortRunsForDisplay(runs)
	got := []string{runs[0].FileName, runs[1].FileName, runs[2].FileName, runs[3].FileName}
	want := []string{"a-geth-new.json", "a-geth.json", "a-reth.json", "b.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRunHasClientNormalizesKnownNames(t *testing.T) {
	run := ListingRun{Clients: []string{"nimbus-el_dev"}}
	if !runHasClient(run, "nimbusel") {
		t.Fatalf("expected alias to match client")
	}
}
