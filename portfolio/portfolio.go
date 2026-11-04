// Package portfolio loads the underwriting portfolio
// into Postgres from the OpenML ARFF files
package portfolio

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// arffReader streams the data rows of an ARFF file. Only the subset of ARFF
// used by OpenML's freMTPL2 exports is supported: comma-separated values,
// optionally wrapped in quotes, no sparse rows and no commas inside values
type arffReader struct {
	sc   *bufio.Scanner
	line int
}

// newARFFReader reads the header up to and including the @data line
func newARFFReader(r io.Reader) (*arffReader, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	a := &arffReader{sc: sc}

	for sc.Scan() {
		a.line++
		if strings.EqualFold(strings.TrimSpace(sc.Text()), "@data") {
			return a, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no @data section found")
}

// Next returns the fields of the next data row, or io.EOF after the last one
func (a *arffReader) Next() ([]string, error) {
	for a.sc.Scan() {
		a.line++
		text := strings.TrimSpace(a.sc.Text())
		if text == "" || strings.HasPrefix(text, "%") {
			continue
		}

		fields := strings.Split(text, ",")
		for i, f := range fields {
			fields[i] = strings.Trim(strings.TrimSpace(f), `'"`)
		}
		return fields, nil
	}

	if err := a.sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// Line is the current line number, for error messages
func (a *arffReader) Line() int { return a.line }
