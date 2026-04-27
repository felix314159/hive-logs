package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

const (
	timeFormat = "2006-01-02, 15:04:05"
	version    = "1.0"
)

func formatTime(t time.Time) string {
	return t.Local().Format(timeFormat)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(os.Stderr)
		return nil
	}
	if args[0] == "version" || args[0] == "-v" || args[0] == "--version" {
		fmt.Println(version)
		return nil
	}

	cmd := args[0]
	args = args[1:]

	switch cmd {
	case "groups":
		return cmdGroups(args)
	case "suites":
		return cmdSuites(args)
	case "clients":
		return cmdClients(args)
	case "list":
		return cmdList(args)
	case "fetch":
		return cmdFetch(args)
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `hive-logs finds Hive failures and fetches per-test logs.

Usage:
  --version
      Print the current version.
  groups [--json]
      List all result groups with their website URL and the latest run timestamp.
  suites [--json]
      List all suites across result groups with the latest run timestamp.
  clients [--json]
      List known client names (offline; from compiled-in list).
  groups GROUP [--client NAME] [--all] [--files] [--limit N] [--json]
      Print the latest matching Hive runs grouped by suite, then client.
      --all includes older runs; --files prints run file names for --run-file.
  groups GROUP SUITE [--json]
      Per-client pass/fail counts, run start, and duration for the latest SUITE run in GROUP.
  groups GROUP SUITE CLIENT [--json]
      List CLIENT's failing tests in the latest SUITE run and download
      hive.log + <CLIENT>.log bundles for each into ./logs.
  list --suite NAME --client NAME [--group NAME] [--test TEXT] [--regex]
       [--status fail|pass|all] [--limit N] [--run-file FILE] [--json]
      List tests from the latest matching run with pass/fail status and log availability.
  fetch --suite NAME --client NAME --test TEXT [--group NAME] [--regex]
        [--out DIR] [--limit N] [--full-client-log] [--include-pass]
        [--run-file FILE] [--json]
      Download hive.log + <CLIENT>.log bundles for matching failing tests into --out.
`)
}

type Client struct {
	baseURL string
	http    *http.Client
}

func newClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) getRange(ctx context.Context, path string, begin, end int64) ([]byte, error) {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if begin >= 0 && end > begin {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", begin, end-1))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}

	const maxLogBytes = 200 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK && begin >= 0 && end > begin {
		if end <= int64(len(data)) {
			return data[begin:end], nil
		}
		return nil, fmt.Errorf("server ignored range and log is too small for [%d,%d)", begin, end)
	}
	return data, nil
}

type Group struct {
	Name            string   `json:"name"`
	Address         string   `json:"address"`
	GitHubWorkflows []string `json:"github_workflows"`
}

type ListingRun struct {
	Name     string            `json:"name"`
	NTests   int               `json:"ntests"`
	Passes   int               `json:"passes"`
	Fails    int               `json:"fails"`
	Timeout  bool              `json:"timeout"`
	Clients  []string          `json:"clients"`
	Versions map[string]string `json:"versions"`
	Start    time.Time         `json:"start"`
	FileName string            `json:"fileName"`
	Size     int64             `json:"size"`
	SimLog   string            `json:"simLog"`
}

type SuiteResult struct {
	ID             int                 `json:"id"`
	Name           string              `json:"name"`
	Description    string              `json:"description"`
	ClientVersions map[string]string   `json:"clientVersions"`
	TestCases      map[string]TestCase `json:"testCases"`
	SimLog         string              `json:"simLog"`
	TestDetailsLog string              `json:"testDetailsLog"`
	RunMetadata    *RunMetadata        `json:"runMetadata"`
}

type RunMetadata struct {
	HiveCommand  []string      `json:"hiveCommand"`
	HiveVersion  *HiveVersion  `json:"hiveVersion"`
	ClientConfig *ClientConfig `json:"clientConfig"`
}

type HiveVersion struct {
	Commit     string `json:"commit"`
	CommitDate string `json:"commitDate"`
	Branch     string `json:"branch"`
	Dirty      bool   `json:"dirty"`
}

type ClientConfig struct {
	FilePath string               `json:"filePath"`
	Content  *ClientConfigContent `json:"content"`
}

type ClientConfigContent struct {
	Clients []ClientConfigEntry `json:"clients"`
}

type ClientConfigEntry struct {
	Client     string            `json:"client"`
	Nametag    string            `json:"nametag"`
	Dockerfile string            `json:"dockerfile"`
	BuildArgs  map[string]string `json:"build_args"`
}

type TestCase struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Start         time.Time             `json:"start"`
	End           time.Time             `json:"end"`
	SummaryResult SummaryResult         `json:"summaryResult"`
	ClientInfo    map[string]ClientInfo `json:"clientInfo"`
}

