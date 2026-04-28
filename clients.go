// Package main normalizes Hive client names and extracts client version, branch, and fixture metadata.
package main

import (
	"regexp"
	"sort"
	"strings"
)

func suiteClientVersionInfo(suite *SuiteResult, canonical string) (version, commit string) {
	for k, v := range suite.ClientVersions {
		if normalizeClient(k) != canonical {
			continue
		}
		line := firstLine(v)
		return cleanClientVersion(canonical, line), extractCommitHash(line)
	}
	return "", ""
}

// cleanClientVersion strips per-client noise (binary name, build host, etc.)
// from the runtime version string and leaves just the actionable bit. The
// raw shapes vary by client, so each one needs its own rule.
func cleanClientVersion(canonical, raw string) string {
	raw = strings.TrimSpace(raw)
	cleaned := raw
	switch canonical {
	case "besu", "go-ethereum", "nimbus-el":
		// "<Name>/<version>/<arch>/<extras>" — keep the version segment.
		if parts := strings.SplitN(raw, "/", 3); len(parts) >= 2 {
			cleaned = parts[1]
		}
	case "ethrex":
		// "ethrex/<version>-<branch>-<commit>/<arch>/..." — keep just the
		// leading semver tag before the branch/commit fragments.
		if parts := strings.SplitN(raw, "/", 3); len(parts) >= 2 {
			cleaned = parts[1]
			if i := strings.Index(cleaned, "-"); i > 0 {
				cleaned = cleaned[:i]
			}
		}
	case "reth":
		cleaned = strings.TrimPrefix(raw, "Reth Version: ")
	}
	return strings.TrimPrefix(cleaned, "v")
}

var commitHashRe = regexp.MustCompile(`(?i)[0-9a-f]{7,40}`)

// extractCommitHash returns the first 7 chars of the first hex run that looks
// like a git commit SHA in the raw version string, or "" if none is present.
func extractCommitHash(raw string) string {
	m := commitHashRe.FindString(raw)
	if m == "" {
		return ""
	}
	if len(m) > 7 {
		return strings.ToLower(m[:7])
	}
	return strings.ToLower(m)
}

func suiteClientBranch(suite *SuiteResult) string {
	if suite.RunMetadata == nil || suite.RunMetadata.ClientConfig == nil ||
		suite.RunMetadata.ClientConfig.Content == nil {
		return ""
	}
	for _, c := range suite.RunMetadata.ClientConfig.Content.Clients {
		if tag, ok := c.BuildArgs["tag"]; ok && tag != "" {
			return tag
		}
	}
	return ""
}

func suiteFixtures(suite *SuiteResult) fixturesInfo {
	var info fixturesInfo
	if suite.RunMetadata == nil {
		return info
	}
	cmd := suite.RunMetadata.HiveCommand
	for i := 0; i+1 < len(cmd); i++ {
		if cmd[i] != "--sim.buildarg" {
			continue
		}
		v := cmd[i+1]
		switch {
		case strings.HasPrefix(v, "fixtures="):
			info.Release, info.URL = parseFixturesAssetURL(strings.TrimPrefix(v, "fixtures="))
		case strings.HasPrefix(v, "branch="):
			info.Branch = strings.TrimPrefix(v, "branch=")
		}
	}
	return info
}

// parseFixturesAssetURL takes a GitHub release asset URL like
// https://github.com/<org>/<repo>/releases/download/<tag>/<file> and returns
// the tag and the corresponding /releases/tag/<tag> page URL.
func parseFixturesAssetURL(assetURL string) (release, tagURL string) {
	const marker = "/releases/download/"
	i := strings.Index(assetURL, marker)
	if i < 0 {
		return "", ""
	}
	base := assetURL[:i]
	rest := assetURL[i+len(marker):]
	if j := strings.Index(rest, "/"); j > 0 {
		release = rest[:j]
	} else {
		release = rest
	}
	if release == "" {
		return "", ""
	}
	return release, base + "/releases/tag/" + release
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

func isKnownClientName(name string) bool {
	canonical := normalizeClient(name)
	for _, known := range hiveKnownClients {
		if canonical == known {
			return true
		}
	}
	return false
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
