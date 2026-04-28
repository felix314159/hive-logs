package main

import (
	"strings"
	"testing"
)

func TestParseQueryArgsInterleavesFlagsAndKeyValues(t *testing.T) {
	qf, err := parseQueryArgs([]string{
		"group=generic",
		"--base-url=https://example.test",
		"--limit", "3",
		"--all=false",
		"--files",
		"--json=true",
		"suite=eels/consume-rlp",
		"client=nimbus-el",
	})
	if err != nil {
		t.Fatal(err)
	}

	if qf.baseURL != "https://example.test" || qf.limit != 3 || qf.all ||
		!qf.showFiles || !qf.json {
		t.Fatalf("query flags = %+v", qf)
	}
	if qf.common.group != "generic" || qf.common.suite != "eels/consume-rlp" || qf.common.client != "nimbus-el" {
		t.Fatalf("common = %+v", qf.common)
	}
}

func TestParseQueryArgsRejectsUnknownKey(t *testing.T) {
	if _, err := parseQueryArgs([]string{"team=infra"}); err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestParseQueryArgsReportsBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--client"},
		{"--client", "geth"},
		{"--limit", "nope"},
		{"--json=maybe"},
		{"--unknown"},
		{"generic"},
	} {
		if _, err := parseQueryArgs(args); err == nil {
			t.Fatalf("parseQueryArgs(%v) returned nil error", args)
		}
	}
}

func TestParseQueryArgsSuggestsNextMissingKey(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no key set suggests group",
			args: []string{"generic"},
			want: []string{`missing key for "generic"`, "did you mean group=generic?"},
		},
		{
			name: "group set suggests suite",
			args: []string{"group=bal", "eels/consume-engine"},
			want: []string{`missing key for "eels/consume-engine"`, "did you mean suite=eels/consume-engine?"},
		},
		{
			name: "group and suite set suggests client",
			args: []string{"group=bal", "suite=eels/consume-engine", "nimbus-el"},
			want: []string{`missing key for "nimbus-el"`, "did you mean client=nimbus-el?"},
		},
		{
			name: "all keys set rejects extra positional",
			args: []string{"group=bal", "suite=eels/consume-engine", "client=nimbus-el", "extra"},
			want: []string{`unexpected positional argument "extra"`, "group=, suite=, and client= are already set"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseQueryArgs(tc.args)
			if err == nil {
				t.Fatalf("parseQueryArgs(%v) returned nil error", tc.args)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not contain %q", err.Error(), want)
				}
			}
		})
	}
}
