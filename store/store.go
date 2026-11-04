// Package store implements Postgres connection and schema
package store

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

func DescribeSchema(ctx context.Context, pool *pgxpool.Pool, schema string) (string, error) {
	rows, err := pool.Query(ctx, `
         SELECT c.table_name,
         c.column_name,
		            c.data_type,
		            COALESCE(col_description(format('%I.%I', c.table_schema, c.table_name)::regclass, c.ordinal_position), '')
         FROM information_schema.columns c
         WHERE c.table_schema = $1
         ORDER BY c.table_name, c.ordinal_position`, schema)
	if err != nil {
		return "", fmt.Errorf("query %s schema: %w", schema, err)
	}
	defer rows.Close()

	var (
		b       strings.Builder
		current string
	)
	for rows.Next() {
		var table, column, dataType, comment string
		if err := rows.Scan(&table, &column, &dataType, &comment); err != nil {
			return "", err
		}

		if table != current {
			if current != "" {
				b.WriteString(")\n")
			}
			fmt.Fprintf(&b, "%s.%s(", schema, table)
			current = table
		} else {
			b.WriteString(", ")
		}

		fmt.Fprintf(&b, "%s %s", column, dataType)
		if comment != "" {
			fmt.Fprintf(&b, " -- %s", comment)
		}
	}
	if current != "" {
		b.WriteString(")\n")
	}

	return b.String(), rows.Err()
}
