package workflows

import (
	"reflect"
	"testing"
)

func TestUngroundedNumbers(t *testing.T) {
	sources := []string{
		"Which age band has the highest frequency?",
		"age_band | claim_frequency\n18-25 | 0.17509154\n36-50 | 0.09940123\n",
		"[1] source: ncid.pdf\nClaims cost per policy increased by 10% in 2025 to €424.",
		"pure_premium\n167.1125\nseverity | 2278.5375\nmax_bonus | 384156\nfreq | 0.0802",
	}

	tests := []struct {
		name   string
		answer string
		want   []string
	}{
		{"exact and rounded", "18-25 has 0.175 claims per policy-year versus 0.099 for 36-50.", nil},
		{"document figure", "The Irish market figure is €424 in 2025.", nil},
		{"integer rounding", "Pure premium is €167 and severity is €2,279.", nil},
		{"thousands separators", "There are 384,156 policies at the maximum bonus.", nil},
		{"percentage of a fraction", "Frequency at maximum bonus is 8.0%.", nil},
		{"small integers ignored", "Of the 5 bands, the top 3 are young.", nil},
		{"fabricated figure", "Your pure premium is about €39.6, roughly 10 times lower.", []string{"39.6"}},
		{"wrong rounding", "Severity is €2,300.", []string{"2,300"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ungroundedNumbers(tt.answer, sources); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
