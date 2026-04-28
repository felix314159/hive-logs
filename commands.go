// Package main orchestrates hive-logs commands by combining flag values, Hive results, filtering, and output.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

func cmdGroups(args []string) error {
	gf, rest, err := parseGroupsArgs(args)
	if err != nil {
		return err
	}
	if len(rest) > 3 {
		return fmt.Errorf("too many positional arguments for groups: %s", strings.Join(rest[3:], " "))
	}

	ctx := context.Background()
	client := newClient(gf.baseURL)

	if len(rest) > 0 {
		if len(rest) == 3 {
			return fetchSuiteClientFailures(ctx, client, rest[0], rest[1], rest[2], gf.json)
		}
		if len(rest) == 2 {
			return listSuiteClients(ctx, client, rest[0], rest[1], gf.json)
		}
		return listGroupRuns(ctx, client, rest[0], gf)
	}

	groups, err := fetchGroups(ctx, client)
	if err != nil {
		return err
	}

	base := strings.TrimRight(gf.baseURL, "/")
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

	if gf.json {
		return writePrettyJSON(os.Stdout, summaries)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "GROUP\tURL\tLATEST")
	for _, s := range summaries {
		latest := ""
		if !s.Latest.IsZero() {
			latest = formatTime(s.Latest)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.URL, latest)
	}
	return w.Flush()
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

func cmdSuites(args []string) error {
	var baseURL string
	var jsonOut bool
	fs := flag.NewFlagSet("suites", flag.ExitOnError)
	fs.StringVar(&baseURL, "base-url", defaultBaseURL, "Hive results origin")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments for suites: %s", strings.Join(fs.Args(), " "))
	}

	ctx := context.Background()
	client := newClient(baseURL)
	groups, err := fetchGroups(ctx, client)
	if err != nil {
		return err
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

	if jsonOut {
		return writePrettyJSON(os.Stdout, entries)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SUITE\tGROUP\tLATEST")
	for _, e := range entries {
		latest := ""
		if !e.Latest.IsZero() {
			latest = formatTime(e.Latest)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.Suite, e.Group, latest)
	}
	return w.Flush()
}

func cmdClients(args []string) error {
	var jsonOut bool
	fs := flag.NewFlagSet("clients", flag.ExitOnError)
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments for clients: %s", strings.Join(fs.Args(), " "))
	}

	if jsonOut {
		return writePrettyJSON(os.Stdout, hiveKnownClients)
	}
	for _, c := range hiveKnownClients {
		fmt.Println(c)
	}
	return nil
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
		return fmt.Errorf("no runs found for group=%s suite=%s", group, suite)
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
	matches := filterRuns(runs, suite, clientName, "latest")
	if len(matches) == 0 {
		return fmt.Errorf("no run found for group=%s suite=%s client=%s", group, suite, clientName)
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
			baseURL: client.baseURL,
			group:   group,
			suite:   suite,
			client:  clientName,
		},
		outDir: "logs",
		limit:  0,
		status: "fail",
	}

	bundles := make([]BundleSummary, 0, len(tests))
	for _, match := range tests {
		bundle, err := fetchBundle(ctx, client, ff, run, suiteResult, match)
		if err != nil {
			return err
		}
		bundles = append(bundles, bundle)
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

func listGroupRuns(ctx context.Context, client *Client, group string, gf groupsFlags) error {
	runs, err := fetchListing(ctx, client, group)
	if err != nil {
		return err
	}
	latestMode := "latest"
	if gf.all {
		latestMode = ""
	}
	runs = filterRuns(runs, "", gf.client, latestMode)
	sortRunsForDisplay(runs)
	if gf.limit > 0 && len(runs) > gf.limit {
		runs = runs[:gf.limit]
	}
	if gf.json {
		return writePrettyJSON(os.Stdout, runs)
	}

	headers := []string{"START", "SUITE", "CLIENTS", "PASS", "FAIL"}
	right := []bool{false, false, false, true, true}
	if gf.showFiles {
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
		if gf.showFiles {
			row = append(row, textCell(run.FileName))
		}
		w.addRow(row...)
	}
	w.flush()
	return nil
}

func cmdList(args []string) error {
	var cf commonFlags
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	addCommonFlags(fs, &cf)
	limit := fs.Int("limit", 100, "maximum rows to print")
	status := fs.String("status", "fail", "fail, pass, or all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cf.suite == "" {
		return errors.New("--suite is required")
	}
	if cf.client == "" {
		return errors.New("--client is required")
	}

	ctx := context.Background()
	client := newClient(cf.baseURL)
	run, err := selectRun(ctx, client, cf)
	if err != nil {
		return err
	}
	suite, err := fetchSuite(ctx, client, cf.group, run.FileName)
	if err != nil {
		return err
	}
	matches, err := matchingTests(suite, cf.test, cf.regex, *status)
	if err != nil {
		return err
	}
	sortMatches(matches)
	if *limit > 0 && len(matches) > *limit {
		matches = matches[:*limit]
	}

	rows := make([]ListRow, 0, len(matches))
	for _, match := range matches {
		rows = append(rows, listRow(cf.group, run, suite, match))
	}
	if cf.json {
		return writePrettyJSON(os.Stdout, rows)
	}

	fmt.Printf("%s / %s / %s latest run: %s (%s)\n\n", cf.group, cf.suite, cf.client, formatTime(run.Start), run.FileName)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tTEST\tHIVE_LOG\tCLIENT_LOGS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", row.TestID, row.Status, row.Name, yesNo(row.HasHiveLog), row.ClientLogCount)
	}
	return w.Flush()
}

func cmdFetch(args []string) error {
	var ff fetchFlags
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	addCommonFlags(fs, &ff.common)
	fs.StringVar(&ff.outDir, "out", "logs", "output directory")
	fs.IntVar(&ff.limit, "limit", 1, "maximum matching tests to fetch, 0 for all")
	fs.BoolVar(&ff.fullClient, "full-client-log", false, "fetch full client log files")
	fs.BoolVar(&ff.includePass, "include-pass", false, "allow matching passing tests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ff.common.suite == "" {
		return errors.New("--suite is required")
	}
	if ff.common.client == "" {
		return errors.New("--client is required")
	}
	if ff.common.test == "" {
		return errors.New("--test is required")
	}
	ff.status = "fail"
	if ff.includePass {
		ff.status = "all"
	}

	ctx := context.Background()
	client := newClient(ff.common.baseURL)
	run, err := selectRun(ctx, client, ff.common)
	if err != nil {
		return err
	}
	suite, err := fetchSuite(ctx, client, ff.common.group, run.FileName)
	if err != nil {
		return err
	}
	matches, err := matchingTests(suite, ff.common.test, ff.common.regex, ff.status)
	if err != nil {
		return err
	}
	sortMatches(matches)
	if len(matches) == 0 {
		return errors.New("no matching tests")
	}
	if ff.limit > 0 && len(matches) > ff.limit {
		matches = matches[:ff.limit]
	}

	var bundles []BundleSummary
	for _, match := range matches {
		bundle, err := fetchBundle(ctx, client, ff, run, suite, match)
		if err != nil {
			return err
		}
		bundles = append(bundles, bundle)
	}

	if ff.common.json {
		return writePrettyJSON(os.Stdout, bundles)
	}
	if len(bundles) > 0 {
		fmt.Printf("url: %s\n\n", bundles[0].WebsiteURL)
	}
	for _, b := range bundles {
		fmt.Printf("wrote %s\n", b.Directory)
		fmt.Printf("  hive log:              %s\n", b.HiveLogPath)
		fmt.Printf("  client log:            %s\n", b.ClientLogPath)
		fmt.Printf("  reproduce commands:    %s\n", b.ReproduceCommandsPath)
	}
	return nil
}

type ListRow struct {
	Group          string    `json:"group"`
	Suite          string    `json:"suite"`
	Client         string    `json:"client"`
	RunStart       time.Time `json:"run_start"`
	RunFile        string    `json:"run_file"`
	TestID         string    `json:"test_id"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	HasHiveLog     bool      `json:"has_hive_log"`
	ClientLogCount int       `json:"client_log_count"`
}

func listRow(group string, run ListingRun, suite *SuiteResult, match TestMatch) ListRow {
	status := "FAIL"
	if match.Test.SummaryResult.Pass {
		status = "PASS"
	}
	return ListRow{
		Group:          group,
		Suite:          suite.Name,
		Client:         strings.Join(normalizedClients(run.Clients), ","),
		RunStart:       run.Start,
		RunFile:        run.FileName,
		TestID:         match.TestID,
		Name:           match.Test.Name,
		Status:         status,
		HasHiveLog:     suite.TestDetailsLog != "" && match.Test.SummaryResult.Log != nil,
		ClientLogCount: len(match.Test.ClientInfo),
	}
}
