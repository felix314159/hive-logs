package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatVersionFixtureAndClientLines(t *testing.T) {
	if got := formatHiveVersion(&HiveVersion{Branch: "main", Commit: "abcdef123456"}); got != "Hive: branch=main, commit=abcdef1" {
		t.Fatalf("formatHiveVersion() = %q", got)
	}
	if got := formatClientInfo("reth", "main", "1234567", "1.0.0"); got != "Client: reth, branch=main, commit=1234567, version=1.0.0" {
		t.Fatalf("formatClientInfo() = %q", got)
	}
	if got := formatFixtures(fixturesInfo{Branch: "dev", Release: "v1", URL: "https://example.test"}); got != "EELS: branch=dev, fixtures=v1, url=https://example.test" {
		t.Fatalf("formatFixtures() = %q", got)
	}
	start := time.Date(2026, 4, 28, 9, 1, 24, 0, time.UTC).Local()
	if got := formatRunHeader("1234567-abcdef.json", start, "https://example.test/run"); got != "Ethpandaops: run=1234567-abcdef, date="+formatTime(start)+", url=https://example.test/run" {
		t.Fatalf("formatRunHeader() = %q", got)
	}
}

func TestTruncateFormatHMSAndYesNo(t *testing.T) {
	if got := truncate("abcdef", 5); got != "ab..." {
		t.Fatalf("truncate() = %q", got)
	}
	if got := formatHMS(2*time.Hour + 3*time.Minute + 4*time.Second + 400*time.Millisecond); got != "02:03:04" {
		t.Fatalf("formatHMS() = %q", got)
	}
	if yesNo(true) != "yes" || yesNo(false) != "no" {
		t.Fatalf("yesNo returned unexpected values")
	}
}

func TestTextTablePadsAndRightAligns(t *testing.T) {
	var buf bytes.Buffer
	table := newTextTable(&buf, []string{"NAME", "COUNT"}, []bool{false, true})
	table.addRow(textCell("a"), colorIntCell(7, true))
	table.addRow(textCell("longer"), colorIntCell(0, false))
	table.flush()

	output := buf.String()
	if !strings.Contains(output, "NAME    COUNT") ||
		!strings.Contains(output, "a           "+ansiGreen+"7"+ansiReset) ||
		!strings.Contains(output, "longer      "+ansiRed+"0"+ansiReset) {
		t.Fatalf("table output:\n%s", output)
	}
}

func TestWritePrettyJSONIndentsOutput(t *testing.T) {
	var buf bytes.Buffer
	if err := writePrettyJSON(&buf, map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "{\n  \"n\": 1\n}\n" {
		t.Fatalf("json = %q", got)
	}
}
