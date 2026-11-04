package portfolio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sample = `@relation freMTPL2freq
@attribute IDpol numeric
@attribute VehGas string
% a comment
@data
1,'Regular'

3.0,"Diesel"
`

func TestARFFReader(t *testing.T) {
	r, err := newARFFReader(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}

	var rows [][]string
	for {
		f, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, f)
	}

	if len(rows) != 2 || rows[0][1] != "Regular" || rows[1][0] != "3.0" || rows[1][1] != "Diesel" {
		t.Fatalf("unexpected rows: %q", rows)
	}
}

func TestARFFReaderRequiresData(t *testing.T) {
	if _, err := newARFFReader(strings.NewReader("@relation x\n")); err == nil {
		t.Fatal("expected error without @data")
	}
}

func TestPolicyRow(t *testing.T) {
	totals := map[int64]float64{1: 1128.12}

	// IDpol, ClaimNb, Exposure, Area, VehPower, VehAge, DrivAge, BonusMalus, VehBrand, VehGas, Density, Region
	row, err := policyRow(strings.Split("1,1,0.1,D,5,2,55,50,B12,Regular,1217,R82", ","), totals)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{int64(1), int64(1), "0.1", int64(55), int64(2), "Regular", "D", int64(50), "1128.12"}
	if !reflect.DeepEqual(row, want) {
		t.Errorf("got %v, want %v", row, want)
	}

	// A policy with no claims in the claims file has a total of zero.
	row, err = policyRow(strings.Split("2,0,0.5,A,5,2,40,100,B1,Diesel,10,R11", ","), totals)
	if err != nil || row[8] != "0.00" {
		t.Errorf("got (%v, %v), want total 0.00", row, err)
	}

	if _, err := policyRow(strings.Split("1,x,0.1,D,5,0,55,50,B12,Regular,1217,R82", ","), totals); err == nil {
		t.Error("expected error for non-numeric claim count")
	}
	if _, err := policyRow([]string{"1"}, totals); err == nil {
		t.Error("expected error for short row")
	}
}

func TestSumClaims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sev.arff")
	data := "@relation sev\n@data\n1.0,100.5\n2.0,10\n1.0,20\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	totals, n, err := sumClaims(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || totals[1] != 120.5 || totals[2] != 10 {
		t.Errorf("got %v from %d rows", totals, n)
	}
}
