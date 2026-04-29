// Package main writes fetched failure bundles and handles local path construction and filename sanitization.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	rpcCompatLaunchLogMu    sync.Mutex
	rpcCompatLaunchLogCache = make(map[string]string)
)

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
		if errors.Is(err, errNoClientLog) {
			clientLog = missingClientLogMessage()
			if isRPCCompatSuite(ff.common.suite, suite.Name) {
				launchPath, launchErr := ensureRPCCompatClientLaunchLog(ctx, client, ff, run, suite)
				if launchErr == nil {
					clientLog = rpcCompatClientLogReference(clientPath, launchPath)
				} else {
					clientLog = append(clientLog, []byte(fmt.Sprintf("failed to fetch rpc-compat client launch log: %v\n", launchErr))...)
				}
			}
		} else {
			clientLog = []byte(fmt.Sprintf("failed to fetch client log: %v\n", err))
		}
	}
	if err := os.WriteFile(clientPath, clientLog, 0o644); err != nil {
		return BundleSummary{}, err
	}

	reproduceCommandsPath := filepath.Join(dir, "reproduce_commands.md")
	if err := writeReproduceCommands(reproduceCommandsPath, meta, "hive.log", clientLogName); err != nil {
		return BundleSummary{}, err
	}

	testFile, testVector := splitTestName(match.Test.Name)
	return BundleSummary{
		Directory:             dir,
		WebsiteURL:            meta.WebsiteURL,
		SummaryPath:           summaryPath,
		ReproduceCommandsPath: reproduceCommandsPath,
		HiveLogPath:           hivePath,
		ClientLogPath:         clientPath,
		ClientLogs:            clientFiles,
		TestName:              match.Test.Name,
		TestFile:              testFile,
		TestVector:            testVector,
		TestID:                match.TestID,
		RunFile:               run.FileName,
	}, nil
}

func missingClientLogMessage() []byte {
	return []byte("no client log exists for this test\n")
}

func isRPCCompatSuite(names ...string) bool {
	for _, name := range names {
		if name == "rpc-compat" || simulatorName(name) == "rpc-compat" {
			return true
		}
	}
	return false
}

func ensureRPCCompatClientLaunchLog(ctx context.Context, client *Client, ff fetchFlags, run ListingRun, suite *SuiteResult) (string, error) {
	launch, ok := findRPCCompatClientLaunch(suite, ff.common.client)
	if !ok {
		return "", errors.New("rpc-compat client launch test not found")
	}

	dir := filepath.Join(
		ff.outDir,
		sanitizeFileName(ff.common.group),
		sanitizePathSegments(ff.common.suite),
		sanitizeFileName(normalizeClient(ff.common.client)),
	)
	path := filepath.Join(dir, "client_launch.log")
	cacheKey := run.FileName + "\x00" + path

	rpcCompatLaunchLogMu.Lock()
	if cached, ok := rpcCompatLaunchLogCache[cacheKey]; ok {
		rpcCompatLaunchLogMu.Unlock()
		return cached, nil
	}
	rpcCompatLaunchLogMu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	launchFlags := ff
	launchFlags.fullClient = true
	data, _, err := fetchClientLogs(ctx, client, launchFlags, launch)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}

	rpcCompatLaunchLogMu.Lock()
	rpcCompatLaunchLogCache[cacheKey] = path
	rpcCompatLaunchLogMu.Unlock()
	return path, nil
}

func findRPCCompatClientLaunch(suite *SuiteResult, clientName string) (TestMatch, bool) {
	var fallback TestMatch
	foundFallback := false
	for id, tc := range suite.TestCases {
		if !isRPCCompatClientLaunchName(tc.Name) {
			continue
		}
		match := TestMatch{TestID: id, Test: tc}
		if clientLaunchMatchesClient(tc, clientName) {
			return match, true
		}
		if !foundFallback {
			fallback = match
			foundFallback = true
		}
	}
	if foundFallback {
		return fallback, true
	}
	return TestMatch{}, false
}

func isRPCCompatClientLaunchName(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	return name == "client launch" || strings.HasPrefix(name, "client launch ")
}

func clientLaunchMatchesClient(tc TestCase, clientName string) bool {
	want := normalizeClient(clientName)
	if want == "" {
		return true
	}
	for _, info := range tc.ClientInfo {
		if normalizeClient(info.Name) == want {
			return true
		}
		if strings.HasPrefix(info.LogFile, clientName+"/") || strings.HasPrefix(info.LogFile, want+"/") {
			return true
		}
	}
	return false
}

func rpcCompatClientLogReference(clientPath, launchPath string) []byte {
	ref, err := filepath.Rel(filepath.Dir(clientPath), launchPath)
	if err != nil {
		ref = launchPath
	}
	ref = filepath.ToSlash(ref)
	return []byte(fmt.Sprintf("no client log exists for this test; see %s for the rpc-compat client launch log\n", ref))
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

func bundleDirName(group, suite, client string, run ListingRun, match TestMatch) string {
	file, vector := splitTestName(match.Test.Name)
	runSuffix := strings.TrimSuffix(run.FileName, ".json")
	base := filepath.Join(
		sanitizeFileName(group),
		sanitizePathSegments(suite),
		sanitizeFileName(normalizeClient(client)),
	)

	if file != "" && vector != "" {
		fileLeaf := truncateBundleName(sanitizeFileName(file)) + "-" + runSuffix
		return strings.ToLower(filepath.Join(base, fileLeaf, bundleVectorLeaf(vector, match.TestID)))
	}
	// File-only entry (or unparsed name): keep the bundle in a single
	// flat directory so non-pytest suites like discv4 don't grow an extra
	// nested vector level for tests that have no inner vector.
	leaf := bundleVectorLeaf(match.Test.Name, match.TestID) + "-" + runSuffix
	return strings.ToLower(filepath.Join(base, leaf))
}

// bundleVectorLeaf builds the per-vector directory name. The test ID is
// always appended when present so two vectors that share a sanitized prefix
// (e.g. after truncation) still land in distinct directories.
func bundleVectorLeaf(vector, testID string) string {
	name := truncateBundleName(sanitizeFileName(vector))
	if testID == "" {
		if name == "" {
			return "test"
		}
		return name
	}
	if name == "" {
		return "test-" + testID
	}
	return name + "-" + testID
}

// truncateBundleName caps a sanitized name at a length that keeps the full
// path well under common filesystem limits. The trailing dash, if any, is
// stripped so the next segment joins cleanly.
func truncateBundleName(name string) string {
	const maxLen = 120
	if len(name) <= maxLen {
		return name
	}
	return strings.TrimRight(name[:maxLen], "-")
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
