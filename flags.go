// Package main declares shared command flags and parses the flexible groups command arguments.
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

type groupsFlags struct {
	baseURL   string
	all       bool
	showFiles bool
	limit     int
	json      bool
}

func parseGroupsArgs(args []string) (groupsFlags, []string, error) {
	gf := groupsFlags{
		baseURL: defaultBaseURL,
		limit:   0,
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "--") {
			rest = append(rest, arg)
			continue
		}

		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch name {
		case "base-url":
			if !hasValue {
				i++
				if i >= len(args) {
					return gf, nil, errors.New("--base-url requires a value")
				}
				value = args[i]
			}
			gf.baseURL = value
		case "limit":
			if !hasValue {
				i++
				if i >= len(args) {
					return gf, nil, errors.New("--limit requires a value")
				}
				value = args[i]
			}
			limit, err := strconv.Atoi(value)
			if err != nil {
				return gf, nil, fmt.Errorf("invalid --limit %q: %w", value, err)
			}
			gf.limit = limit
		case "all":
			if hasValue {
				v, err := strconv.ParseBool(value)
				if err != nil {
					return gf, nil, fmt.Errorf("invalid --all %q: %w", value, err)
				}
				gf.all = v
				continue
			}
			gf.all = true
		case "files":
			if hasValue {
				v, err := strconv.ParseBool(value)
				if err != nil {
					return gf, nil, fmt.Errorf("invalid --files %q: %w", value, err)
				}
				gf.showFiles = v
				continue
			}
			gf.showFiles = true
		case "json":
			if hasValue {
				v, err := strconv.ParseBool(value)
				if err != nil {
					return gf, nil, fmt.Errorf("invalid --json %q: %w", value, err)
				}
				gf.json = v
				continue
			}
			gf.json = true
		default:
			return gf, nil, fmt.Errorf("unknown groups flag --%s", name)
		}
	}
	return gf, rest, nil
}

type fetchFlags struct {
	common     commonFlags
	outDir     string
	fullClient bool
}
