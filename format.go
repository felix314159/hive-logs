// Package main formats hive-logs data for terminal tables, JSON output, status colors, and compact summaries.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

func formatRunHeader(runFile string, runStart time.Time, websiteURL string) string {
	parts := make([]string, 0, 3)
	if runFile != "" {
		parts = append(parts, "run="+strings.TrimSuffix(runFile, ".json"))
	}
	if !runStart.IsZero() {
		parts = append(parts, "date="+formatTime(runStart))
	}
	if websiteURL != "" {
		parts = append(parts, "url="+websiteURL)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Ethpandaops: " + strings.Join(parts, ", ")
}

func formatHiveVersion(hv *HiveVersion) string {
	if hv == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if hv.Branch != "" {
		parts = append(parts, "branch="+hv.Branch)
	}
	if hv.Commit != "" {
		short := hv.Commit
		if len(short) > 7 {
			short = short[:7]
		}
		parts = append(parts, "commit="+short)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Hive: " + strings.Join(parts, ", ")
}

func formatClientInfo(name, branch, commit, version string) string {
	parts := make([]string, 0, 4)
	if name != "" {
		parts = append(parts, name)
	}
	if branch != "" {
		parts = append(parts, "branch="+branch)
	}
	if commit != "" {
		parts = append(parts, "commit="+commit)
	}
	if version != "" {
		parts = append(parts, "version="+version)
	}
	if len(parts) <= 1 {
		return ""
	}
	return "Client: " + strings.Join(parts, ", ")
}

func formatFixtures(fx fixturesInfo) string {
	parts := make([]string, 0, 3)
	if fx.Branch != "" {
		parts = append(parts, "branch="+fx.Branch)
	}
	if fx.Release != "" {
		parts = append(parts, "fixtures="+fx.Release)
	}
	if fx.URL != "" {
		parts = append(parts, "url="+fx.URL)
	}
	if len(parts) == 0 {
		return ""
	}
	return "EELS: " + strings.Join(parts, ", ")
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
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
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiGrey   = "\x1b[90m"
	ansiOrange = "\x1b[38;5;208m"
	ansiReset  = "\x1b[0m"
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

func writePrettyJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
