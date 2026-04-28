// Package main orchestrates hive-logs commands by combining flag values, Hive results, filtering, and output.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

func cmdQuery(args []string) error {
	qf, err := parseQueryArgs(args)
	if err != nil {
		return err
	}
	if qf.common.group == "" {
		if qf.common.suite != "" || qf.common.client != "" {
			return errors.New("group= is required when suite= or client= is set")
		}
		return errors.New("group= is required")
	}
	if qf.common.client != "" && qf.common.suite == "" {
		return errors.New("client= requires suite= to be set")
	}

	ctx := context.Background()
	client := newClient(qf.baseURL)

	if err := ensureGroupExists(ctx, client, qf.common.group); err != nil {
		return err
	}

	if qf.common.client != "" {
		return fetchSuiteClientFailures(ctx, client, qf.common.group, qf.common.suite, qf.common.client, qf.json)
	}
	if qf.common.suite != "" {
		return listSuiteClients(ctx, client, qf.common.group, qf.common.suite, qf.json)
	}
	return listGroupRuns(ctx, client, qf.common.group, qf)
}

// availableSuites returns the unique sorted suite names present in runs.
func availableSuites(runs []ListingRun) []string {
	seen := make(map[string]bool)
	for _, r := range runs {
		if r.Name != "" {
			seen[r.Name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// availableClients returns the unique sorted normalized client names that
// participated in any of the given runs.
func availableClients(runs []ListingRun) []string {
	seen := make(map[string]bool)
	for _, r := range runs {
		for _, c := range normalizedClients(r.Clients) {
			seen[c] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ensureGroupExists fails with a list of available groups when group is not in
// the Hive discovery file.
func ensureGroupExists(ctx context.Context, client *Client, group string) error {
	groups, err := fetchGroups(ctx, client)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.Name == group {
			return nil
		}
		names = append(names, g.Name)
	}
	sort.Strings(names)
	return fmt.Errorf("group %q does not exist; available groups: %s", group, strings.Join(names, ", "))
}

func writeGroupSummaries(w io.Writer, summaries []GroupSummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "GROUP\tURL\tLATEST")
	for _, s := range summaries {
		latest := ""
		if !s.Latest.IsZero() {
			latest = formatTime(s.Latest)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.URL, latest)
	}
	tw.Flush()
}

type GroupSummary struct {
	Name   string    `json:"name"`
	URL    string    `json:"url"`
	Latest time.Time `json:"latest"`
}

type SuiteSummary struct {
	Suite  string    `json:"suite"`
	Group  string    `json:"group"`
	Latest time.Time `json:"latest"`
}

func writeSuiteSummaries(w io.Writer, entries []SuiteSummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SUITE\tGROUP\tLATEST")
	for _, e := range entries {
		latest := ""
		if !e.Latest.IsZero() {
			latest = formatTime(e.Latest)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Suite, e.Group, latest)
	}
	tw.Flush()
}

type ListSummary struct {
	Groups  []GroupSummary `json:"groups"`
	Suites  []SuiteSummary `json:"suites"`
	Clients []string       `json:"clients"`
}

func cmdList(args []string) error {
	var baseURL string
	var jsonOut bool
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	fs.StringVar(&baseURL, "base-url", defaultBaseURL, "Hive results origin")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments for list: %s", strings.Join(fs.Args(), " "))
	}

	ctx := context.Background()
	client := newClient(baseURL)
	groups, err := fetchGroupSummaries(ctx, client, baseURL)
	if err != nil {
		return err
	}
	suites, err := fetchSuiteSummaries(ctx, client)
	if err != nil {
		return err
	}
	summary := ListSummary{
		Groups:  groups,
		Suites:  suites,
		Clients: hiveKnownClients,
	}

	if jsonOut {
		return writePrettyJSON(os.Stdout, summary)
	}

	fmt.Println("GROUPS")
	writeGroupSummaries(os.Stdout, summary.Groups)
	fmt.Println()
	fmt.Println("SUITES")
	writeSuiteSummaries(os.Stdout, summary.Suites)
	fmt.Println()
	fmt.Println("CLIENTS")
	for _, c := range summary.Clients {
		fmt.Println(c)
	}
	return nil
}

func fetchGroupSummaries(ctx context.Context, client *Client, baseURL string) ([]GroupSummary, error) {
	groups, err := fetchGroups(ctx, client)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(baseURL, "/")
	summaries := make([]GroupSummary, len(groups))
	var wg sync.WaitGroup
	for i, g := range groups {
		summaries[i] = GroupSummary{
			Name: g.Name,
			URL:  fmt.Sprintf(hiveGroupURLFormat, base, url.PathEscape(g.Name)),
		}
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			runs, err := fetchListing(ctx, client, name)
			if err != nil {
				return
			}
			for _, r := range runs {
				if r.Start.After(summaries[i].Latest) {
					summaries[i].Latest = r.Start
				}
			}
		}(i, g.Name)
	}
	wg.Wait()
	return summaries, nil
}

func fetchSuiteSummaries(ctx context.Context, client *Client) ([]SuiteSummary, error) {
	groups, err := fetchGroups(ctx, client)
	if err != nil {
		return nil, err
	}

	var (
		mu      sync.Mutex
		entries []SuiteSummary
		wg      sync.WaitGroup
	)
	for _, g := range groups {
		wg.Add(1)
		go func(group string) {
			defer wg.Done()
			runs, err := fetchListing(ctx, client, group)
			if err != nil {
				return
			}
			latest := make(map[string]time.Time)
			for _, r := range runs {
				if r.Start.After(latest[r.Name]) {
					latest[r.Name] = r.Start
				}
			}
			mu.Lock()
			for suite, ts := range latest {
				entries = append(entries, SuiteSummary{Suite: suite, Group: group, Latest: ts})
			}
			mu.Unlock()
		}(g.Name)
	}
	wg.Wait()

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Group != entries[j].Group {
			return entries[i].Group < entries[j].Group
		}
		return entries[i].Suite < entries[j].Suite
	})
	return entries, nil
}

type SuiteClientSummary struct {
	Client   string        `json:"client"`
	Passes   int           `json:"passes"`
	Fails    int           `json:"fails"`
	NTests   int           `json:"ntests"`
	Timeout  bool          `json:"timeout"`
	RunStart time.Time     `json:"run_start"`
	RunFile  string        `json:"run_file"`
	Duration time.Duration `json:"duration_ns"`
	Branch   string        `json:"branch,omitempty"`
	Commit   string        `json:"commit,omitempty"`
	Version  string        `json:"version,omitempty"`
}

type fixturesInfo struct {
	Release string `json:"release,omitempty"`
	Branch  string `json:"branch,omitempty"`
	URL     string `json:"url,omitempty"`
}

func listSuiteClients(ctx context.Context, client *Client, group, suite string, jsonOut bool) error {
	runs, err := fetchListing(ctx, client, group)
	if err != nil {
		return err
	}
	matches := filterRuns(runs, suite, "", "latest")
	if len(matches) == 0 {
		if isKnownClientName(suite) {
			return fmt.Errorf(
				"expected a suite name for group %q, but %q is a client name; specify it as `group=%s suite=SUITE client=%s`; use `group=%s` to list suites and clients in this group",
				group,
				suite,
				group,
				suite,
				group,
			)
		}
		return fmt.Errorf("suite %q does not exist; available suites for group %q: %s", suite, group, strings.Join(availableSuites(runs), ", "))
	}

	seen := make(map[string]bool)
	out := make([]SuiteClientSummary, 0)
	for _, run := range matches {
		for _, c := range normalizedClients(run.Clients) {
			if seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, SuiteClientSummary{
				Client:   c,
				Passes:   run.Passes,
				Fails:    run.Fails,
				NTests:   run.NTests,
				Timeout:  run.Timeout,
				RunStart: run.Start,
				RunFile:  run.FileName,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Client < out[j].Client })

	var (
		wg          sync.WaitGroup
		metaMu      sync.Mutex
		fixtures    fixturesInfo
		hiveVersion *HiveVersion
	)
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			suiteData, err := fetchSuite(ctx, client, group, out[i].RunFile)
			if err != nil {
				return
			}
			out[i].Duration = suiteDuration(suiteData)
			out[i].Version, out[i].Commit = suiteClientVersionInfo(suiteData, out[i].Client)
			out[i].Branch = suiteClientBranch(suiteData)

			fx := suiteFixtures(suiteData)
			var hv *HiveVersion
			if suiteData.RunMetadata != nil {
				hv = suiteData.RunMetadata.HiveVersion
			}
			metaMu.Lock()
			if fixtures.Release == "" && fixtures.Branch == "" && (fx.Release != "" || fx.Branch != "") {
				fixtures = fx
			}
			if hiveVersion == nil && hv != nil {
				hiveVersion = hv
			}
			metaMu.Unlock()
		}(i)
	}
	wg.Wait()

	if jsonOut {
		return writePrettyJSON(os.Stdout, struct {
			Hive     *HiveVersion         `json:"hive,omitempty"`
			Fixtures fixturesInfo         `json:"fixtures,omitempty"`
			Clients  []SuiteClientSummary `json:"clients"`
		}{Hive: hiveVersion, Fixtures: fixtures, Clients: out})
	}

	newest := out[0]
	for _, e := range out[1:] {
		if e.RunStart.After(newest.RunStart) {
			newest = e
		}
	}
	fmt.Printf("%s / %s\n", group, suite)
	fmt.Printf("%s\nrun=%s\n", formatTime(newest.RunStart), strings.TrimSuffix(newest.RunFile, ".json"))
	if line := formatHiveVersion(hiveVersion); line != "" {
		fmt.Println(line)
	}
	if line := formatFixtures(fixtures); line != "" {
		fmt.Println(line)
	}
	fmt.Println()

	w := newTextTable(os.Stdout, []string{"CLIENT", "PASS", "FAIL", "START", "DURATION", "BRANCH", "COMMIT", "VERSION"}, []bool{false, true, true, false, false, false, false, false})
	for _, e := range out {
		w.addRow(
			textCell(e.Client),
			passCell(e.Passes),
			failCell(e.Fails),
			textCell(formatTime(e.RunStart)),
			textCell(formatHMS(e.Duration)),
			textCell(e.Branch),
			textCell(e.Commit),
			textCell(truncate(e.Version, 80)),
		)
	}
	w.flush()
	return nil
}

