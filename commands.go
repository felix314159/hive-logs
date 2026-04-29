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
	if qf.common.group == "" && qf.common.suite == "" {
		if qf.common.client != "" {
			return errors.New("group= is required when client= is set without suite=")
		}
		return errors.New("group= is required")
	}
	ctx := context.Background()
	client := newClient(qf.baseURL)

	if qf.common.group == "" {
		groups, err := fetchGroups(ctx, client)
		if err != nil {
			return err
		}
		matches, err := findGroupsContainingSuite(ctx, client, qf.common.suite, groups)
		if err != nil {
			return err
		}
		switch len(matches) {
		case 0:
			return fmt.Errorf("suite %q not found in any group", qf.common.suite)
		case 1:
			qf.common.group = matches[0]
		default:
			names := make([]string, len(groups))
			for i, g := range groups {
				names[i] = g.Name
			}
			sort.Strings(names)
			return fmt.Errorf("group= is required because suite %q exists in multiple groups. available groups: %s",
				qf.common.suite, strings.Join(names, ", "))
		}
	} else if err := ensureGroupExists(ctx, client, qf.common.group); err != nil {
		return err
	}

	if qf.common.client != "" && qf.common.suite == "" {
		runs, err := fetchListing(ctx, client, qf.common.group)
		if err != nil {
			return err
		}
		suites := availableSuites(runs)
		switch len(suites) {
		case 0:
			return fmt.Errorf("client= requires suite= to be set; group %q has no suites", qf.common.group)
		case 1:
			qf.common.suite = suites[0]
		default:
			return fmt.Errorf("client= requires suite= to be set when group %q has multiple suites; available suites: %s",
				qf.common.group, strings.Join(suites, ", "))
		}
	}

	if qf.common.client != "" {
		return fetchSuiteClientFailures(ctx, client, qf.common.group, qf.common.suite, qf.common.client, qf.json, qf.showLogPaths)
	}
	if qf.common.suite != "" {
		return listSuiteClients(ctx, client, qf.common.group, qf.common.suite, qf.json, qf.withDuration)
	}
	return listGroupRuns(ctx, client, qf.common.group, qf)
}

// findGroupsContainingSuite fetches each group's listing in parallel and
// returns the sorted names of groups whose listing contains a run with the
// given suite name. Used to infer group= when the user omitted it but the
// suite name is unique across groups.
func findGroupsContainingSuite(ctx context.Context, client *Client, suite string, groups []Group) ([]string, error) {
	fetchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		matches  []string
		errOnce  sync.Once
		firstErr error
	)
	for _, g := range groups {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			runs, err := fetchListing(fetchCtx, client, name)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			for _, s := range availableSuites(runs) {
				if s == suite {
					mu.Lock()
					matches = append(matches, name)
					mu.Unlock()
					return
				}
			}
		}(g.Name)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Strings(matches)
	return matches, nil
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

func listSuiteClients(ctx context.Context, client *Client, group, suite string, jsonOut, withDuration bool) error {
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
		return fmt.Errorf("suite %q does not exist in group %q; available suites for group %q: %s", suite, group, group, strings.Join(availableSuites(runs), ", "))
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

	fixtures, hiveVersion := hydrateSuiteSummaries(ctx, client, group, out, !jsonOut, withDuration)

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
	if line := formatRunHeader(newest.RunFile, newest.RunStart, hiveTestURL(client.baseURL, group, newest.RunFile)); line != "" {
		fmt.Println(line)
	}
	if line := formatHiveVersion(hiveVersion); line != "" {
		fmt.Println(line)
	}
	if line := formatFixtures(fixtures); line != "" {
		fmt.Println(line)
	}
	fmt.Println()

	headers := []string{"CLIENT", "PASS", "FAIL", "START"}
	right := []bool{false, true, true, false}
	if withDuration {
		headers = append(headers, "DURATION")
		right = append(right, false)
	}
	headers = append(headers, "BRANCH", "COMMIT", "VERSION")
	right = append(right, false, false, false)
	w := newTextTable(os.Stdout, headers, right)
	for _, e := range out {
		row := []tableCell{
			textCell(e.Client),
			passCell(e.Passes, e.Fails),
			failCell(e.Fails),
			textCell(formatTime(e.RunStart)),
		}
		if withDuration {
			row = append(row, textCell(formatHMS(e.Duration)))
		}
		row = append(row,
			textCell(e.Branch),
			textCell(e.Commit),
			textCell(truncate(e.Version, 80)),
		)
		w.addRow(row...)
	}
	w.flush()
	return nil
}

