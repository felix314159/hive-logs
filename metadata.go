// Package main builds structured metadata summaries that describe fetched Hive failure bundles.
package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

func hiveTestURL(baseURL, group, runFile string) string {
	return fmt.Sprintf(hiveTestURLFormat,
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(group),
		url.PathEscape(strings.TrimSuffix(runFile, ".json")))
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
	TestFile              string   `json:"test_file,omitempty"`
	TestVector            string   `json:"test_vector"`
	TestID                string   `json:"test_id"`
	RunFile               string   `json:"run_file"`
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
		WebsiteURL:      hiveTestURL(baseURL, group, run.FileName),
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
