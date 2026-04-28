// Package main models the Hive JSON records consumed from discovery, listing, suite, and log metadata files.
package main

import "time"

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
