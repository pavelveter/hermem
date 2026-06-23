package main

import (
	"fmt"
	"io"
)

// bannerArt is the 7-row HERMEM block-glyph logo. Each letter is
// ~11 columns of half-block (░) and full-block (█) glyphs so the
// strokes have softer tops than a pure █ wall. Whitespace
// separators between letters are preserved; trailing whitespace
// per line is trimmed to keep terminal line-wrap honest.
const bannerArt = `░██     ░██ ░██████████ ░█████████  ░███     ░███ ░██████████ ░███     ░███
░██     ░██ ░██         ░██     ░██ ░████   ░████ ░██         ░████   ░████
░██     ░██ ░██         ░██     ░██ ░██░██ ░██░██ ░██         ░██░██ ░██░██
░██████████ ░█████████  ░█████████  ░██ ░████ ░██ ░█████████  ░██ ░████ ░██
░██     ░██ ░██         ░██   ░██   ░██  ░██  ░██ ░██         ░██  ░██  ░██
░██     ░██ ░██         ░██    ░██  ░██       ░██ ░██         ░██       ░██
░██     ░██ ░██████████ ░██     ░██ ░██       ░██ ░██████████ ░██       ░██`

// usageText is the post-banner command reference. Kept as a
// const string so command-line output stays byte-for-byte aligned
// across runs and snapshot tests can diff against it.
const usageText = `Usage: hermem <command> [args]

Commands:
  store            Store a fact (JSON on stdin)
  search           Search memory (JSON on stdin)
  query            Full pipeline: search + graph walk (JSON on stdin)
  edge             Add an edge (JSON on stdin)
  ingest           Ingest dialog (JSON on stdin)
  task-status      Update task status (JSON on stdin)
  task-executable  List executable tasks (JSON on stdin)
  task-next        Alias for task-executable
  task-list        List tasks with filters (JSON on stdin)
  task-show        Show task + relations (JSON on stdin)
  task-dep         Add/remove task dependency (JSON on stdin)
  task-create      Create task with auto-linked context edges (JSON on stdin)
  task-rollback    Find rollback task for failed task (JSON on stdin)
  serve            Run HTTP server`

// printUsage writes the HERMEM banner followed by the command
// reference to w. A blank line is emitted above the banner so it
// sits visually separated from the surrounding shell context (the
// previous shell prompt or another tool's stderr).
//
// Output is plain text with no ANSI escapes — the banner reads
// the same in terminals, log files, CI capture, and pipes.
func printUsage(w io.Writer) {
	fmt.Fprintln(w) // blank line above the banner
	fmt.Fprintln(w, bannerArt)
	fmt.Fprintln(w) // blank line between banner and usage
	fmt.Fprintln(w, usageText)
}
