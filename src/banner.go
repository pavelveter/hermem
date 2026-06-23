package main

import (
	"fmt"
	"io"
	"os"
	"strings"
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

// ANSI sequences. Keep them const so the table lands in the
// binary's read-only data segment; Fprintln won't reallocate.
const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
)

// rainbowStep is the hue delta between consecutive visible
// characters. The 360-step spectrum completes in 360/rainbowStep
// characters. With step=4 a full rainbow spans 90 characters; the
// ~520-char HERMEM banner cycles roughly 5.8 times so the gradient
// is visually obvious without burning excessive escape bytes.
const rainbowStep = 4

// ansiTrueColor returns the SGR sequence that sets the 24-bit
// foreground color, e.g. "\033[38;2;255;128;0m". Always paired
// with ansiReset at the end of the colored region so the next
// print starts with default styling.
func ansiTrueColor(r, g, b int) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

// hueToRGB converts a hue ∈ [0,1) at saturation 1, value 1 into
// 24-bit RGB. Follows the wikipedia six-segment rainbow formula;
// no clamping needed because S and V are fixed at 1.
func hueToRGB(h float64) (int, int, int) {
	i := int(h * 6)
	f := h*6 - float64(i)
	var r, g, bl float64
	switch i % 6 {
	case 0:
		r, g, bl = 1, f, 0
	case 1:
		r, g, bl = 1-f, 1, 0
	case 2:
		r, g, bl = 0, 1, f
	case 3:
		r, g, bl = 0, 1-f, 1
	case 4:
		r, g, bl = f, 0, 1
	case 5:
		r, g, bl = 1, 0, 1-f
	}
	return int(r * 255), int(g * 255), int(bl * 255)
}

// colorizeBanner wraps each visible character in s with an ANSI
// true-color escape, hue-shifted by rainbowStep per character.
// Newlines emit uncolored; spaces emit uncolored but DO advance
// the hue index so a long gap between glyphs doesn't collapse the
// rainbow into monochrome. When colored is false the function
// returns s unchanged — used by all non-TTY paths so test
// captures, log files, and CI runners don't accumulate literal
// escape bytes.
func colorizeBanner(s string, colored bool) string {
	if !colored {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) * 6)
	b.WriteString(ansiBold)
	hueIdx := 0
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteRune(r)
		case ' ':
			b.WriteRune(r)
			hueIdx += rainbowStep
		default:
			hue := float64(hueIdx%360) / 360.0
			red, green, blue := hueToRGB(hue)
			b.WriteString(ansiTrueColor(red, green, blue))
			b.WriteRune(r)
			hueIdx += rainbowStep
		}
	}
	b.WriteString(ansiReset)
	return b.String()
}

// writerIsTTY reports whether w is attached to a character device.
// Non-*os.File writers (bytes.Buffer, test stubs, http.ResponseWriter
// when piped) return false so ANSI emission only happens for real
// terminals.
func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// shouldColor gates ANSI emission through three orthogonal checks:
//   - writerIsTTY: pipable output (bytes.Buffer, redirects, http body)
//     goes out plain.
//   - NO_COLOR env var: per https://no-color.org community contract,
//     any non-empty value disables color even on a TTY.
//   - $TERM in {dumb, ""}: lowest-common-denominator terminals that
//     render SGR sequences as literal bytes.
//
// The TTY check is first because it's the cheapest invariant and,
// when false, already guarantees readability without escape codes.
func shouldColor(w io.Writer) bool {
	if !writerIsTTY(w) {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if term := os.Getenv("TERM"); term == "dumb" || term == "" {
		return false
	}
	return true
}

// printUsage writes the colored HERMEM banner followed by the
// command reference to w. Banner colors are emitted only when w
// passes shouldColor; otherwise the banner and usage are plain
// so pipe/redirect/CI consumers see clean text.
func printUsage(w io.Writer) {
	col := shouldColor(w)
	fmt.Fprintln(w, colorizeBanner(bannerArt, col))
	fmt.Fprintln(w)
	fmt.Fprintln(w, usageText)
}
