package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientTrimsBaseURL(t *testing.T) {
	c := newClient("https://example.test///")
	if c.baseURL != "https://example.test" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
}

func TestClientGetFetchesCleanPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/group/listing.jsonl" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	data, err := newClient(server.URL+"/").get(context.Background(), "/group/listing.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Fatalf("data = %q", data)
	}
}

func TestClientGetReportsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := newClient(server.URL).get(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientGetRangeSendsRangeHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Range"), "bytes=2-5"; got != want {
			t.Fatalf("Range = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("cdef"))
	}))
	defer server.Close()

	data, err := newClient(server.URL).getRange(context.Background(), "log", 2, 6)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cdef" {
		t.Fatalf("data = %q", data)
	}
}

func TestClientGetRangeSlicesWhenServerIgnoresRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("abcdef"))
	}))
	defer server.Close()

	data, err := newClient(server.URL).getRange(context.Background(), "log", 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "bcd" {
		t.Fatalf("data = %q", data)
	}
}

func TestClientGetRangeErrorsWhenIgnoredRangeTooSmall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("abc"))
	}))
	defer server.Close()

	_, err := newClient(server.URL).getRange(context.Background(), "log", 1, 8)
	if err == nil || !strings.Contains(err.Error(), "server ignored range") {
		t.Fatalf("err = %v", err)
	}
}
