// Package main matches and sorts test cases inside a Hive suite according to status and name filters.
package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type TestMatch struct {
	TestID string
	Test   TestCase
}

func matchingTests(suite *SuiteResult, pattern string, regex bool, status string) ([]TestMatch, error) {
	var re *regexp.Regexp
	patternLower := strings.ToLower(pattern)
	if pattern != "" && regex {
		var err error
		re, err = regexp.Compile("(?i)" + pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid --test regex: %w", err)
		}
	}
	var matches []TestMatch
	for id, tc := range suite.TestCases {
		switch status {
		case "", "all":
		case "fail":
			if tc.SummaryResult.Pass {
				continue
			}
		case "pass":
			if !tc.SummaryResult.Pass {
				continue
			}
		default:
			return nil, fmt.Errorf("invalid status %q", status)
		}
		if pattern != "" {
			if re != nil {
				if !re.MatchString(tc.Name) {
					continue
				}
			} else if !strings.Contains(strings.ToLower(tc.Name), patternLower) {
				continue
			}
		}
		matches = append(matches, TestMatch{TestID: id, Test: tc})
	}
	return matches, nil
}

func sortMatches(matches []TestMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		ii, ierr := strconv.Atoi(matches[i].TestID)
		jj, jerr := strconv.Atoi(matches[j].TestID)
		if ierr == nil && jerr == nil {
			return ii < jj
		}
		return matches[i].TestID < matches[j].TestID
	})
}

// splitTestName splits a Hive test name into the test file (or category) and
// the test vector portion. Two conventions are recognised:
//
//   - pytest-style names use "::" (e.g. "tests/foo.py::test_bar[x]-client");
//     the prefix is the file and the suffix is the vector.
//   - Hive Go-simulator names use ", " to separate a category from a variant
//     (e.g. "Blob Transaction Ordering, Multiple Accounts (Cancun) (geth)");
//     the prefix is treated as the category/file.
//
// Names matching neither return ("", name), except for the well-known
// "test file loader" meta-test emitted by the consensus simulator: it loads
// the JSON test files and launches the per-vector tests, so it has no inner
// vector and is treated as a file-only entry.
func splitTestName(name string) (file, vector string) {
	if name == "test file loader" {
		return name, ""
	}
	if i := strings.Index(name, "::"); i >= 0 {
		return name[:i], name[i+2:]
	}
	if i := strings.Index(name, ", "); i >= 0 {
		return name[:i], name[i+2:]
	}
	return "", name
}
