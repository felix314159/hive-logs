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

// minClientLogSliceBytes is the smallest LogOffsets slice we treat as
// meaningful. Hive sometimes records a 1-byte (or 0-byte) slice for a test
// that was never actually executed — for example, when the consensus
// simulator's "test file loader" meta-test fails and the per-test offsets
// just point at the current end of the client log file. Below this
// threshold we ignore the slice and fetch the entire client log so the
// bundle isn't effectively empty.
const minClientLogSliceBytes = 64

// minClientLogSliceFraction triggers a fall-back to the full client log
// when the recorded slice is smaller than 1/N of the underlying file. The
// consensus simulator (used by suites like legacy and legacy-cancun)
// records offsets around a narrow window of client activity, so the slice
// can be a few hundred bytes of an otherwise multi-KB log that contains
// the actual test failure. In that case the slice is technically valid
// but useless for diagnosis, and refetching the full log is much more
// helpful than the few lines hive happened to capture.
const minClientLogSliceFraction = 10

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
		if !ff.fullClient && info.LogOffsets != nil &&
			info.LogOffsets.End-info.LogOffsets.Begin >= minClientLogSliceBytes {
			begin, end = info.LogOffsets.Begin, info.LogOffsets.End
		}
		logPath := pathJoin(ff.common.group, hiveResultsDir, info.LogFile)
		data, fileSize, err := client.getRangeWithSize(ctx, logPath, begin, end)
		if err != nil {
			fmt.Fprintf(&out, "failed to fetch log: %v\n\n", err)
			continue
		}
		if begin >= 0 && fileSize > 0 && int64(len(data))*minClientLogSliceFraction < fileSize {
			full, _, fullErr := client.getRangeWithSize(ctx, logPath, -1, -1)
			if fullErr == nil {
				data = full
			}
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
