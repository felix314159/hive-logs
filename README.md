# hive-logs

`hive-logs` is a direct CLI for the public EthPandaOps Hive result site.
It finds logs for a result group, suite, and client, then fetches the two
log sources needed for local LLM debugging:

- `hive.log`: the per-test Hive/simulator detail log range
- `client.log`: the per-test client log range(s)

The tool uses the static layout served by `https://hive.ethpandaops.io`:
`discovery.json`, `<group>/listing.jsonl`, `<group>/results/<suite>.json`, and
range requests for log files.

## Build

```sh
go build ./...
```

## Examples

Print version:

```sh
go run . --version
```

List groups, suites, and known clients:

```sh
go run . list
```

Show the latest run for each suite/client in a group:

```sh
go run . group=generic
```

Add `--all` when you want to include older runs, then add `--files` to print
file names. Add `--limit N` when you want to cap the number of rows printed.

Show per-client pass/fail counts for the latest run of a simulator:

```sh
go run . group=generic suite=engine-api
```

List failing tests for a client in the latest matching run and fetch all failure
bundles into `./logs`:

```sh
go run . group=generic suite=eels/consume-engine client=go-ethereum
```

Each bundle contains:

- `summary.json`: run metadata, Hive command, versions, test id, and log offsets
- `hive.log`: Hive/simulator log slice for that test
- `client.log`: client log slice(s) for that test
- `reproduce_commands.md`: reproduction context and the Hive command
