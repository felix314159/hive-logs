---
name: hive-logs
description: Explains how to use `hive-logs` to fetch logs of failed hive tests.
---

Ethpandaops team uses [hive](https://github.com/ethereum/hive) to start
different clients and run various tests from different test suites against them.
The results are published and `hive-logs` is the tool you can use to fetch
logs of failing tests. The users of this tool are devs of a specific client,
and they are usually interested in the failures of the lastest run of 
a specific suite. Ethpandaops publishes results in a format where different
groups contain different test suites, each suite involves various clients.

## Usage

To get an overview of which groups, suites and clients exist run:
`go run . list`

To see an overview of passed and failed tests of a specific suite run:
`go run . group=<group> suite=<suite>`

To list and download the logs of failing tests for a specific client run:
`go run . group=<group> suite=<suite> client=<client>`

You can assume that the user does not care about passing tests and will
use this tool to investigate failing tests. If it is unclear which test results
the user is interested in (e.g. they mention a specific test name but you do
not know which simulator contains that test) just ask the user for clarification.
Some suites (currently `consume-rlp` and `consume-engine`) exist under the 
same name for different groups, you should ask the user for group clarification
if the same client has failures for those duplicate-name suites.

After using `hive-logs` to collect the relevant logs you will have access to 
relevant context for debugging the cause of the test failure (e.g. hive
version and logs from test run, client branch and commit and client logs
from test run, in some cases also EELS branch and fixture release). You should
offer to investigate the issue further, you will need access to the client
source code so if you can't find it ask the user for the local path.