type SummaryResult struct {
	Pass    bool      `json:"pass"`
	Timeout bool      `json:"timeout"`
	Details string    `json:"details"`
	Log     *LogRange `json:"log"`
}

type LogRange struct {
	Begin int64 `json:"begin"`
	End   int64 `json:"end"`
}

type ClientInfo struct {
	ID             string    `json:"id"`
	IP             string    `json:"ip"`
	Name           string    `json:"name"`
	InstantiatedAt string    `json:"instantiatedAt"`
	LogFile        string    `json:"logFile"`
	LogOffsets     *LogRange `json:"logOffsets"`
}

type commonFlags struct {
	baseURL string
	group   string
	suite   string
	client  string
	test    string
	runFile string
	regex   bool
	json    bool
}

func addCommonFlags(fs *flag.FlagSet, cf *commonFlags) {
	fs.StringVar(&cf.baseURL, "base-url", defaultBaseURL, "Hive results origin")
	fs.StringVar(&cf.group, "group", defaultGroup, "result group")
	fs.StringVar(&cf.suite, "suite", "", "suite/simulator name")
	fs.StringVar(&cf.client, "client", "", "client name")
	fs.StringVar(&cf.test, "test", "", "case-insensitive text matched against test names")
	fs.StringVar(&cf.runFile, "run-file", "", "specific result JSON file")
	fs.BoolVar(&cf.regex, "regex", false, "treat --test as a case-insensitive regular expression")
	fs.BoolVar(&cf.json, "json", false, "emit JSON")
}

type groupsFlags struct {
	baseURL   string
	client    string
	all       bool
	showFiles bool
	limit     int
	json      bool
}

