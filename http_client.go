// Package main wraps HTTP access to the Hive results origin, including byte-range log retrieval.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

func (c *Client) getRange(ctx context.Context, path string, begin, end int64) ([]byte, error) {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if begin >= 0 && end > begin {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", begin, end-1))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}

	const maxLogBytes = 200 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK && begin >= 0 && end > begin {
		if end <= int64(len(data)) {
			return data[begin:end], nil
		}
		return nil, fmt.Errorf("server ignored range and log is too small for [%d,%d)", begin, end)
	}
	return data, nil
}
