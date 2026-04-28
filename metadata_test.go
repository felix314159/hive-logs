package main

import (
	"strings"
	"testing"
	"time"
)

func TestBuildMetadataNormalizesSortsAndCleansFields(t *testing.T) {
	run := ListingRun{
		Clients:  []string{"go-ethereum_main"},
		Start:    time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		FileName: "run file.json",
	}
	suite := &SuiteResult{
		Name:           "suite-a",
		ClientVersions: map[string]string{"go-ethereum_main": "Geth/v1.0/linux"},
		RunMetadata: &RunMetadata{
			HiveCommand: []string{"hive", "--sim", "suite-a"},
			HiveVersion: &HiveVersion{Commit: "abcdef123456", Branch: "main"},
			ClientConfig: &ClientConfig{Content: &ClientConfigContent{Clients: []ClientConfigEntry{
				{Client: "go-ethereum", BuildArgs: map[string]string{"tag": "main"}},
			}}},
		},
	}
	match := TestMatch{TestID: "7", Test: TestCase{
		Name:        "failing test",
		Description: "line<br><b>two</b>",
		SummaryResult: SummaryResult{
			Pass:    false,
			Timeout: true,
		},
		ClientInfo: map[string]ClientInfo{
			"b": {ID: "2", Name: "second"},
			"a": {ID: "1", Name: "first"},
		},
	}}

	meta := buildMetadata("https://hive.example/", "group name", run, suite, match)
	if meta.Client != "go-ethereum" || meta.TestDescription != "line\ntwo" || !meta.Timeout {
		t.Fatalf("metadata = %+v", meta)
	}
	if len(meta.ClientInfo) != 2 || meta.ClientInfo[0].ID != "1" || meta.ClientInfo[1].ID != "2" {
		t.Fatalf("client info order = %+v", meta.ClientInfo)
	}
	if !strings.Contains(meta.WebsiteURL, "group%20name") || !strings.Contains(meta.WebsiteURL, "run%20file") {
		t.Fatalf("website URL = %q", meta.WebsiteURL)
	}
	if len(meta.BuildInfo) != 1 || meta.HiveVersion.Branch != "main" {
		t.Fatalf("metadata build fields = %+v", meta)
	}
}
