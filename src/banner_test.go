package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBannerArtHasSevenLines(t *testing.T) {
	lines := strings.Split(bannerArt, "\n")
	if len(lines) != 7 {
		t.Fatalf("bannerArt has %d lines, want 7", len(lines))
	}
	for i, line := range lines {
		if line == "" {
			t.Errorf("bannerArt line %d is empty", i+1)
		}
	}
}

func TestBannerArtStartsWithBlockGlyph(t *testing.T) {
	// H is the leftmost letter; every row opens with the soft-top
	// "░██" pattern that anchors the H's left pillar.
	for i, line := range strings.Split(bannerArt, "\n") {
		if !strings.HasPrefix(line, "░██") {
			t.Errorf("line %d does not start with '░██': %q", i+1, line)
		}
	}
}

func TestBannerArtContainsAllLetters(t *testing.T) {
	// The M's bottom flares wide to "░██       ░██" so any line
	// containing that pattern is identifiable as an M row. Row 6
	// is the canonical example.
	lines := strings.Split(bannerArt, "\n")
	if len(lines) < 7 {
		t.Fatal("bannerArt shorter than expected")
	}
	if !strings.Contains(lines[5], "░██       ░██") {
		t.Errorf("row 6 should contain M-bottom pattern '░██       ░██': %q", lines[5])
	}
}

func TestPrintUsagePlainToBuffer(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()

	// Plain output — no ANSI escapes regardless of TTY state.
	if strings.Contains(out, "\033[") {
		t.Errorf("printUsage should be ANSI-clean, got escapes:\n%s", out)
	}
	if !strings.Contains(out, "Usage: hermem") {
		t.Errorf("missing 'Usage: hermem' header in usage output")
	}
	for _, cmd := range []string{"store", "search", "query", "edge", "ingest", "serve"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("missing command %q in usage output", cmd)
		}
	}
}

func TestPrintUsageBlankLineAboveBanner(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	lines := strings.Split(buf.String(), "\n")
	if len(lines) < 2 {
		t.Fatalf("printUsage produced fewer than 2 lines: %q", buf.String())
	}
	// Line 0 is the empty line above the banner.
	if lines[0] != "" {
		t.Errorf("first line of printUsage output should be blank, got %q", lines[0])
	}
	// Line 1 is the first row of the banner (H's top).
	if !strings.HasPrefix(lines[1], "░██") {
		t.Errorf("second line should be first banner row, got %q", lines[1])
	}
}

func TestPrintUsageBannerBeforeUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()

	bannerIdx := strings.Index(out, "░██")
	usageIdx := strings.Index(out, "Usage:")
	if bannerIdx < 0 {
		t.Errorf("banner prefix '░██' not found in output")
	}
	if usageIdx < 0 {
		t.Errorf("'Usage:' line not found in output")
	}
	if bannerIdx >= usageIdx {
		t.Errorf("banner should precede usage line: bannerIdx=%d usageIdx=%d", bannerIdx, usageIdx)
	}
}
