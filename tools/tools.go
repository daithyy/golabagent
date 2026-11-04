// Package tools defines the functions the agent can call
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/daithyy/golabagent/llm"
	"github.com/daithyy/golabagent/rag"
)

const (
	SearchDocsName     = "search_docs"
	QueryPortfolioName = "query_portfolio_db"

	searchResults = 5
	maxRows       = 100
)

// tool inputs
type SearchDocsArgs struct {
	Query string `json:"query" jsonschema:"What to look for, using specific terms, e.g. 'Irish private motor average claims cost per policy'"`
}

type QueryPortfolioArgs struct {
	SQL string `json:"sql" jsonschema:"A single read-only PostgreSQL SELECT statement over the uw schema"`
}

// tools output
type Result struct {
	Text    string
	Sources []string
}

// problem that can be fixed, returned as tool result
type ArgError struct{ msg string }

func (e *ArgError) Error() string { return e.msg }

func argErrorf(format string, a ...any) error { return &ArgError{msg: fmt.Sprintf(format, a...)} }

type Toolbox struct {
	pool            *pgxpool.Pool
	indexer         *rag.Indexer
	portfolioSchema string
}

func New(pool *pgxpool.Pool, indexer *rag.Indexer, portfolioSchema string) *Toolbox {
	return &Toolbox{pool: pool, indexer: indexer, portfolioSchema: portfolioSchema}
}

func (t *Toolbox) SearchDocsDescription() string {
	return "Search the reference documents: Central Bank of Ireland reports on the Irish private motor market " +
		"(National Claims Information Database) and on differential pricing, EIOPA's opinion on AI governance in insurance, " +
		"the French bonus-malus rules, and the portfolio data dictionary. " +
		"Use it for market benchmarks, regulation, definitions and rating rules. " +
		"Returns the most relevant passages, each labelled with its source file name for citation."
}

// QueryPortfolioDescription includes metric definitions as well as the
// schema, so the model computes rates the way an actuary would (weighted by
// exposure) rather than averaging per-policy values
func (t *Toolbox) QueryPortfolioDescription() string {
	return "Run a read-only SQL query against the underwriting portfolio: a table of motor insurance policies " +
		"with the claims made on them. Use it for anything quantitative about the portfolio. " +
		fmt.Sprintf("At most %d rows are returned, so aggregate in SQL rather than fetching raw rows. ", maxRows) +
		"There are no dates in this data, so it cannot answer questions about trends over time.\n\n" +
		"Metric definitions:\n" +
		"- Claim frequency = SUM(claim_count) / SUM(exposure): claims per policy-year. Always weight by exposure.\n" +
		"- Pure premium = SUM(total_claim_amount) / SUM(exposure): claims cost in euro per policy-year.\n" +
		"- Band continuous values with CASE, e.g. driver_age bands 18-25, 26-35, 36-50, 51-65, 66+.\n\n" +
		"Schema:\n" + t.portfolioSchema
}

// Definitions returns the tool specs in Ollama format
func (t *Toolbox) Definitions() ([]llm.Tool, error) {
	searchSchema, err := jsonschema.For[SearchDocsArgs](nil)
	if err != nil {
		return nil, err
	}
	sqlSchema, err := jsonschema.For[QueryPortfolioArgs](nil)
	if err != nil {
		return nil, err
	}

	return []llm.Tool{
		{Type: "function", Function: llm.ToolFunction{Name: SearchDocsName, Description: t.SearchDocsDescription(), Parameters: searchSchema}},
		{Type: "function", Function: llm.ToolFunction{Name: QueryPortfolioName, Description: t.QueryPortfolioDescription(), Parameters: sqlSchema}},
	}, nil
}

// Call dispatches a model tool call by name
func (t *Toolbox) Call(ctx context.Context, name string, rawArgs json.RawMessage) (Result, error) {
	switch name {
	case SearchDocsName:
		var args SearchDocsArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return Result{}, argErrorf("invalid arguments for %s: %v", name, err)
		}
		return t.SearchDocs(ctx, args)
	case QueryPortfolioName:
		var args QueryPortfolioArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return Result{}, argErrorf("invalid arguments for %s: %v", name, err)
		}
		return t.QueryPortfolio(ctx, args)
	default:
		return Result{}, argErrorf("unknown tool %q; available tools: %s, %s", name, SearchDocsName, QueryPortfolioName)
	}
}

