// Command shh reads shell history files and reports on them.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
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
  top    show the most frequently run commands
  stats  show summary statistics for a history file

flags (top):
  -n int      number of commands to show (default 10)
  --json      output as JSON instead of a table

flags (stats):
  --json      output as JSON instead of plain text

if no file is given, shh reads $HISTFILE, falling back to
~/.zsh_history or ~/.bash_history, whichever exists first.`)
}

func runTop(args []string) error {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	n := fs.Int("n", 10, "number of commands to show")
	jsonOut := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := loadEntries(fs.Args())
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
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := loadEntries(fs.Args())
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
		entries, err := history.Parse(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", p, err)
		}
		all = append(all, entries...)
	}
	return all, nil
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
	return "", fmt.Errorf("no history file found; set $HISTFILE or pass a path")
}
