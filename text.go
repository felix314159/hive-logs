// Package main contains general text, path, HTML cleanup, and shell quoting helpers used across hive-logs.
package main

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

func firstLine(s string) string {
	s = strings.TrimRight(s, "\r\n")
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func simulatorName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func pathJoin(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			cleaned = append(cleaned, strings.Trim(part, "/"))
		}
	}
	return strings.Join(cleaned, "/")
}

func cleanDescription(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	tagRe := regexp.MustCompile(`<[^>]+>`)
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func shellJoin(args []string) string {
	var out []string
	for _, arg := range args {
		if arg == "" {
			out = append(out, "''")
			continue
		}
		if strings.IndexFunc(arg, func(r rune) bool {
			return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '=' || r == ':' || r == ',' || r == '+' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
		}) == -1 {
			out = append(out, arg)
			continue
		}
		out = append(out, strconv.Quote(arg))
	}
	return strings.Join(out, " ")
}
