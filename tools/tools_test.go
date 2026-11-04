package tools

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestValidateSQL(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    string
		wantErr bool
	}{
		{name: "select", sql: "SELECT * FROM policies", want: "SELECT * FROM policies"},
		{name: "trailing semicolon", sql: "  select 1;  ", want: "select 1"},
		{name: "cte", sql: "WITH x AS (SELECT 1) SELECT * FROM x", want: "WITH x AS (SELECT 1) SELECT * FROM x"},
		{name: "empty", sql: "  ", wantErr: true},
		{name: "multiple statements", sql: "SELECT 1; DROP TABLE uw.policies", wantErr: true},
		{name: "escape transaction", sql: "SELECT 1) q; COMMIT; DELETE FROM uw.policies; SELECT (1", wantErr: true},
		{name: "delete", sql: "DELETE FROM uw.policies", wantErr: true},
		{name: "update", sql: "update uw.policies set claim_count = 0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateSQL(tt.sql)
			if tt.wantErr {
				var argErr *ArgError
				if !errors.As(err, &argErr) {
					t.Fatalf("want ArgError, got %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("got (%q, %v), want %q", got, err, tt.want)
			}
		})
	}
}

func TestWrapSQLSurvivesTrailingComment(t *testing.T) {
	got := wrapSQL("SELECT 1 -- count")
	want := "SELECT * FROM (\nSELECT 1 -- count\n) AS q LIMIT 101"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestArgSchemas(t *testing.T) {
	s, err := jsonschema.For[QueryPortfolioArgs](nil)
	if err != nil {
		t.Fatal(err)
	}

	b, _ := json.Marshal(s)
	var got struct {
		Type       string                    `json:"type"`
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	if got.Type != "object" || len(got.Required) != 1 || got.Required[0] != "sql" {
		t.Errorf("unexpected schema: %s", b)
	}
	if got.Properties["sql"]["description"] == "" {
		t.Errorf("description missing: %s", b)
	}
}
