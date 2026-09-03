# shell-history-stats

Your shell has been logging every command you've ever run. bash and zsh
disagree on how, though: plain bash history is one line per command with no
timestamp unless `HISTTIMEFORMAT` is set (in which case it writes a
`#<unix-seconds>` comment before each line), and zsh's extended history
format packs the timestamp and elapsed time into the line itself
(`: 1690000000:0;git status`), with long commands continued across multiple
lines using a trailing backslash.

`shh` parses either format into one common structure and answers the
questions the raw file can't: what do I actually run all day, and how many
distinct commands are in this file.

## Install

```
go install github.com/matthewh988/shell-history-stats/cmd/shh@latest
```

or build from a clone:

```
go build -o shh ./cmd/shh
```

## Usage

By default `shh` reads `$HISTFILE`, falling back to `~/.zsh_history` then
`~/.bash_history`, whichever exists first. You can also point it at a file
directly.

```
$ shh top -n 5
    142  git status
     98  ls
     71  cd ..
     40  git diff
     33  npm run build

$ shh top -n 3 --json
[
  {
    "command": "git status",
    "count": 142
  },
  {
    "command": "ls",
    "count": 98
  },
  {
    "command": "cd ..",
    "count": 71
  }
]

$ shh stats
entries:          4213
unique commands:  1052
oldest:           2025-11-02T08:14:22-05:00
newest:           2026-09-04T17:03:11-05:00

$ shh stats --json
{
  "total_entries": 4213,
  "unique_commands": 1052,
  "oldest_time": "2025-11-02T08:14:22-05:00",
  "newest_time": "2026-09-04T17:03:11-05:00"
}
```

`--json` is supported on every subcommand and always produces the same
shape whether the input is a bash or zsh history file, which is the whole
point: the file format is an implementation detail you shouldn't need to
know about to pipe this into `jq` or another script.

## As a library

The parser is usable on its own:

```go
f, _ := os.Open(os.Getenv("HISTFILE"))
entries, err := history.Parse(f)
if err != nil {
    log.Fatal(err)
}
for _, e := range entries {
    fmt.Println(e.Command, e.Time)
}
```

`Entry.Time` is the zero `time.Time` when the source line had no timestamp,
which is the common case for a plain bash history file.

## Status

Early. Parses bash (plain and `HISTTIMEFORMAT`) and zsh (`EXTENDED_HISTORY`)
formats. No dependencies outside the standard library.
