// Package main filters, deduplicates, and orders Hive listing runs for command selection and display.
package main

import (
	"sort"
	"strings"
)

func filterRuns(runs []ListingRun, suite, clientName, latestMode string) []ListingRun {
	var out []ListingRun
	for _, run := range runs {
		if suite != "" && run.Name != suite && simulatorName(run.Name) != suite {
			continue
		}
		if clientName != "" && !runHasClient(run, clientName) {
			continue
		}
		out = append(out, run)
	}
	if latestMode != "latest" {
		return out
	}
	sortRunsNewestFirst(out)
	return latestBySuiteClient(out)
}

func latestBySuiteClient(runs []ListingRun) []ListingRun {
	seen := make(map[string]bool)
	var out []ListingRun
	for _, run := range runs {
		add := false
		for _, client := range normalizedClients(run.Clients) {
			key := run.Name + "/" + client
			if !seen[key] {
				add = true
				seen[key] = true
			}
		}
		if add {
			out = append(out, run)
		}
	}
	return out
}

func sortRunsNewestFirst(runs []ListingRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		return runs[i].Start.After(runs[j].Start)
	})
}

func sortRunsForDisplay(runs []ListingRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].Name != runs[j].Name {
			return runs[i].Name < runs[j].Name
		}
		leftClients := clientsDisplayKey(runs[i].Clients)
		rightClients := clientsDisplayKey(runs[j].Clients)
		if leftClients != rightClients {
			return leftClients < rightClients
		}
		if !runs[i].Start.Equal(runs[j].Start) {
			return runs[i].Start.After(runs[j].Start)
		}
		return runs[i].FileName < runs[j].FileName
	})
}

func clientsDisplayKey(clients []string) string {
	return strings.Join(normalizedSortedClients(clients), ",")
}

func runHasClient(run ListingRun, want string) bool {
	want = normalizeClient(want)
	for _, got := range run.Clients {
		if normalizeClient(got) == want || got == want {
			return true
		}
	}
	return false
}
