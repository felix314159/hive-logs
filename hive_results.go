// Package main decodes Hive result files and selects the run that command handlers should inspect.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

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
