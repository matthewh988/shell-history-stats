// Package history parses shell history files into a common representation.
//
// bash and zsh disagree on the format. Plain bash history is one command
// per line. With HISTTIMEFORMAT set, bash prepends a "#<unix-seconds>"
// comment line before each command. zsh's "extended history" format
// (EXTENDED_HISTORY) writes each entry as ": <start>:<elapsed>;<command>"
// on a single line, with long commands continued across multiple lines
// using a trailing backslash. This package understands all three.
package history

import (
	"bufio"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is a single shell history record. Time is the zero value when the
// source line carried no timestamp.
type Entry struct {
	Command string
	Time    time.Time
	Line    int
}

// Parse reads a history file and returns its entries in file order.
func Parse(r io.Reader) ([]Entry, error) {
	scanner := bufio.NewScanner(r)
	// history files can contain very long single lines (heredocs, long
	// pipelines); grow the buffer well past the default 64KiB token limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var entries []Entry
	var pendingTime time.Time
	var hasPendingTime bool
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" {
			continue
		}

		if ts, ok := parseBashTimestampComment(line); ok {
			pendingTime = ts
			hasPendingTime = true
			continue
		}

		cmd := line
		var entryTime time.Time

		if meta, ok := parseZshMeta(line); ok {
			cmd = meta.command
			entryTime = meta.when
		} else if hasPendingTime {
			entryTime = pendingTime
			hasPendingTime = false
		}

		for strings.HasSuffix(cmd, "\\") && scanner.Scan() {
			lineNo++
			cmd = strings.TrimSuffix(cmd, "\\") + "\n" + scanner.Text()
		}

		entries = append(entries, Entry{Command: cmd, Time: entryTime, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// parseBashTimestampComment recognizes a bash HISTTIMEFORMAT line, which is
// a comment holding nothing but a unix timestamp: "#1690000000".
func parseBashTimestampComment(line string) (time.Time, bool) {
	if len(line) < 2 || line[0] != '#' || !isAllDigits(line[1:]) {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(line[1:], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

type zshMeta struct {
	command string
	when    time.Time
}

// parseZshMeta recognizes zsh EXTENDED_HISTORY lines:
// ": <start-unix-seconds>:<elapsed-seconds>;<command>"
func parseZshMeta(line string) (zshMeta, bool) {
	rest := strings.TrimPrefix(line, ": ")
	if rest == line {
		return zshMeta{}, false
	}
	semi := strings.Index(rest, ";")
	if semi < 0 {
		return zshMeta{}, false
	}
	meta := rest[:semi]
	m := zshMeta{command: rest[semi+1:]}
	colon := strings.Index(meta, ":")
	tsField := meta
	if colon >= 0 {
		tsField = meta[:colon]
	}
	if secs, err := strconv.ParseInt(strings.TrimSpace(tsField), 10, 64); err == nil {
		m.when = time.Unix(secs, 0)
	}
	return m, true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ParseFish reads a fish shell history file and returns its entries in file
// order. fish writes history as a sequence of blocks that look like YAML but
// aren't parsed as such by fish itself, so this doesn't use a YAML parser
// either:
//
//	- cmd: git status
//	  when: 1690000000
//	- cmd: git push
//	  when: 1690000100
//	  paths:
//	    - some/file
//
// A command's embedded backslashes and newlines are escaped as "\\\\" and
// "\\n" rather than spanning multiple lines, so unlike Parse there's no
// continuation handling here: every entry is exactly one line in the file.
// The optional "paths" field (files fish thinks the command touched) is
// ignored; its list lines don't match any prefix this function looks for, so
// they're skipped without special-casing.
func ParseFish(r io.Reader) ([]Entry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var entries []Entry
	var current *Entry
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		if cmd, ok := strings.CutPrefix(line, "- cmd: "); ok {
			entries = append(entries, Entry{Command: unescapeFishCommand(cmd), Line: lineNo})
			current = &entries[len(entries)-1]
			continue
		}
		if when, ok := strings.CutPrefix(line, "  when: "); ok && current != nil {
			if secs, err := strconv.ParseInt(strings.TrimSpace(when), 10, 64); err == nil {
				current.Time = time.Unix(secs, 0)
			}
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// unescapeFishCommand reverses fish's history escaping, where a literal
// backslash is written as "\\" and a newline as "\n".
func unescapeFishCommand(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// CommandCount is one command paired with how many times it occurs.
type CommandCount struct {
	Command string `json:"command"`
	Count   int    `json:"count"`
}

// CountCommands tallies exact-match occurrences of each command line.
func CountCommands(entries []Entry) map[string]int {
	counts := make(map[string]int, len(entries))
	for _, e := range entries {
		counts[e.Command]++
	}
	return counts
}

// TopCommands returns the n most frequent commands, most frequent first.
// Ties break alphabetically so output is stable across runs. n <= 0 returns
// every command.
func TopCommands(entries []Entry, n int) []CommandCount {
	counts := CountCommands(entries)
	result := make([]CommandCount, 0, len(counts))
	for cmd, c := range counts {
		result = append(result, CommandCount{Command: cmd, Count: c})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Command < result[j].Command
	})
	if n > 0 && n < len(result) {
		result = result[:n]
	}
	return result
}

// FilterByTime returns the entries whose Time falls within [since, until],
// in file order. A zero since or until leaves that bound unset. Entries with
// no timestamp are dropped whenever either bound is set, since there's no
// way to know whether an untimestamped entry belongs in the range.
func FilterByTime(entries []Entry, since, until time.Time) []Entry {
	if since.IsZero() && until.IsZero() {
		return entries
	}
	var out []Entry
	for _, e := range entries {
		if e.Time.IsZero() {
			continue
		}
		if !since.IsZero() && e.Time.Before(since) {
			continue
		}
		if !until.IsZero() && e.Time.After(until) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Search returns the entries whose Command matches query, in file order.
// query is matched as a case-insensitive substring unless useRegex is set,
// in which case it is compiled as a Go regular expression and matched
// against the command as-is (regex matching is case-sensitive unless the
// pattern itself opts into "(?i)").
func Search(entries []Entry, query string, useRegex bool) ([]Entry, error) {
	if useRegex {
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, err
		}
		var matches []Entry
		for _, e := range entries {
			if re.MatchString(e.Command) {
				matches = append(matches, e)
			}
		}
		return matches, nil
	}

	q := strings.ToLower(query)
	var matches []Entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Command), q) {
			matches = append(matches, e)
		}
	}
	return matches, nil
}
