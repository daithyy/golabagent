package rag

import (
	"strings"
	"testing"
)

const sop = `# Bonus-Malus Rules

Applies to all French motor policies.

## How the coefficient changes

Each claim-free year reduces the coefficient by 5%.

Each at-fault claim increases it by 25%.

## Limits

The minimum is 0.50.
The maximum is 3.50.`

func TestChunkAddsContextHeaders(t *testing.T) {
	chunks := Chunk(sop, 1200)

	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks (intro + one per section), got %d: %q", len(chunks), chunks)
	}

	for _, c := range chunks {
		if !strings.HasPrefix(c, "Document: Bonus-Malus Rules\n") {
			t.Errorf("chunk missing document header: %q", c)
		}
	}

	if !strings.Contains(chunks[1], "Section: How the coefficient changes") || !strings.Contains(chunks[1], "by 5%") {
		t.Errorf("section chunk wrong: %q", chunks[1])
	}
	if strings.Contains(chunks[1], "0.50") {
		t.Errorf("chunk straddles sections: %q", chunks[1])
	}
}

func TestChunkRespectsMaxChars(t *testing.T) {
	para := strings.Repeat("word ", 50) // 250 chars
	doc := strings.Repeat(para+"\n\n", 10)

	chunks := Chunk(doc, 600)
	if len(chunks) < 4 {
		t.Fatalf("expected the document to be split, got %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 600 {
			t.Errorf("chunk is %d chars, want <= 600", len(c))
		}
	}
}

func TestChunkSplitsLongParagraphs(t *testing.T) {
	// A PDF page often arrives as one "paragraph" of many short lines.
	var lines []string
	for range 40 {
		lines = append(lines, strings.Repeat("x", 59))
	}
	page := strings.Join(lines, "\n") // 2,399 chars

	chunks := Chunk("# Report\n\n"+page, 1200)
	if len(chunks) < 2 {
		t.Fatalf("expected the page to be split, got %d chunk(s)", len(chunks))
	}
	for _, c := range chunks {
		body := strings.TrimPrefix(c, "Document: Report\n\n")
		if len(body) > 1200 {
			t.Errorf("chunk body is %d chars, want <= 1200", len(body))
		}
	}
}

func TestSplitLongFallsBackToWords(t *testing.T) {
	line := strings.Repeat("word ", 100) // one 500-char line
	pieces := splitLong(strings.TrimSpace(line), 120)
	if len(pieces) < 4 {
		t.Fatalf("expected the line to be split at words, got %d", len(pieces))
	}
	for _, p := range pieces {
		if len(p) > 120 || strings.HasPrefix(p, " ") {
			t.Errorf("bad piece %q", p)
		}
	}
}
