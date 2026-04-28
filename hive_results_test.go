package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchGroupsListingAndSuite(t *testing.T) {
	run := ListingRun{
		Name:     "suite-a",
		Clients:  []string{"go-ethereum"},
		Start:    time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC),
		FileName: "run.json",
	}
	suite := SuiteResult{Name: "suite-a"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discovery.json":
			fmt.Fprint(w, `[{"name":"generic","address":"addr","github_workflows":["wf"]}]`)
		case "/generic/listing.jsonl":
			fmt.Fprintln(w)
			if err := json.NewEncoder(w).Encode(run); err != nil {
				t.Fatal(err)
			}
		case "/generic/results/run.json":
			if err := json.NewEncoder(w).Encode(suite); err != nil {
				t.Fatal(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newClient(server.URL)
	groups, err := fetchGroups(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != "generic" || groups[0].GitHubWorkflows[0] != "wf" {
		t.Fatalf("groups = %+v", groups)
	}

	runs, err := fetchListing(context.Background(), client, "generic")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].FileName != "run.json" {
		t.Fatalf("runs = %+v", runs)
	}

	gotSuite, err := fetchSuite(context.Background(), client, "generic", "run.json")
	if err != nil {
		t.Fatal(err)
	}
	if gotSuite.Name != "suite-a" {
		t.Fatalf("suite = %+v", gotSuite)
	}
}

func TestFetchListingReportsBadLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"name":"ok"}`)
		fmt.Fprintln(w, `{bad`)
	}))
	defer server.Close()

	_, err := fetchListing(context.Background(), newClient(server.URL), "generic")
	if err == nil || !strings.Contains(err.Error(), "decode listing line") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectRunUsesRunFileWithoutNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("selectRun should not call server for explicit run-file")
	}))
	defer server.Close()

	run, err := selectRun(context.Background(), newClient(server.URL), commonFlags{
		suite:   "suite-a",
		client:  "reth",
		runFile: "manual.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Name != "suite-a" || run.FileName != "manual.json" || run.Clients[0] != "reth" {
		t.Fatalf("run = %+v", run)
	}
}

func TestSelectRunChoosesNewestMatchingRun(t *testing.T) {
	server := listingServer(t, []ListingRun{
		{Name: "suite-a", Clients: []string{"go-ethereum"}, Start: time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC), FileName: "old.json"},
		{Name: "suite-a", Clients: []string{"go-ethereum"}, Start: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC), FileName: "new.json"},
	})
	defer server.Close()

	run, err := selectRun(context.Background(), newClient(server.URL), commonFlags{
		group:  "generic",
		suite:  "suite-a",
		client: "geth",
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.FileName != "new.json" {
		t.Fatalf("run = %+v", run)
	}
}
