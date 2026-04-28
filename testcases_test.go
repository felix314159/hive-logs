package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatchingTestsFiltersByStatusAndText(t *testing.T) {
	suite := &SuiteResult{TestCases: map[string]TestCase{
		"1": {Name: "Engine API fails", SummaryResult: SummaryResult{Pass: false}},
		"2": {Name: "Engine API passes", SummaryResult: SummaryResult{Pass: true}},
		"3": {Name: "Other failure", SummaryResult: SummaryResult{Pass: false}},
	}}

	matches, err := matchingTests(suite, "engine", false, "fail")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].TestID != "1" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestMatchingTestsSupportsRegexAndAllStatuses(t *testing.T) {
	suite := &SuiteResult{TestCases: map[string]TestCase{
		"1": {Name: "alpha-1", SummaryResult: SummaryResult{Pass: false}},
		"2": {Name: "alpha-2", SummaryResult: SummaryResult{Pass: true}},
	}}
	matches, err := matchingTests(suite, `alpha-\d`, true, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestMatchingTestsReportsInvalidInputs(t *testing.T) {
	suite := &SuiteResult{TestCases: map[string]TestCase{"1": {Name: "x"}}}
	if _, err := matchingTests(suite, "[", true, "fail"); err == nil || !strings.Contains(err.Error(), "invalid --test regex") {
		t.Fatalf("regex err = %v", err)
	}
	if _, err := matchingTests(suite, "", false, "unknown"); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("status err = %v", err)
	}
}

func TestSortMatchesOrdersNumericIDsBeforeLexicographicFallback(t *testing.T) {
	matches := []TestMatch{{TestID: "10"}, {TestID: "2"}, {TestID: "alpha"}, {TestID: "1"}}
	sortMatches(matches)
	got := []string{matches[0].TestID, matches[1].TestID, matches[2].TestID, matches[3].TestID}
	want := []string{"1", "2", "10", "alpha"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