func parseGroupsArgs(args []string) (groupsFlags, []string, error) {
	gf := groupsFlags{
		baseURL: defaultBaseURL,
		limit:   0,
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "--") {
			rest = append(rest, arg)
			continue
		}

		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch name {
		case "base-url":
			if !hasValue {
				i++
				if i >= len(args) {
					return gf, nil, errors.New("--base-url requires a value")
				}
				value = args[i]
			}
			gf.baseURL = value
		case "client":
			if !hasValue {
				i++
				if i >= len(args) {
					return gf, nil, errors.New("--client requires a value")
				}
				value = args[i]
			}
			gf.client = value
		case "limit":
			if !hasValue {
				i++
				if i >= len(args) {
					return gf, nil, errors.New("--limit requires a value")
				}
				value = args[i]
			}
			limit, err := strconv.Atoi(value)
			if err != nil {
				return gf, nil, fmt.Errorf("invalid --limit %q: %w", value, err)
			}
			gf.limit = limit
		case "all":
			if hasValue {
				v, err := strconv.ParseBool(value)
				if err != nil {
					return gf, nil, fmt.Errorf("invalid --all %q: %w", value, err)
				}
				gf.all = v
				continue
			}
			gf.all = true
		case "files":
			if hasValue {
				v, err := strconv.ParseBool(value)
				if err != nil {
					return gf, nil, fmt.Errorf("invalid --files %q: %w", value, err)
				}
				gf.showFiles = v
				continue
			}
			gf.showFiles = true
		case "json":
			if hasValue {
				v, err := strconv.ParseBool(value)
				if err != nil {
					return gf, nil, fmt.Errorf("invalid --json %q: %w", value, err)
				}
				gf.json = v
				continue
			}
			gf.json = true
		default:
			return gf, nil, fmt.Errorf("unknown groups flag --%s", name)
		}
	}
	return gf, rest, nil
}

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

	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			suite, err := fetchSuite(ctx, client, group, out[i].RunFile)
			if err != nil {
				return
			}
			out[i].Duration = suiteDuration(suite)
		}(i)
	}
	wg.Wait()

	if jsonOut {
		return writePrettyJSON(os.Stdout, out)
	}

	newest := out[0]
	for _, e := range out[1:] {
		if e.RunStart.After(newest.RunStart) {
			newest = e
		}
	}
	fmt.Printf("%s / %s\n", group, suite)
	fmt.Printf("%s\nrun=%s\n\n", formatTime(newest.RunStart), strings.TrimSuffix(newest.RunFile, ".json"))

	w := newTextTable(os.Stdout, []string{"CLIENT", "PASS", "FAIL", "START", "DURATION"}, []bool{false, true, true, false, false})
	for _, e := range out {
		w.addRow(
			textCell(e.Client),
			passCell(e.Passes),
			failCell(e.Fails),
			textCell(formatTime(e.RunStart)),
			textCell(formatHMS(e.Duration)),
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
		fmt.Printf("%s / %s / %s — no failing tests in latest run (%s)\n",
			group, suite, clientName, formatTime(run.Start))
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
	word := "tests"
	if len(bundles) == 1 {
		word = "test"
	}
	fmt.Printf("%s\nrun=%s\nurl=%s\n%s%d failing %s%s\n\n",
		formatTime(run.Start), strings.TrimSuffix(run.FileName, ".json"), bundles[0].WebsiteURL,
		ansiRed, len(bundles), word, ansiReset)
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

func formatHMS(d time.Duration) string {
	total := int(d.Round(time.Second).Seconds())
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiReset = "\x1b[0m"
)

type textTable struct {
	w       io.Writer
	headers []string
	right   []bool
	rows    [][]tableCell
}

type tableCell struct {
	text  string
	plain string
}

func newTextTable(w io.Writer, headers []string, right []bool) *textTable {
	return &textTable{w: w, headers: headers, right: right}
}

func (t *textTable) addRow(cells ...tableCell) {
	t.rows = append(t.rows, cells)
}

func (t *textTable) flush() {
	widths := make([]int, len(t.headers))
	for i, header := range t.headers {
		widths[i] = len(header)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && len(cell.plain) > widths[i] {
				widths[i] = len(cell.plain)
			}
		}
	}

	headerCells := make([]tableCell, len(t.headers))
	for i, header := range t.headers {
		headerCells[i] = textCell(header)
	}
	t.writeRow(widths, headerCells)
	for _, row := range t.rows {
		t.writeRow(widths, row)
	}
}

func (t *textTable) writeRow(widths []int, row []tableCell) {
	for i, cell := range row {
		if i > 0 {
			fmt.Fprint(t.w, "  ")
		}
		padding := widths[i] - len(cell.plain)
		if padding < 0 {
			padding = 0
		}
		if i < len(t.right) && t.right[i] {
			fmt.Fprint(t.w, strings.Repeat(" ", padding))
			fmt.Fprint(t.w, cell.text)
			continue
		}
		fmt.Fprint(t.w, cell.text)
		fmt.Fprint(t.w, strings.Repeat(" ", padding))
	}
	fmt.Fprintln(t.w)
}

func textCell(text string) tableCell {
	return tableCell{text: text, plain: text}
}

func passCell(n int) tableCell {
	return colorIntCell(n, n != 0)
}

func failCell(n int) tableCell {
	return colorIntCell(n, n == 0)
}

func colorIntCell(n int, green bool) tableCell {
	plain := strconv.Itoa(n)
	return tableCell{text: colorInt(n, green), plain: plain}
}

func colorInt(n int, green bool) string {
	color := ansiRed
	if green {
		color = ansiGreen
	}
	return fmt.Sprintf("%s%d%s", color, n, ansiReset)
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

type fetchFlags struct {
	common      commonFlags
	outDir      string
	limit       int
	fullClient  bool
	includePass bool
	status      string
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

func fetchGroups(ctx context.Context, client *Client) ([]Group, error) {
	data, err := client.get(ctx, hiveDiscoveryFile)
	if err != nil {
		return nil, err
	}
	var groups []Group
	if err := json.Unmarshal(data, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func fetchListing(ctx context.Context, client *Client, group string) ([]ListingRun, error) {
	data, err := client.get(ctx, pathJoin(group, hiveListingFile))
	if err != nil {
		return nil, err
	}
	var runs []ListingRun
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var run ListingRun
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			return nil, fmt.Errorf("decode listing line: %w", err)
		}
		runs = append(runs, run)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

func fetchSuite(ctx context.Context, client *Client, group, fileName string) (*SuiteResult, error) {
	data, err := client.get(ctx, pathJoin(group, hiveResultsDir, fileName))
	if err != nil {
		return nil, err
	}
	var suite SuiteResult
	if err := json.Unmarshal(data, &suite); err != nil {
		return nil, err
	}
	return &suite, nil
}

func selectRun(ctx context.Context, client *Client, cf commonFlags) (ListingRun, error) {
	if cf.runFile != "" {
		return ListingRun{
			Name:     cf.suite,
			FileName: cf.runFile,
			Clients:  []string{cf.client},
		}, nil
	}

	runs, err := fetchListing(ctx, client, cf.group)
	if err != nil {
		return ListingRun{}, err
	}
	matches := filterRuns(runs, cf.suite, cf.client, "latest")
	if len(matches) == 0 {
		return ListingRun{}, fmt.Errorf("no run found for group=%s suite=%s client=%s", cf.group, cf.suite, cf.client)
	}
	sortRunsNewestFirst(matches)
	return matches[0], nil
}

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

type BundleSummary struct {
	Directory             string   `json:"directory"`
	WebsiteURL            string   `json:"website_url"`
	SummaryPath           string   `json:"summary_path"`
	ReproduceCommandsPath string   `json:"reproduce_commands_path"`
	HiveLogPath           string   `json:"hive_log_path"`
	ClientLogPath         string   `json:"client_log_path"`
	ClientLogs            []string `json:"client_logs"`
	TestName              string   `json:"test_name"`
	TestID                string   `json:"test_id"`
	RunFile               string   `json:"run_file"`
}

func fetchBundle(ctx context.Context, client *Client, ff fetchFlags, run ListingRun, suite *SuiteResult, match TestMatch) (BundleSummary, error) {
	dir := filepath.Join(ff.outDir, bundleDirName(ff.common.group, ff.common.suite, ff.common.client, run, match))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BundleSummary{}, err
	}

	meta := buildMetadata(client.baseURL, ff.common.group, run, suite, match)
	summaryPath := filepath.Join(dir, "summary.json")
	if err := writeJSONFile(summaryPath, meta); err != nil {
		return BundleSummary{}, err
	}

	hivePath := filepath.Join(dir, "hive.log")
	hiveLog, err := fetchHiveLog(ctx, client, ff.common.group, suite, match)
	if err != nil {
		hiveLog = []byte(fmt.Sprintf("failed to fetch hive log: %v\n", err))
	}
	if err := os.WriteFile(hivePath, hiveLog, 0o644); err != nil {
		return BundleSummary{}, err
	}

	clientLogName := normalizeClient(ff.common.client) + ".log"
	clientPath := filepath.Join(dir, clientLogName)
	clientLog, clientFiles, err := fetchClientLogs(ctx, client, ff, match)
	if err != nil {
		clientLog = []byte(fmt.Sprintf("failed to fetch client log: %v\n", err))
	}
	if err := os.WriteFile(clientPath, clientLog, 0o644); err != nil {
		return BundleSummary{}, err
	}

	reproduceCommandsPath := filepath.Join(dir, "reproduce_commands.md")
	if err := writeReproduceCommands(reproduceCommandsPath, meta, "hive.log", clientLogName); err != nil {
		return BundleSummary{}, err
	}

	return BundleSummary{
		Directory:             dir,
		WebsiteURL:            meta.WebsiteURL,
		SummaryPath:           summaryPath,
		ReproduceCommandsPath: reproduceCommandsPath,
		HiveLogPath:           hivePath,
		ClientLogPath:         clientPath,
		ClientLogs:            clientFiles,
		TestName:              match.Test.Name,
		TestID:                match.TestID,
		RunFile:               run.FileName,
	}, nil
}

func fetchHiveLog(ctx context.Context, client *Client, group string, suite *SuiteResult, match TestMatch) ([]byte, error) {
	if suite.TestDetailsLog == "" {
		return []byte(match.Test.SummaryResult.Details), nil
	}
	if match.Test.SummaryResult.Log == nil {
		return []byte(match.Test.SummaryResult.Details), nil
	}
	logPath := pathJoin(group, hiveResultsDir, suite.TestDetailsLog)
	data, err := client.getRange(ctx, logPath, match.Test.SummaryResult.Log.Begin, match.Test.SummaryResult.Log.End)
	if err != nil {
		return nil, err
	}
	if details := strings.TrimSpace(match.Test.SummaryResult.Details); details != "" {
		data = append([]byte(details+"\n\n"), data...)
	}
	return data, nil
}

func fetchClientLogs(ctx context.Context, client *Client, ff fetchFlags, match TestMatch) ([]byte, []string, error) {
	if len(match.Test.ClientInfo) == 0 {
		return nil, nil, errors.New("test has no clientInfo")
	}
	infos := make([]ClientInfo, 0, len(match.Test.ClientInfo))
	for _, info := range match.Test.ClientInfo {
		infos = append(infos, info)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].ID < infos[j].ID
	})

	var out bytes.Buffer
	var files []string
	for _, info := range infos {
		if info.LogFile == "" {
			continue
		}
		files = append(files, info.LogFile)
		fmt.Fprintf(&out, "===== client %s id=%s ip=%s log=%s =====\n", info.Name, info.ID, info.IP, info.LogFile)
		begin, end := int64(-1), int64(-1)
		if !ff.fullClient && info.LogOffsets != nil {
			begin, end = info.LogOffsets.Begin, info.LogOffsets.End
		}
		data, err := client.getRange(ctx, pathJoin(ff.common.group, hiveResultsDir, info.LogFile), begin, end)
		if err != nil {
			fmt.Fprintf(&out, "failed to fetch log: %v\n\n", err)
			continue
		}
		out.Write(data)
		if len(data) == 0 || data[len(data)-1] != '\n' {
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	if len(files) == 0 {
		return nil, nil, errors.New("test has no client log files")
	}
	return out.Bytes(), files, nil
}

type FailureMetadata struct {
	Group           string              `json:"group"`
	Suite           string              `json:"suite"`
	Client          string              `json:"client"`
	ClientVersions  map[string]string   `json:"client_versions"`
	RunStart        time.Time           `json:"run_start"`
	RunFile         string              `json:"run_file"`
	WebsiteURL      string              `json:"website_url"`
	TestID          string              `json:"test_id"`
	TestName        string              `json:"test_name"`
	TestDescription string              `json:"test_description"`
	Pass            bool                `json:"pass"`
	Timeout         bool                `json:"timeout"`
	HiveCommand     []string            `json:"hive_command"`
	HiveVersion     *HiveVersion        `json:"hive_version,omitempty"`
	ClientInfo      []ClientInfo        `json:"client_info"`
	BuildInfo       []ClientConfigEntry `json:"build_info,omitempty"`
}

func buildMetadata(baseURL, group string, run ListingRun, suite *SuiteResult, match TestMatch) FailureMetadata {
	infos := make([]ClientInfo, 0, len(match.Test.ClientInfo))
	for _, info := range match.Test.ClientInfo {
		infos = append(infos, info)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].ID < infos[j].ID
	})

	var hiveCommand []string
	var hiveVersion *HiveVersion
	var buildInfo []ClientConfigEntry
	if suite.RunMetadata != nil {
		hiveCommand = suite.RunMetadata.HiveCommand
		hiveVersion = suite.RunMetadata.HiveVersion
		if suite.RunMetadata.ClientConfig != nil && suite.RunMetadata.ClientConfig.Content != nil {
			buildInfo = suite.RunMetadata.ClientConfig.Content.Clients
		}
	}

	return FailureMetadata{
		Group:           group,
		Suite:           suite.Name,
		Client:          strings.Join(normalizedClients(run.Clients), ","),
		ClientVersions:  suite.ClientVersions,
		RunStart:        run.Start,
		RunFile:         run.FileName,
		WebsiteURL:      fmt.Sprintf(hiveTestURLFormat, strings.TrimRight(baseURL, "/"), url.PathEscape(group), url.PathEscape(strings.TrimSuffix(run.FileName, ".json"))),
		TestID:          match.TestID,
		TestName:        match.Test.Name,
		TestDescription: cleanDescription(match.Test.Description),
		Pass:            match.Test.SummaryResult.Pass,
		Timeout:         match.Test.SummaryResult.Timeout,
		HiveCommand:     hiveCommand,
		HiveVersion:     hiveVersion,
		ClientInfo:      infos,
		BuildInfo:       buildInfo,
	}
}

func writeReproduceCommands(path string, meta FailureMetadata, hiveLogName, clientLogName string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Reproduce Commands\n\n")
	fmt.Fprintf(&b, "Group: `%s`\n\n", meta.Group)
	fmt.Fprintf(&b, "Suite: `%s`\n\n", meta.Suite)
	fmt.Fprintf(&b, "Client: `%s`\n\n", meta.Client)
	fmt.Fprintf(&b, "Run: `%s` at `%s`\n\n", meta.RunFile, formatTime(meta.RunStart))
	fmt.Fprintf(&b, "Test ID: `%s`\n\n", meta.TestID)
	fmt.Fprintf(&b, "Test name: `%s`\n\n", meta.TestName)
	if meta.TestDescription != "" {
		fmt.Fprintf(&b, "Description:\n%s\n\n", meta.TestDescription)
	}
	if len(meta.HiveCommand) > 0 {
		fmt.Fprintf(&b, "Hive command:\n```sh\n%s\n```\n\n", shellJoin(meta.HiveCommand))
	}
	fmt.Fprintf(&b, "Analyze `%s` and `%s` together. Identify the first meaningful failure, explain whether it looks like a client bug, test issue, infrastructure issue, or inconclusive, and suggest the smallest next reproduction command or code area to inspect.\n", hiveLogName, clientLogName)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeJSONFile(path string, v any) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func writePrettyJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func bundleDirName(group, suite, client string, run ListingRun, match TestMatch) string {
	name := sanitizeFileName(match.Test.Name)
	if len(name) > 90 {
		name = name[:90]
	}
	leaf := name + "-" + strings.TrimSuffix(run.FileName, ".json")
	full := filepath.Join(
		sanitizeFileName(group),
		sanitizePathSegments(suite),
		sanitizeFileName(normalizeClient(client)),
		leaf,
	)
	return strings.ToLower(full)
}

// sanitizePathSegments sanitizes each `/`-separated segment so e.g.
// `eels/consume-engine` becomes `eels/consume-engine` (segments cleaned but
// the slash preserved as a path separator).
func sanitizePathSegments(s string) string {
	parts := strings.Split(s, "/")
	for i, p := range parts {
		parts[i] = sanitizeFileName(p)
	}
	return strings.Join(parts, "/")
}

func sanitizeFileName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeClient(key string) string {
	key = strings.TrimSpace(key)
	for _, known := range hiveKnownClients {
		if key == known || strings.HasPrefix(key, known+"_") {
			return known
		}
	}
	if idx := strings.LastIndex(key, "_"); idx > 0 {
		return key[:idx]
	}
	if alias, ok := hiveClientAliases[key]; ok {
		return alias
	}
	return key
}

func normalizedClients(clients []string) []string {
	out := make([]string, 0, len(clients))
	for _, client := range clients {
		out = append(out, normalizeClient(client))
	}
	return out
}

func normalizedSortedClients(clients []string) []string {
	out := normalizedClients(clients)
	sort.Strings(out)
	return out
}

func simulatorName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func pathJoin(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			cleaned = append(cleaned, strings.Trim(part, "/"))
		}
	}
	return strings.Join(cleaned, "/")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func cleanDescription(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	tagRe := regexp.MustCompile(`<[^>]+>`)
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func shellJoin(args []string) string {
	var out []string
	for _, arg := range args {
		if arg == "" {
			out = append(out, "''")
			continue
		}
		if strings.IndexFunc(arg, func(r rune) bool {
			return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '=' || r == ':' || r == ',' || r == '+' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
		}) == -1 {
			out = append(out, arg)
			continue
		}
		out = append(out, strconv.Quote(arg))
	}
	return strings.Join(out, " ")
}
