package main

import "testing"

func TestNormalizeClientHandlesAliasesSuffixesAndFallback(t *testing.T) {
	tests := map[string]string{
		" geth ":             "go-ethereum",
		"go-ethereum_main":   "go-ethereum",
		"nimbusel":           "nimbus-el",
		"custom_client_test": "custom_client",
		"unknown":            "unknown",
	}
	for input, want := range tests {
		if got := normalizeClient(input); got != want {
			t.Fatalf("normalizeClient(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsKnownClientNameHandlesCanonicalAliasesAndSuffixes(t *testing.T) {
	for _, name := range []string{"besu", "geth", "go-ethereum_main"} {
		if !isKnownClientName(name) {
			t.Fatalf("isKnownClientName(%q) = false, want true", name)
		}
	}
	if isKnownClientName("engine-api") {
		t.Fatal("isKnownClientName(\"engine-api\") = true, want false")
	}
}

func TestCleanClientVersionAndCommitExtraction(t *testing.T) {
	tests := []struct {
		client string
		raw    string
		want   string
	}{
		{"go-ethereum", "Geth/v1.15.0/linux abcdef123456", "1.15.0"},
		{"ethrex", "ethrex/v0.9.1-main-abcdef0/x86", "0.9.1"},
		{"reth", "Reth Version: 1.3.4", "1.3.4"},
		{"nethermind", "v1.31.0-custom", "1.31.0-custom"},
	}
	for _, tt := range tests {
		if got := cleanClientVersion(tt.client, tt.raw); got != tt.want {
			t.Fatalf("cleanClientVersion(%q, %q) = %q, want %q", tt.client, tt.raw, got, tt.want)
		}
	}
	if got := extractCommitHash("build ABCDEF1234567890 done"); got != "abcdef1" {
		t.Fatalf("extractCommitHash() = %q", got)
	}
}

func TestSuiteClientVersionInfoMatchesCanonicalName(t *testing.T) {
	suite := &SuiteResult{ClientVersions: map[string]string{
		"go-ethereum_main": "Geth/v1.15.0/linux abcdef123456",
	}}
	version, commit := suiteClientVersionInfo(suite, "go-ethereum")
	if version != "1.15.0" || commit != "abcdef1" {
		t.Fatalf("version=%q commit=%q", version, commit)
	}
}

func TestSuiteClientBranchAndFixtures(t *testing.T) {
	suite := &SuiteResult{RunMetadata: &RunMetadata{
		HiveCommand: []string{
			"hive",
			"--sim.buildarg", "fixtures=https://github.com/ethereum/execution-spec-tests/releases/download/v4.2.0/fixtures.tar.gz",
			"--sim.buildarg", "branch=pectra-devnet-5",
		},
		ClientConfig: &ClientConfig{Content: &ClientConfigContent{Clients: []ClientConfigEntry{
			{BuildArgs: map[string]string{"tag": "main"}},
		}}},
	}}

	if got := suiteClientBranch(suite); got != "main" {
		t.Fatalf("suiteClientBranch() = %q", got)
	}
	fx := suiteFixtures(suite)
	if fx.Release != "v4.2.0" || fx.Branch != "pectra-devnet-5" ||
		fx.URL != "https://github.com/ethereum/execution-spec-tests/releases/tag/v4.2.0" {
		t.Fatalf("fixtures = %+v", fx)
	}
}

func TestParseFixturesAssetURLRejectsNonReleaseAsset(t *testing.T) {
	release, tagURL := parseFixturesAssetURL("https://example.test/not-a-release")
	if release != "" || tagURL != "" {
		t.Fatalf("release=%q tagURL=%q", release, tagURL)
	}
}
