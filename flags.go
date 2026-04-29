// Package main declares shared command flags and parses the key=value query arguments.
package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type commonFlags struct {
	group  string
	suite  string
	client string
}

type queryFlags struct {
	common       commonFlags
	baseURL      string
	all          bool
	showFiles    bool
	showLogPaths bool
	limit        int
	json         bool
	withDuration bool
}

// parseQueryArgs parses the key=value query syntax (group=, suite=, client=)
// and the supporting flags (--base-url, --all, --files, --limit, --json).
// Positional args without a recognised key are rejected.
func parseQueryArgs(args []string) (queryFlags, error) {
	qf := queryFlags{
		baseURL: defaultBaseURL,
		limit:   0,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if !strings.HasPrefix(arg, "--") {
			name, value, ok := strings.Cut(arg, "=")
			if !ok {
				return qf, missingKeyError(arg, qf.common)
			}
			switch name {
			case "group":
				qf.common.group = value
			case "suite":
				qf.common.suite = value
			case "client":
				qf.common.client = value
			default:
				return qf, fmt.Errorf("unknown query key %q (expected group=, suite=, or client=)", name)
			}
			continue
		}

		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch name {
		case "base-url":
			if !hasValue {
				i++
				if i >= len(args) {
					return qf, errors.New("--base-url requires a value")
				}
				value = args[i]
			}
			qf.baseURL = value
		case "limit":
			if !hasValue {
				i++
				if i >= len(args) {
					return qf, errors.New("--limit requires a value")
				}
				value = args[i]
			}
			limit, err := strconv.Atoi(value)
			if err != nil {
				return qf, fmt.Errorf("invalid --limit %q: %w", value, err)
			}
			qf.limit = limit
		case "all":
			v, err := parseBoolFlag(name, value, hasValue)
			if err != nil {
				return qf, err
			}
			qf.all = v
		case "files":
			v, err := parseBoolFlag(name, value, hasValue)
			if err != nil {
				return qf, err
			}
			qf.showFiles = v
		case "show-log-paths":
			v, err := parseBoolFlag(name, value, hasValue)
			if err != nil {
				return qf, err
			}
			qf.showLogPaths = v
		case "json":
			v, err := parseBoolFlag(name, value, hasValue)
			if err != nil {
				return qf, err
			}
			qf.json = v
		case "duration":
			v, err := parseBoolFlag(name, value, hasValue)
			if err != nil {
				return qf, err
			}
			qf.withDuration = v
		default:
			return qf, fmt.Errorf("unknown flag --%s", name)
		}
	}
	return qf, nil
}

func parseBoolFlag(name, value string, hasValue bool) (bool, error) {
	if !hasValue {
		return true, nil
	}
	v, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid --%s %q: %w", name, value, err)
	}
	return v, nil
}

// missingKeyError builds a hint based on which query keys have already been
// parsed: an unprefixed positional argument is most likely the next key the
// user forgot to label.
func missingKeyError(arg string, c commonFlags) error {
	switch {
	case c.group == "":
		return fmt.Errorf("missing key for %q: did you mean group=%s?", arg, arg)
	case c.suite == "":
		return fmt.Errorf("missing key for %q, did you mean suite=%s?", arg, arg)
	case c.client == "":
		return fmt.Errorf("missing key for %q, did you mean client=%s?", arg, arg)
	default:
		return fmt.Errorf("unexpected positional argument %q: group=, suite=, and client= are already set", arg)
	}
}

type fetchFlags struct {
	common     commonFlags
	outDir     string
	fullClient bool
}
