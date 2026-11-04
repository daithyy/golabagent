package rag

import (
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

func extractPDF(path string) (text string, err error) {
	// the PDF library can panic on malformed files
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("extract %s: %v", path, r)
		}
	}()

	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		if e := f.Close(); e != nil {
			err = e
		}
	}()

	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}

		rows, err := page.GetTextByRow()
		if err != nil {
			return "", fmt.Errorf("extract %s page %d: %w", path, i, err)
		}

		for _, row := range rows {
			var line strings.Builder
			for _, word := range row.Content {
				line.WriteString(word.S)
			}

			clean := strings.Join(strings.Fields(line.String()), " ")
			if clean == "" || isNumber(clean) {
				continue
			}
			b.WriteString(clean)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

func isNumber(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
