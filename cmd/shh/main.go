// Command shh reads shell history files and reports on them.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/matthewh988/shell-history-stats/history"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "top":
		err = runTop(os.Args[2:])
	case "stats":
		err = runStats(os.Args[2:])
	case "search":
		err = runSearch(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "shh: unknown command %q\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "shh: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: shh <command> [flags] [file...]

commands:
  top     show the most frequently run commands
  stats   show summary statistics for a history file
  search  find commands matching a substring or regex

flags (top):
  -n int      number of commands to show (default 10)
  --json      output as JSON instead of a table
  --since     only include commands at or after this time
  --until     only include commands at or before this time

flags (stats):
  --json      output as JSON instead of plain text
  --since     only include commands at or after this time
  --until     only include commands at or before this time

flags (search):
  --regex     treat the query as a regular expression instead of a
              case-insensitive substring
  --json      output as JSON instead of plain text
  --since     only include commands at or after this time
  --until     only include commands at or before this time

usage (search):
  shh search [flags] <query> [file...]

--since and --until accept RFC3339 ("2026-01-02T15:04:05-05:00"),
"2026-01-02 15:04:05", "2026-01-02", or a duration ("36h") meaning that
long ago from now. Entries with no timestamp are excluded whenever
either flag is set, since there's no way to know where they fall.

if no file is given, shh reads $HISTFILE, falling back to
~/.zsh_history, ~/.bash_history, or fish's fish_history, whichever
exists first. bash, zsh, and fish history files are all detected
automatically; you don't need to say which one you're pointing at.`)
}

func runTop(args []string) error {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	n := fs.Int("n", 10, "number of commands to show")
	jsonOut := fs.Bool("json", false, "output as JSON")
	since := fs.String("since", "", "only include commands at or after this time")
	until := fs.String("until", "", "only include commands at or before this time")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := loadEntries(fs.Args())
	if err != nil {
		return err
	}
	entries, err = filterByTimeFlags(entries, *since, *until)
	if err != nil {
		return err
	}

	top := history.TopCommands(entries, *n)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(top)
	}

	for _, c := range top {
		fmt.Printf("%6d  %s\n", c.Count, c.Command)
	}
	return nil
}

type statsOutput struct {
	TotalEntries   int    `json:"total_entries"`
	UniqueCommands int    `json:"unique_commands"`
	OldestTime     string `json:"oldest_time,omitempty"`
	NewestTime     string `json:"newest_time,omitempty"`
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	since := fs.String("since", "", "only include commands at or after this time")
	until := fs.String("until", "", "only include commands at or before this time")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := loadEntries(fs.Args())
	if err != nil {
		return err
	}
	entries, err = filterByTimeFlags(entries, *since, *until)
	if err != nil {
		return err
	}

	out := statsOutput{TotalEntries: len(entries)}
	seen := make(map[string]struct{}, len(entries))
	var oldest, newest time.Time
	for _, e := range entries {
		seen[e.Command] = struct{}{}
		if e.Time.IsZero() {
			continue
		}
		if oldest.IsZero() || e.Time.Before(oldest) {
			oldest = e.Time
		}
		if newest.IsZero() || e.Time.After(newest) {
			newest = e.Time
		}
	}
	out.UniqueCommands = len(seen)
	if !oldest.IsZero() {
		out.OldestTime = oldest.Format(time.RFC3339)
		out.NewestTime = newest.Format(time.RFC3339)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Printf("entries:          %d\n", out.TotalEntries)
	fmt.Printf("unique commands:  %d\n", out.UniqueCommands)
	if out.OldestTime != "" {
		fmt.Printf("oldest:           %s\n", out.OldestTime)
		fmt.Printf("newest:           %s\n", out.NewestTime)
	} else {
		fmt.Println("no timestamps found in history file(s)")
	}
	return nil
}

type searchResult struct {
	Command string `json:"command"`
	Time    string `json:"time,omitempty"`
	Line    int    `json:"line"`
}

func runSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	useRegex := fs.Bool("regex", false, "treat the query as a regular expression")
	jsonOut := fs.Bool("json", false, "output as JSON")
	since := fs.String("since", "", "only include commands at or after this time")
	until := fs.String("until", "", "only include commands at or before this time")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("search requires a query, e.g. shh search git")
	}
	query, paths := rest[0], rest[1:]

	entries, err := loadEntries(paths)
	if err != nil {
		return err
	}
	entries, err = filterByTimeFlags(entries, *since, *until)
	if err != nil {
		return err
	}

	matches, err := history.Search(entries, query, *useRegex)
	if err != nil {
		return fmt.Errorf("invalid query: %w", err)
	}

	if *jsonOut {
		results := make([]searchResult, len(matches))
		for i, m := range matches {
			r := searchResult{Command: m.Command, Line: m.Line}
			if !m.Time.IsZero() {
				r.Time = m.Time.Format(time.RFC3339)
			}
			results[i] = r
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	for _, m := range matches {
		if m.Time.IsZero() {
			fmt.Printf("%6d  %s\n", m.Line, m.Command)
		} else {
			fmt.Printf("%6d  %s  %s\n", m.Line, m.Time.Format(time.RFC3339), m.Command)
		}
	}
	return nil
}

func loadEntries(paths []string) ([]history.Entry, error) {
	if len(paths) == 0 {
		p, err := defaultHistoryFile()
		if err != nil {
			return nil, err
		}
		paths = []string{p}
	}

	var all []history.Entry
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		entries, err := parseHistoryFile(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", p, err)
		}
		all = append(all, entries...)
	}
	return all, nil
}

const fishPrefix = "- cmd: "

// parseHistoryFile picks bash/zsh or fish parsing based on the file's first
// line: fish history entries start with "- cmd: ", which neither bash nor
// zsh ever write.
func parseHistoryFile(f *os.File) ([]history.Entry, error) {
	br := bufio.NewReader(f)
	prefix, err := br.Peek(len(fishPrefix))
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, err
	}
	if string(prefix) == fishPrefix {
		return history.ParseFish(br)
	}
	return history.Parse(br)
}

// filterByTimeFlags applies --since/--until to entries. Either string may be
// empty, meaning that bound is unset.
func filterByTimeFlags(entries []history.Entry, since, until string) ([]history.Entry, error) {
	if since == "" && until == "" {
		return entries, nil
	}
	var sinceTime, untilTime time.Time
	var err error
	if since != "" {
		if sinceTime, err = parseTimeArg(since); err != nil {
			return nil, fmt.Errorf("--since: %w", err)
		}
	}
	if until != "" {
		if untilTime, err = parseTimeArg(until); err != nil {
			return nil, fmt.Errorf("--until: %w", err)
		}
	}
	return history.FilterByTime(entries, sinceTime, untilTime), nil
}

// timeLayouts are the absolute formats accepted by --since/--until, tried in
// order.
var timeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// parseTimeArg parses a --since/--until value. It accepts a duration such as
// "36h", taken as that far before now, or one of timeLayouts interpreted in
// the local timezone.
func parseTimeArg(s string) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q as a time or duration", s)
}

func defaultHistoryFile() (string, error) {
	if f := os.Getenv("HISTFILE"); f != "" {
		return f, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, name := range []string{".zsh_history", ".bash_history"} {
		candidate := filepath.Join(home, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if candidate := fishHistoryFile(home); candidate != "" {
		return candidate, nil
	}
	return "", fmt.Errorf("no history file found; set $HISTFILE or pass a path")
}

// fishHistoryFile returns fish's default history path if it exists. fish
// keeps it under $XDG_DATA_HOME (or ~/.local/share) rather than $HOME
// directly.
func fishHistoryFile(home string) string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	candidate := filepath.Join(dataHome, "fish", "fish_history")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
