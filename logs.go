// Package main retrieves hive.log and client log contents from Hive result files for selected test cases.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var errNoClientLog = errors.New("no client log exists for this test")

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
		return nil, nil, errNoClientLog
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
		return nil, nil, errNoClientLog
	}
	return out.Bytes(), files, nil
}
