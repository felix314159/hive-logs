// Package main records the external Hive deployment paths, URLs, and client names that hive-logs depends on.
package main

// External assumptions about the Hive deployment we point at.
// Anything in this file is a coupling to an outside system; if Hive
// reorganizes its layout, web routes, or naming, this is where the breakage
// will surface and where the fix belongs.

const (
	// defaultBaseURL is the public Hive results origin maintained by ethpandaops.
	// Override per-invocation with --base-url.
	defaultBaseURL = "https://hive.ethpandaops.io"

	// defaultGroup is the result group most users care about. Hive instances
	// can expose other groups (e.g. bal); see `hive-logs groups`.
	defaultGroup = "generic"
)

// Hive data layout under the origin. These are filenames and directories the
// Hive results server publishes; they are not under our control.
const (
	hiveDiscoveryFile = "discovery.json"
	hiveListingFile   = "listing.jsonl"
	hiveResultsDir    = "results"
)

// Hive website URL fragments used to build deep links into the UI.
const (
	hiveGroupURLFormat = "%s/#/group/%s"
	hiveTestURLFormat  = "%s/#/test/%s/%s"
)

// hiveKnownClients are the canonical client names Hive listings expose,
// often suffixed (e.g. `go-ethereum_main`). The normalizer matches a
// `<client>_*` prefix against this list, and `hive-logs clients` prints it.
// Add new entries here when Hive starts publishing a new client.
var hiveKnownClients = []string{
	"besu",
	"erigon",
	"ethrex",
	"go-ethereum",
	"nethermind",
	"nimbus-el",
	"reth",
}

// hiveClientAliases maps short or alternate client names that appear in
// Hive output back to their canonical form.
var hiveClientAliases = map[string]string{
	"geth":     "go-ethereum",
	"nimbusel": "nimbus-el",
}
