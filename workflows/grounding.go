package workflows

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// numberPattern matches numbers such as 424, 0.175, 2,278.54 or 384 156
var numberPattern = regexp.MustCompile(`\d{1,3}(?:[,\x{00a0}\x{202f} ]\d{3})+(?:\.\d+)?|\d+(?:\.\d+)?`)

// ungroundedNumbers returns the numbers in answer that cannot be traced to
// any of the sources (the question and the tool results). A number counts as
// grounded if it is a rounding of a source number, or of a source number
// times 100 (a fraction quoted as a percentage)
//
// It is a cheap, deterministic hallucination check for quantitative answers:
// a model that states a figure it never queried is caught before the answer
// reaches a user. Integers up to 10 are ignored, as they are usually counts
// or ordinals ("top 3", "5 bands") rather than data
func ungroundedNumbers(answer string, sources []string) []string {
	var known []float64
	for _, s := range sources {
		for _, m := range numberPattern.FindAllString(s, -1) {
			if v, _, ok := parseNumber(m); ok {
				known = append(known, v)
			}
		}
	}

	var missing []string
	seen := map[string]bool{}
	for _, m := range numberPattern.FindAllString(answer, -1) {
		v, decimals, ok := parseNumber(m)
		if !ok || seen[m] || (decimals == 0 && v <= 10) {
			continue
		}
		seen[m] = true

		if !grounded(v, decimals, known) {
			missing = append(missing, m)
		}
	}

	return missing
}

func grounded(v float64, decimals int, known []float64) bool {
	tolerance := 0.5*math.Pow10(-decimals) + 1e-9
	for _, k := range known {
		if math.Abs(v-k) <= tolerance || math.Abs(v-k*100) <= tolerance {
			return true
		}
	}
	return false
}

// parseNumber parses a matched number, removing thousands separators, and
// reports how many decimal places it was written with
func parseNumber(s string) (float64, int, bool) {
	clean := strings.NewReplacer(",", "", " ", "", " ", "", " ", "").Replace(s)
	v, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, 0, false
	}

	decimals := 0
	if _, frac, ok := strings.Cut(clean, "."); ok {
		decimals = len(frac)
	}
	return v, decimals, true
}
