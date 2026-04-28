// Package main writes fetched failure bundles and handles local path construction and filename sanitization.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
