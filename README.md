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

List groups:

```sh
go run . groups
```

List simulators (suites) available in a group, with run counts and the latest
run timestamp:

```sh
go run . groups generic
```

Show per-client pass/fail counts for the latest run of a simulator:

```sh
go run . groups generic engine-api
```

Find recent runs for a suite/client:

```sh
go run . runs --group generic --suite eels/consume-engine --client go-ethereum
```

List failing tests in the latest matching run. `--test` is a case-insensitive
substring by default, so full pytest ids can be pasted without escaping regex
characters. Add `--regex` when you want regex matching.

```sh
go run . list --group generic --suite eels/consume-engine --client go-ethereum --test 7702
```

Fetch the first matching failure bundle:

```sh
go run . fetch --group generic --suite eels/consume-engine --client go-ethereum --test 'some_test_name' --out logs
```

Fetch all matching failures:

```sh
go run . fetch --group generic --suite eels/consume-engine --client go-ethereum --test 7702 --limit 0
```

Each bundle contains:

- `summary.json`: run metadata, Hive command, versions, test id, and log offsets
- `hive.log`: Hive/simulator log slice for that test
- `client.log`: client log slice(s) for that test
- `reproduce_commands.md`: reproduction context and the Hive command

---

Imagine client `go-ethereum` fails the simulator `consume-engine` for test
`tests/paris/eip7610_create_collision/test_initcollision.py::test_init_collision_create_tx[fork_Cancun-tx_type_0-blockchain_test_engine_from_state_test-non-empty-balance-correct-initcode]`, so now to fetch the relevant logs (hive + client logs) run:

```go
go run . fetch --group generic --suite eels/consume-engine --client go-ethereum --test 'tests/paris/eip7610_create_collision/test_initcollision.py::test_init_collision_create_tx[fork_Cancun-tx_type_0-blockchain_test_engine_from_state_test-non-empty-balance-correct-initcode]'
```
