package main

import (
	"encoding/json"
	"testing"
)

func TestSuiteResultJSONModelsDecodeHiveShape(t *testing.T) {
	data := []byte(`{
		"id": 7,
		"name": "eels/consume-engine",
		"clientVersions": {"go-ethereum_main": "Geth/v1.2.3/linux"},
		"testDetailsLog": "details.log",
		"runMetadata": {
			"hiveCommand": ["hive", "--sim", "eels"],
			"hiveVersion": {"commit": "abcdef123456", "branch": "main", "dirty": true},
			"clientConfig": {"filePath": "clients.yaml", "content": {"clients": [
				{"client": "go-ethereum", "nametag": "main", "dockerfile": "Dockerfile", "build_args": {"tag": "master"}}
			]}}
		},
		"testCases": {
			"1": {
				"name": "test-a",
				"summaryResult": {"pass": false, "timeout": true, "details": "boom", "log": {"begin": 2, "end": 5}},
				"clientInfo": {"0": {"id": "0", "name": "geth", "logFile": "client.log", "logOffsets": {"begin": 4, "end": 8}}}
			}
		}
	}`)

	var suite SuiteResult
	if err := json.Unmarshal(data, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.ID != 7 || suite.Name != "eels/consume-engine" || suite.TestDetailsLog != "details.log" {
		t.Fatalf("suite header = %+v", suite)
	}
	if suite.RunMetadata == nil || suite.RunMetadata.HiveVersion.Commit != "abcdef123456" {
		t.Fatalf("run metadata = %+v", suite.RunMetadata)
	}
	tc := suite.TestCases["1"]
	if tc.SummaryResult.Pass || !tc.SummaryResult.Timeout || tc.SummaryResult.Log.Begin != 2 {
		t.Fatalf("test case = %+v", tc)
	}
	if tc.ClientInfo["0"].LogOffsets.End != 8 {
		t.Fatalf("client info = %+v", tc.ClientInfo["0"])
	}
}
