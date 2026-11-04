package rag

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

//go:embed search.sql
var searchSQL string

// Search runs hybrid retrieval
// 1. top candidates by vector similarity
// 2. full-text rank
// merged with reciprocal rank fusion (RRF)
// Vectors catch paraphrases, e.g. "no-claims discount" or "bonus"
// Keywords catch exact terms and codes like "R11" or "price walking" that embeddings obfuscate
func (ix *Indexer) Search(ctx context.Context, query string, k int) ([]Hit, error) {
	vectors, err := ix.embed.Embed(ctx, []string{queryPrefix + query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	candidates := max(k*4, 20)

	rows, err := ix.pool.Query(ctx, searchSQL,
		vectorLiteral(vectors[0]), orTSQuery(query), candidates, k, rrfK, query)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Hit, error) {
		var h Hit
		err := row.Scan(&h.Source, &h.ChunkIndex, &h.Content, &h.Score, &h.VectorRank, &h.KeywordRank)
		return h, err
	})
}

// Converts free text into an OR tsquery, e.g. "claim | free | bonus"
// used to find candidate chunks through GIN index
//
// Postgres `websearch_to_tsquery` uses AND for every word
func orTSQuery(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	seen := make(map[string]bool, len(words))
	terms := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
	}

	return strings.Join(terms, " | ")
}