func fetchSuiteClientFailures(ctx context.Context, client *Client, group, suite, clientName string, jsonOut, showLogPaths bool) error {
	if !isKnownClientName(clientName) {
		return fmt.Errorf("client %q does not exist, the following clients exist: %s", clientName, strings.Join(hiveKnownClients, ", "))
	}
	runs, err := fetchListing(ctx, client, group)
	if err != nil {
		return err
	}
	suiteRuns := filterRuns(runs, suite, "", "latest")
	if len(suiteRuns) == 0 {
		return fmt.Errorf("suite %q does not exist in group %q; available suites for group %q: %s", suite, group, group, strings.Join(availableSuites(runs), ", "))
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

	divider := strings.Repeat("─", 80)
	fmt.Println(divider)
	printBundlesGroupedByFile(os.Stdout, bundles, showLogPaths)
	fmt.Printf("\n%s\n", divider)
	fmt.Printf("%s / %s / %s\n", group, suite, clientName)
	if line := formatRunHeader(run.FileName, run.Start, bundles[0].WebsiteURL); line != "" {
		fmt.Println(line)
	}
	if suiteResult.RunMetadata != nil {
		if line := formatHiveVersion(suiteResult.RunMetadata.HiveVersion); line != "" {
			fmt.Println(line)
		}
	}
	if line := formatFixtures(suiteFixtures(suiteResult)); line != "" {
		fmt.Println(line)
	}
	clientVersion, clientCommit := suiteClientVersionInfo(suiteResult, clientName)
	if line := formatClientInfo(clientName, suiteClientBranch(suiteResult), clientCommit, clientVersion); line != "" {
		fmt.Println(line)
	}
	fmt.Println()
	fileCount := countTestFiles(bundles)
	if len(bundles) > 0 {
		fmt.Printf("%sAll logs of failed tests have been stored in ./%s%s\n", ansiGreen, ff.outDir, ansiReset)
	}
	fmt.Printf("%s%s%s\n",
		ansiRed, formatVectorFileCount(len(bundles), fileCount), ansiReset)
	return nil
}

// formatVectorFileCount renders the red summary line stating how many failing
// test vectors live in how many distinct test files.
func formatVectorFileCount(vectorCount, fileCount int) string {
	vectorWord := "test vectors"
	if vectorCount == 1 {
		vectorWord = "test vector"
	}
	if fileCount == 0 {
		return fmt.Sprintf("%d failing %s", vectorCount, vectorWord)
	}
	fileWord := "test files"
	if fileCount == 1 {
		fileWord = "test file"
	}
	return fmt.Sprintf("%d failing %s in %d %s", vectorCount, vectorWord, fileCount, fileWord)
}

// countTestFiles returns the number of distinct, non-empty test file paths
// across the given bundles.
func countTestFiles(bundles []BundleSummary) int {
	seen := make(map[string]struct{})
	for _, b := range bundles {
		if b.TestFile == "" {
			continue
		}
		seen[b.TestFile] = struct{}{}
	}
	return len(seen)
}

// printBundlesGroupedByFile prints bundles grouped by their test file. Within
// a group, bullets are indented under the file header. Bundles with no test
// file (no `::` in the test name) are printed last with no header. When
// showLogPaths is false, only the file header and test-vector bullets are
// printed (the per-bundle hive/client/reproduce paths are suppressed).
func printBundlesGroupedByFile(w io.Writer, bundles []BundleSummary, showLogPaths bool) {
	type group struct {
		file    string
		bundles []BundleSummary
	}
	order := make([]string, 0)
	groups := make(map[string]*group)
	for _, b := range bundles {
		g, ok := groups[b.TestFile]
		if !ok {
			g = &group{file: b.TestFile}
			groups[b.TestFile] = g
			order = append(order, b.TestFile)
		}
		g.bundles = append(g.bundles, b)
	}

	groupDivider := strings.Repeat("─", 80)
	first := true
	for _, key := range order {
		g := groups[key]
		if !first {
			fmt.Fprintln(w)
			fmt.Fprintf(w, "%s%s%s\n", ansiGrey, groupDivider, ansiReset)
		}
		first = false
		bulletIndent := ""
		if g.file != "" {
			fmt.Fprintf(w, "%s%s%s\n", ansiRed, g.file, ansiReset)
			bulletIndent = "  "
		}
		for i, b := range g.bundles {
			if i > 0 && showLogPaths {
				fmt.Fprintln(w)
			}
			label := b.TestVector
			if label == "" && b.TestFile == "" {
				label = b.TestName
			}
			hasBullet := label != ""
			if hasBullet {
				fmt.Fprintf(w, "%s• %s%s%s\n", bulletIndent, ansiCoral, label, ansiReset)
			}
			if showLogPaths {
				pathIndent := bulletIndent
				if hasBullet {
					pathIndent += "    "
				}
				fmt.Fprintf(w, "%shive log:    %s\n", pathIndent, b.HiveLogPath)
				fmt.Fprintf(w, "%sclient log:  %s\n", pathIndent, b.ClientLogPath)
				fmt.Fprintf(w, "%sreproduce:   %s\n", pathIndent, b.ReproduceCommandsPath)
			}
		}
	}
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

// fetchSuiteConcurrency caps in-flight suite-JSON fetches. Each fetch is
// already streamed (network and JSON parse overlap), so this is mostly a
// guard against opening too many connections at once.
const fetchSuiteConcurrency = 8

// hydrateSuiteSummaries fills per-client duration, version, and branch by
// fetching each unique RunFile exactly once. Duration/Version/Branch are
// derived from the suite JSON; Fixtures and HiveVersion are global so we
// return the first non-empty value seen.
//
// When withDuration is false, only the suite header (everything before the
// huge testCases field) is fetched — typically a >20x speedup, but the
// returned summaries have Duration=0. When withDuration is true the full
// suite JSON is downloaded and Duration is populated.
//
// When showProgress is true, a `\r[i/N] P%` indicator is written to stderr as
// fetches complete, mirroring fetchBundlesParallel's UX.
func hydrateSuiteSummaries(ctx context.Context, client *Client, group string, out []SuiteClientSummary, showProgress, withDuration bool) (fixturesInfo, *HiveVersion) {
	runFiles := make(map[string][]int)
	var order []string
	for i, e := range out {
		if _, ok := runFiles[e.RunFile]; !ok {
			order = append(order, e.RunFile)
		}
		runFiles[e.RunFile] = append(runFiles[e.RunFile], i)
	}
	if len(order) == 0 {
		return fixturesInfo{}, nil
	}

	if showProgress {
		fileWord := "files"
		if len(order) == 1 {
			fileWord = "file"
		}
		fmt.Fprintf(os.Stderr, "fetching suite metadata for %d run %s...\n", len(order), fileWord)
	}

	var (
		wg          sync.WaitGroup
		sem         = make(chan struct{}, fetchSuiteConcurrency)
		metaMu      sync.Mutex
		fixtures    fixturesInfo
		hiveVersion *HiveVersion
		progressMu  sync.Mutex
		done        int
	)
	for _, runFile := range order {
		sem <- struct{}{}
		wg.Add(1)
		go func(runFile string) {
			defer wg.Done()
			defer func() { <-sem }()
			var (
				suiteData *SuiteResult
				err       error
			)
			if withDuration {
				suiteData, err = fetchSuite(ctx, client, group, runFile)
			} else {
				suiteData, err = fetchSuiteHeader(ctx, client, group, runFile)
			}
			if err == nil {
				branch := suiteClientBranch(suiteData)
				var duration time.Duration
				if withDuration {
					duration = suiteDuration(suiteData)
				}
				for _, idx := range runFiles[runFile] {
					out[idx].Duration = duration
					out[idx].Branch = branch
					out[idx].Version, out[idx].Commit = suiteClientVersionInfo(suiteData, out[idx].Client)
				}
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
			}
			if showProgress {
				progressMu.Lock()
				done++
				fmt.Fprintf(os.Stderr, "\r[%d/%d] %d%%", done, len(order), done*100/len(order))
				progressMu.Unlock()
			}
		}(runFile)
	}
	wg.Wait()
	if showProgress {
		fmt.Fprintln(os.Stderr)
	}
	return fixtures, hiveVersion
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
			passCell(run.Passes, run.Fails),
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
