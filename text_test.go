package main

import "testing"

func TestTextHelpers(t *testing.T) {
	if got := firstLine("alpha\r\nbeta"); got != "alpha" {
		t.Fatalf("firstLine() = %q", got)
	}
	if got := simulatorName("eels/consume-engine"); got != "consume-engine" {
		t.Fatalf("simulatorName() = %q", got)
	}
	if got := pathJoin("/generic/", "/results/", "run.json"); got != "generic/results/run.json" {
		t.Fatalf("pathJoin() = %q", got)
	}
}

func TestCleanDescriptionConvertsHTMLToPlainText(t *testing.T) {
	got := cleanDescription(" alpha<br/><b>beta</b>&amp;gamma\r\n ")
	if got != "alpha\nbeta&gamma" {
		t.Fatalf("cleanDescription() = %q", got)
	}
}

func TestShellJoinQuotesOnlyWhenNeeded(t *testing.T) {
	got := shellJoin([]string{"hive", "--sim.buildarg", "branch=main", "has space", ""})
	if got != `hive --sim.buildarg branch=main "has space" ''` {
		t.Fatalf("shellJoin() = %q", got)
	}
}
