package rag

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/daithyy/golabagent/llm"
)

const (
	// nomic-embed-text is trained with task prefixes
	documentPrefix = "search_document: "
	queryPrefix    = "search_query: "

	chunkChars = 1200
	embedBatch = 32

	rrfK = 60 // common default
)

type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	EmbedModel() string
}

var _ Embedder = (*llm.Client)(nil)

type Indexer struct {
	pool  *pgxpool.Pool
	embed Embedder
}

func NewIndexer(pool *pgxpool.Pool, embed Embedder) *Indexer {
	return &Indexer{pool: pool, embed: embed}
}

type IngestResult struct {
	Source  string `json:"source"`
	Chunks  int    `json:"chunks"`
	Skipped bool   `json:"skipped"`
}

type Hit struct {
	Source      string  `json:"source"`
	ChunkIndex  int     `json:"chunk_index"`
	Content     string  `json:"content"`
	Score       float64 `json:"score"`
	VectorRank  *int    `json:"vector_rank"`
	KeywordRank *int    `json:"keyword_rank"`
}

// formats an embedding as pgvector text input
// e.g. [0.1,0.2]
func vectorLiteral(v []float32) string {
	b := make([]byte, 0, len(v)*10)
	b = append(b, '[')
	for i, f := range v {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendFloat(b, float64(f), 'f', -1, 32)
	}
	return string(append(b, ']'))
}
