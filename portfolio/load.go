package portfolio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LoadResult struct {
	Policies int64 `json:"policies"`
	Claims   int64 `json:"claims"`
}

var policyColumns = []string{
	"policy_id", "claim_count", "exposure", "driver_age", "vehicle_age",
	"fuel", "area", "bonus_malus", "total_claim_amount",
}

// Load replaces the portfolio with the contents of the freMTPL2freq
// (policies) and freMTPL2sev (claims) ARFF files
//
// The claims file has one row per claim. We add them up per policy first, so
// each policy row carries its total claim amount and queries need no join.
// The table is truncated and reloaded in one transaction, so readers see the
// old portfolio or the new one, never a partial load
func Load(ctx context.Context, pool *pgxpool.Pool, policiesPath, claimsPath string) (lr LoadResult, err error) {
	claimTotals, claims, err := sumClaims(claimsPath)
	if err != nil {
		return LoadResult{}, err
	}

	f, err := os.Open(policiesPath)
	if err != nil {
		return LoadResult{}, err
	}
	defer func() {
		if e := f.Close(); e != nil {
			err = e
		}
	}()

	r, err := newARFFReader(f)
	if err != nil {
		return LoadResult{}, fmt.Errorf("%s: %w", policiesPath, err)
	}

	var policies int64
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `TRUNCATE uw.policies`); err != nil {
			return err
		}

		var err error
		policies, err = tx.CopyFrom(ctx, pgx.Identifier{"uw", "policies"}, policyColumns, pgx.CopyFromFunc(func() ([]any, error) {
			fields, err := r.Next()
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}

			row, err := policyRow(fields, claimTotals)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: %w", policiesPath, r.Line(), err)
			}
			return row, nil
		}))
		return err
	})
	if err != nil {
		return LoadResult{}, fmt.Errorf("load uw.policies: %w", err)
	}

	return LoadResult{Policies: policies, Claims: claims}, nil
}

// sumClaims returns the total claim amount per policy id, and the number of
// claim rows read. Claims for policies that are not in the policy file are
// read here but never reach the database
func sumClaims(path string) (m map[int64]float64, i int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if e := f.Close(); e != nil {
			err = e
		}
	}()

	r, err := newARFFReader(f)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}

	var (
		totals = map[int64]float64{}
		n      int64
	)
	for {
		fields, err := r.Next()
		if errors.Is(err, io.EOF) {
			return totals, n, nil
		}
		if err != nil {
			return nil, 0, err
		}

		// A claims row is: IDpol, ClaimAmount.
		if len(fields) != 2 {
			return nil, 0, fmt.Errorf("%s line %d: want 2 fields, got %d", path, r.Line(), len(fields))
		}
		var p parser
		id, amount := p.int(fields[0]), p.number(fields[1])
		if p.err != nil {
			return nil, 0, fmt.Errorf("%s line %d: %w", path, r.Line(), p.err)
		}
		totals[id] += amount
		n++
	}
}

// policyRow converts a freMTPL2freq row
func policyRow(f []string, claimTotals map[int64]float64) ([]any, error) {
	if len(f) != 12 {
		return nil, fmt.Errorf("want 12 fields, got %d", len(f))
	}

	var p parser
	id := p.int(f[0])
	return []any{
		id, p.int(f[1]), f[2], p.int(f[6]), p.int(f[5]),
		f[9], f[3], p.int(f[7]), strconv.FormatFloat(claimTotals[id], 'f', 2, 64),
	}, p.err
}

// parser keeps the first conversion error so rows can be built in one
// expression
type parser struct{ err error }

// int accepts integral values written as floats (e.g. "1.0"), which the ARFF
// files use for numeric attributes
func (p *parser) int(s string) int64 {
	f := p.number(s)
	if p.err == nil && f != float64(int64(f)) {
		p.err = fmt.Errorf("invalid integer %q", s)
	}
	return int64(f)
}

func (p *parser) number(s string) float64 {
	if p.err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		p.err = fmt.Errorf("invalid number %q", s)
	}
	return f
}
