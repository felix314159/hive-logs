// Package main wraps HTTP access to the Hive results origin, including byte-range log retrieval.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func newClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// getSuiteHeader streams the suite JSON only as far as the testCases field
// and then aborts the response. In Hive's serialization testCases is last
// and by far the largest field, so for callers that only need run-level
// metadata this trims the work from hundreds of MB to a few KB.
func (c *Client) getSuiteHeader(ctx context.Context, path string) (*SuiteResult, error) {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", u, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("decode %s: expected JSON object, got %v", u, tok)
	}

	var suite SuiteResult
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", u, err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("decode %s: expected key, got %v", u, keyTok)
		}
		if key == "testCases" {
			// Hit the heavy field — we have everything we need.
			return &suite, nil
		}
		if err := decodeSuiteField(dec, key, &suite); err != nil {
			return nil, fmt.Errorf("decode %s: %w", u, err)
		}
	}
	return &suite, nil
}

// decodeSuiteField decodes a single top-level suite field by name. Unknown
// keys are skipped via json.RawMessage so the streaming loop keeps moving
// even if Hive adds new fields later.
func decodeSuiteField(dec *json.Decoder, key string, suite *SuiteResult) error {
	switch key {
	case "id":
		return dec.Decode(&suite.ID)
	case "name":
		return dec.Decode(&suite.Name)
	case "description":
		return dec.Decode(&suite.Description)
	case "clientVersions":
		return dec.Decode(&suite.ClientVersions)
	case "simLog":
		return dec.Decode(&suite.SimLog)
	case "testDetailsLog":
		return dec.Decode(&suite.TestDetailsLog)
	case "runMetadata":
		return dec.Decode(&suite.RunMetadata)
	default:
		var raw json.RawMessage
		return dec.Decode(&raw)
	}
}

// getJSONStream issues a GET and decodes the response body into v as bytes
// arrive, so network transfer and JSON parsing overlap. This is meaningfully
// faster than `get` + `json.Unmarshal` for the multi-megabyte suite results
// (e.g. eels/consume-engine, ~40k test cases) we hit in listSuiteClients.
func (c *Client) getJSONStream(ctx context.Context, path string, v any) error {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode %s: %w", u, err)
	}
	return nil
}

func (c *Client) getRange(ctx context.Context, path string, begin, end int64) ([]byte, error) {
	data, _, err := c.getRangeWithSize(ctx, path, begin, end)
	return data, err
}

// getRangeWithSize behaves like getRange but additionally reports the size of
// the underlying file when the server makes it discoverable. Total size comes
// from the Content-Range header on a 206 response (e.g. `bytes 0-99/12345`)
// or from the body length of a full 200 response. When the size cannot be
// determined, totalSize is -1.
func (c *Client) getRangeWithSize(ctx context.Context, path string, begin, end int64) ([]byte, int64, error) {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, -1, err
	}
	if begin >= 0 && end > begin {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", begin, end-1))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, -1, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, -1, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}

	const maxLogBytes = 200 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes))
	if err != nil {
		return nil, -1, err
	}
	totalSize := parseContentRangeTotal(resp.Header.Get("Content-Range"))
	if resp.StatusCode == http.StatusOK && begin >= 0 && end > begin {
		if end <= int64(len(data)) {
			return data[begin:end], int64(len(data)), nil
		}
		return nil, int64(len(data)), fmt.Errorf("server ignored range and log is too small for [%d,%d)", begin, end)
	}
	if totalSize < 0 && resp.StatusCode == http.StatusOK {
		totalSize = int64(len(data))
	}
	return data, totalSize, nil
}

// parseContentRangeTotal extracts the total file size from a Content-Range
// header value like `bytes 0-499/1234`. Returns -1 when the size is missing,
// unknown ("*"), or unparseable.
func parseContentRangeTotal(value string) int64 {
	idx := strings.LastIndex(value, "/")
	if idx < 0 {
		return -1
	}
	size, err := strconv.ParseInt(strings.TrimSpace(value[idx+1:]), 10, 64)
	if err != nil {
		return -1
	}
	return size
}
