package history

import (
	"strings"
	"testing"
	"time"
)

func TestParsePlainBash(t *testing.T) {
	input := "ls -la\ncd /tmp\ngit status\n"
	entries, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Command != "ls -la" {
		t.Errorf("got command %q, want %q", entries[0].Command, "ls -la")
	}
	if !entries[0].Time.IsZero() {
		t.Errorf("expected no timestamp for plain bash entry, got %v", entries[0].Time)
	}
}

func TestParseBashWithTimestamps(t *testing.T) {
	input := "#1690000000\ngit status\n#1690000100\nls -la\n"
	entries, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	want := time.Unix(1690000000, 0)
	if !entries[0].Time.Equal(want) {
		t.Errorf("got time %v, want %v", entries[0].Time, want)
	}
	if entries[1].Command != "ls -la" {
		t.Errorf("got command %q, want %q", entries[1].Command, "ls -la")
	}
}

func TestParseZshExtended(t *testing.T) {
	input := ": 1690000000:0;git status\n: 1690000100:2;ls -la\n"
	entries, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Command != "git status" {
		t.Errorf("got command %q, want %q", entries[0].Command, "git status")
	}
	want := time.Unix(1690000100, 0)
	if !entries[1].Time.Equal(want) {
		t.Errorf("got time %v, want %v", entries[1].Time, want)
	}
}

func TestParseZshContinuation(t *testing.T) {
	input := ": 1690000000:0;echo one \\\necho two\n"
	entries, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	want := "echo one \necho two"
	if entries[0].Command != want {
		t.Errorf("got command %q, want %q", entries[0].Command, want)
	}
}

func TestTopCommands(t *testing.T) {
	entries := []Entry{
		{Command: "git status"},
		{Command: "ls"},
		{Command: "git status"},
		{Command: "ls"},
		{Command: "git status"},
	}
	top := TopCommands(entries, 1)
	if len(top) != 1 {
		t.Fatalf("got %d results, want 1", len(top))
	}
	if top[0].Command != "git status" || top[0].Count != 3 {
		t.Errorf("got %+v, want {git status 3}", top[0])
	}
}

func TestSearchSubstring(t *testing.T) {
	entries := []Entry{
		{Command: "git status"},
		{Command: "ls -la"},
		{Command: "git diff HEAD"},
	}
	matches, err := Search(entries, "GIT", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	if matches[0].Command != "git status" || matches[1].Command != "git diff HEAD" {
		t.Errorf("got %+v", matches)
	}
}

func TestSearchRegex(t *testing.T) {
	entries := []Entry{
		{Command: "git status"},
		{Command: "git commit -m fix"},
		{Command: "ls -la"},
	}
	matches, err := Search(entries, `^git (status|commit)`, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
}

func TestSearchRegexInvalid(t *testing.T) {
	_, err := Search(nil, "(unclosed", true)
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
}

func TestFilterByTimeNoBounds(t *testing.T) {
	entries := []Entry{{Command: "a"}, {Command: "b", Time: time.Unix(100, 0)}}
	got := FilterByTime(entries, time.Time{}, time.Time{})
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
}

func TestFilterByTimeRange(t *testing.T) {
	entries := []Entry{
		{Command: "no-timestamp"},
		{Command: "too-early", Time: time.Unix(100, 0)},
		{Command: "in-range", Time: time.Unix(200, 0)},
		{Command: "too-late", Time: time.Unix(300, 0)},
	}
	got := FilterByTime(entries, time.Unix(150, 0), time.Unix(250, 0))
	if len(got) != 1 || got[0].Command != "in-range" {
		t.Fatalf("got %+v, want only in-range", got)
	}
}

func TestFilterByTimeSinceOnly(t *testing.T) {
	entries := []Entry{
		{Command: "too-early", Time: time.Unix(100, 0)},
		{Command: "in-range", Time: time.Unix(200, 0)},
	}
	got := FilterByTime(entries, time.Unix(150, 0), time.Time{})
	if len(got) != 1 || got[0].Command != "in-range" {
		t.Fatalf("got %+v, want only in-range", got)
	}
}

func TestFilterByTimeUntilOnly(t *testing.T) {
	entries := []Entry{
		{Command: "in-range", Time: time.Unix(100, 0)},
		{Command: "too-late", Time: time.Unix(200, 0)},
	}
	got := FilterByTime(entries, time.Time{}, time.Unix(150, 0))
	if len(got) != 1 || got[0].Command != "in-range" {
		t.Fatalf("got %+v, want only in-range", got)
	}
}

func TestSearchNoMatches(t *testing.T) {
	entries := []Entry{{Command: "ls -la"}}
	matches, err := Search(entries, "nonexistent", false)
	if err != nil {
		t.Fatal(err)
	}
	if matches != nil {
		t.Errorf("got %+v, want nil", matches)
	}
}