func fetchSuiteClientFailures(ctx context.Context, client *Client, group, suite, clientName string, jsonOut bool) error {
	runs, err := fetchListing(ctx, client, group)
	if err != nil {
		return err
	}
	suiteRuns := filterRuns(runs, suite, "", "latest")
	if len(suiteRuns) == 0 {
		return fmt.Errorf("suite %q does not exist; available suites for group %q: %s", suite, group, strings.Join(availableSuites(runs), ", "))
	}
	matches := filterRuns(runs, suite, clientName, "latest")
	if len(matches) == 0 {
		return fmt.Errorf("client %q did not run suite %q in group %q; available clients: %s", clientName, suite, group, strings.Join(availableClients(suiteRuns), ", "))
	}
	sortRunsNewestFirst(matches)
	run := matches[0]

	suiteResult, err := fetchSuite(ctx, client, group, run.FileName)
	if err != nil {
		return err
	}
	tests, err := matchingTests(suiteResult, "", false, "fail")
	if err != nil {
		return err
	}
	sortMatches(tests)

	if len(tests) == 0 {
		if jsonOut {
			return writePrettyJSON(os.Stdout, []BundleSummary{})
		}
		fmt.Printf("%s / %s / %s (%s)\n%sno failing tests in latest run%s\n",
			group, suite, clientName, formatTime(run.Start), ansiGreen, ansiReset)
		return nil
	}

	ff := fetchFlags{
		common: commonFlags{
			group:  group,
			suite:  suite,
			client: clientName,
		},
		outDir: "logs",
	}

	testWord := "tests"
	if len(tests) == 1 {
		testWord = "test"
	}
	fmt.Fprintf(os.Stderr, "fetching logs for %d failing %s...\n", len(tests), testWord)
	bundles, err := fetchBundlesParallel(ctx, client, ff, run, suiteResult, tests)
	if err != nil {
		return err
	}

	if jsonOut {
		return writePrettyJSON(os.Stdout, bundles)
	}

	fmt.Printf("%s / %s / %s\n", group, suite, clientName)
	fmt.Printf("%s\nrun=%s\n", formatTime(run.Start), strings.TrimSuffix(run.FileName, ".json"))
	if suiteResult.RunMetadata != nil {
		if line := formatHiveVersion(suiteResult.RunMetadata.HiveVersion); line != "" {
			fmt.Println(line)
		}
	}
	clientVersion, clientCommit := suiteClientVersionInfo(suiteResult, clientName)
	if line := formatClientInfo(clientName, suiteClientBranch(suiteResult), clientCommit, clientVersion); line != "" {
		fmt.Println(line)
	}
	word := "tests"
	if len(bundles) == 1 {
		word = "test"
	}
	fmt.Printf("url=%s\n%s%d failing %s%s\n\n",
		bundles[0].WebsiteURL, ansiRed, len(bundles), word, ansiReset)
	for _, b := range bundles {
		fmt.Printf("• %s\n", b.TestName)
		fmt.Printf("    hive log:    %s\n", b.HiveLogPath)
		fmt.Printf("    client log:  %s\n", b.ClientLogPath)
		fmt.Printf("    reproduce:   %s\n", b.ReproduceCommandsPath)
	}
	return nil
}

