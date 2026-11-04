package rag

import (
	"fmt"
	"strings"
)

// Chunk splits a markdown document into chunks
func Chunk(doc string, maxChars int) []string {
	var (
		title, section string
		chunks         []string
		body           []string
		size           int
	)

	header := func() string {
		var h strings.Builder
		if title != "" {
			fmt.Fprintf(&h, "Document: %s\n", title)
		}
		if section != "" && section != title {
			fmt.Fprintf(&h, "Section: %s\n", section)
		}
		if h.Len() > 0 {
			h.WriteString("\n")
		}
		return h.String()
	}

	flush := func() {
		if len(body) == 0 {
			return
		}
		chunks = append(chunks, header()+strings.Join(body, "\n\n"))
		body, size = nil, 0
	}

	for para := range strings.SplitSeq(strings.ReplaceAll(doc, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if heading, level, ok := parseHeading(para); ok {
			// a new section starts a new chunk to prevent disruption
			flush()
			if level == 1 && title == "" {
				title = heading
			} else {
				section = heading
			}

			// keep any text that directly follows the heading line
			_, rest, _ := strings.Cut(para, "\n")
			if rest = strings.TrimSpace(rest); rest == "" {
				continue
			}
			para = rest
		}

		for _, piece := range splitLong(para, maxChars) {
			if size > 0 && size+len(piece) > maxChars {
				flush()
			}
			body = append(body, piece)
			size += len(piece)
		}
	}
	flush()

	return chunks
}

// splitLong breaks a paragraph longer than maxChars into pieces at line
// boundaries, falling back to word boundaries for very long lines
func splitLong(para string, maxChars int) []string {
	if len(para) <= maxChars {
		return []string{para}
	}

	var (
		pieces []string
		cur    strings.Builder
	)
	add := func(unit, sep string) {
		if cur.Len() > 0 && cur.Len()+len(sep)+len(unit) > maxChars {
			pieces = append(pieces, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString(sep)
		}
		cur.WriteString(unit)
	}

	for line := range strings.SplitSeq(para, "\n") {
		if len(line) <= maxChars {
			add(line, "\n")
			continue
		}
		for word := range strings.FieldsSeq(line) {
			add(word, " ")
		}
	}
	if cur.Len() > 0 {
		pieces = append(pieces, cur.String())
	}

	return pieces
}

func parseHeading(para string) (string, int, bool) {
	line, _, _ := strings.Cut(para, "\n")
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(line) || line[level] != ' ' {
		return "", 0, false
	}
	return strings.TrimSpace(line[level:]), level, true
}