func (t *Toolbox) SearchDocs(ctx context.Context, args SearchDocsArgs) (Result, error) {
	if strings.TrimSpace(args.Query) == "" {
		return Result{}, argErrorf("query must not be empty")
	}

	hits, err := t.indexer.Search(ctx, args.Query, searchResults)
	if err != nil {
		return Result{}, err
	}
	if len(hits) == 0 {
		return Result{Text: "No matching documents found. Documents may not have been ingested yet."}, nil
	}

	var (
		b       strings.Builder
		sources []string
		seen    = map[string]bool{}
	)
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] source: %s\n%s\n\n", i+1, h.Source, h.Content)
		if !seen[h.Source] {
			seen[h.Source] = true
			sources = append(sources, h.Source)
		}
	}

	return Result{Text: strings.TrimSpace(b.String()), Sources: sources}, nil
}

// QueryPortfolio runs model-generated SQL with simple guardrails:
//
//  1. Basic validation: a single SELECT/WITH statement
//  2. A READ ONLY transaction, so nothing can be changed
//  3. A statement timeout and a row limit
//
// The extended protocol also rejects multiple statements server-side
func (t *Toolbox) QueryPortfolio(ctx context.Context, args QueryPortfolioArgs) (Result, error) {
	query, err := validateSQL(args.SQL)
	if err != nil {
		return Result{}, err
	}

	tx, err := t.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, stmt := range []string{
		`SET LOCAL statement_timeout = '5s'`,
		`SET LOCAL search_path = uw`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return Result{}, fmt.Errorf("%s: %w", stmt, err)
		}
	}

	// text result format means every value arrives as a printable string,
	// whatever its Postgres type
	rows, err := tx.Query(ctx, wrapSQL(query), pgx.QueryResultFormats{pgx.TextFormatCode})
	if err != nil {
		return Result{}, sqlError(err)
	}
	defer rows.Close()

	var b strings.Builder
	cols := rows.FieldDescriptions()
	for i, c := range cols {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(c.Name)
	}
	b.WriteString("\n")

	n := 0
	for rows.Next() {
		if n == maxRows {
			fmt.Fprintf(&b, "... truncated at %d rows; aggregate or add a WHERE clause to see everything\n", maxRows)
			break
		}
		for i, v := range rows.RawValues() {
			if i > 0 {
				b.WriteString(" | ")
			}
			if v == nil {
				b.WriteString("NULL")
			} else {
				b.Write(v)
			}
		}
		b.WriteString("\n")
		n++
	}
	if err := rows.Err(); err != nil {
		return Result{}, sqlError(err)
	}

	if n == 0 {
		b.WriteString("(no rows)\n")
	}

	return Result{Text: b.String()}, nil
}

func validateSQL(sql string) (string, error) {
	q := strings.TrimSpace(sql)
	q = strings.TrimSpace(strings.TrimSuffix(q, ";"))

	if q == "" {
		return "", argErrorf("sql must not be empty")
	}
	if strings.Contains(q, ";") {
		return "", argErrorf("only a single SQL statement is allowed; remove the ';'")
	}

	first := strings.ToUpper(strings.Fields(q)[0])
	if first != "SELECT" && first != "WITH" {
		return "", argErrorf("only read-only SELECT (or WITH ... SELECT) queries are allowed")
	}

	return q, nil
}

// wrapSQL enforces the row limit, newlines stop a trailing "-- comment"
// in the model SQL from swallowing the closing parenthesis
func wrapSQL(query string) string {
	return fmt.Sprintf("SELECT * FROM (\n%s\n) AS q LIMIT %d", query, maxRows+1)
}

// sqlError turns errors the model caused (syntax errors, unknown columns,
// timeouts, permission denied) into ArgErrors with the Postgres message, so
// the model can correct its query
//
// Anything else (e.g. a lost connection) is an infrastructure error
// and is returned as-is, to be retried
func sqlError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	msg := "SQL error: " + pgErr.Message
	if pgErr.Hint != "" {
		msg += " (hint: " + pgErr.Hint + ")"
	}
	if pgErr.Code == "57014" {
		msg += ". The query timed out; aggregate or filter more."
	}
	return &ArgError{msg: msg}
}