// fetchBundleConcurrency caps in-flight log downloads. Each bundle fetch
// issues two HTTP range requests, so this is the practical request fan-out.
const fetchBundleConcurrency = 64

// fetchBundlesParallel downloads bundles for tests concurrently while preserving
// the input ordering. It prints `\r[i/N] P%` to stderr as bundles complete and
// aborts on the first error.
func fetchBundlesParallel(ctx context.Context, client *Client, ff fetchFlags, run ListingRun, suiteResult *SuiteResult, tests []TestMatch) ([]BundleSummary, error) {
	bundles := make([]BundleSummary, len(tests))

	fetchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg         sync.WaitGroup
		sem        = make(chan struct{}, fetchBundleConcurrency)
		errOnce    sync.Once
		firstErr   error
		progressMu sync.Mutex
		done       int
	)

	for i, match := range tests {
		select {
		case <-fetchCtx.Done():
			break
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int, match TestMatch) {
			defer wg.Done()
			defer func() { <-sem }()
			bundle, err := fetchBundle(fetchCtx, client, ff, run, suiteResult, match)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			bundles[i] = bundle
			progressMu.Lock()
			done++
			fmt.Fprintf(os.Stderr, "\r[%d/%d] %d%%", done, len(tests), done*100/len(tests))
			progressMu.Unlock()
		}(i, match)
	}
	wg.Wait()
	fmt.Fprintln(os.Stderr)

	if firstErr != nil {
		return nil, firstErr
	}
	return bundles, nil
}

func suiteDuration(suite *SuiteResult) time.Duration {
	var start, end time.Time
	first := true
	for _, tc := range suite.TestCases {
		if tc.Start.IsZero() || tc.End.IsZero() {
			continue
		}
		if first {
			start, end = tc.Start, tc.End
			first = false
			continue
		}
		if tc.Start.Before(start) {
			start = tc.Start
		}
		if tc.End.After(end) {
			end = tc.End
		}
	}
	if first {
		return 0
	}
	return end.Sub(start)
}

func listGroupRuns(ctx context.Context, client *Client, group string, qf queryFlags) error {
	runs, err := fetchListing(ctx, client, group)
	if err != nil {
		return err
	}
	latestMode := "latest"
	if qf.all {
		latestMode = ""
	}
	runs = filterRuns(runs, "", "", latestMode)
	sortRunsForDisplay(runs)
	if qf.limit > 0 && len(runs) > qf.limit {
		runs = runs[:qf.limit]
	}
	if qf.json {
		return writePrettyJSON(os.Stdout, runs)
	}

	headers := []string{"START", "SUITE", "CLIENTS", "PASS", "FAIL"}
	right := []bool{false, false, false, true, true}
	if qf.showFiles {
		headers = append(headers, "FILE")
		right = append(right, false)
	}
	w := newTextTable(os.Stdout, headers, right)
	for _, run := range runs {
		row := []tableCell{
			textCell(formatTime(run.Start)),
			textCell(run.Name),
			textCell(strings.Join(normalizedSortedClients(run.Clients), ",")),
			passCell(run.Passes),
			failCell(run.Fails),
		}
		if qf.showFiles {
			row = append(row, textCell(run.FileName))
		}
		w.addRow(row...)
	}
	w.flush()
	return nil
}
