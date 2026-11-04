package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

// IngestFile is an idempotent function to chunk, embed and store a markdown, text or PDF document
func (ix *Indexer) IngestFile(ctx context.Context, path, title string) (IngestResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return IngestResult{}, fmt.Errorf("read %s: %w", path, err)
	}

	source := filepath.Base(path)
	h := sha256.New()
	h.Write([]byte(title + "\x00"))
	h.Write(raw)
	hash := hex.EncodeToString(h.Sum(nil))
	model := ix.embed.EmbedModel()

	var unchanged bool
	err = ix.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM rag.documents WHERE source = $1 AND content_hash = $2 AND embed_model = $3)`,
		source, hash, model).Scan(&unchanged)
	if err != nil {
		return IngestResult{}, fmt.Errorf("check %s: %w", source, err)
	}
	if unchanged {
		return IngestResult{Source: source, Skipped: true}, nil
	}

	text := string(raw)
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		if text, err = extractPDF(path); err != nil {
			return IngestResult{}, err
		}
	}
	if title != "" && !strings.HasPrefix(strings.TrimSpace(text), "# ") {
		text = "# " + title + "\n\n" + text
	}

	chunks := Chunk(text, chunkChars)
	if len(chunks) == 0 {
		return IngestResult{Source: source}, nil
	}

	// optimization
	// embed in batches to keep each request to the model server small
	vectors := make([][]float32, 0, len(chunks))
	for start := 0; start < len(chunks); start += embedBatch {
		batch := chunks[start:min(start+embedBatch, len(chunks))]
		inputs := make([]string, len(batch))
		for i, c := range batch {
			inputs[i] = documentPrefix + c
		}

		v, err := ix.embed.Embed(ctx, inputs)
		if err != nil {
			return IngestResult{}, fmt.Errorf("embed %s: %w", source, err)
		}
		vectors = append(vectors, v...)
	}

	err = pgx.BeginFunc(ctx, ix.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO rag.documents (source, content_hash, embed_model) VALUES ($1, $2, $3)
			ON CONFLICT (source) DO UPDATE
			SET content_hash = EXCLUDED.content_hash, embed_model = EXCLUDED.embed_model, ingested_at = now()`,
			source, hash, model); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `DELETE FROM rag.chunks WHERE source = $1`, source); err != nil {
			return err
		}

		batch := &pgx.Batch{}
		for i, c := range chunks {
			batch.Queue(`INSERT INTO rag.chunks (source, chunk_index, content, embedding) VALUES ($1, $2, $3, $4::vector)`,
				source, i, c, vectorLiteral(vectors[i]))
		}
		return tx.SendBatch(ctx, batch).Close()
	})
	if err != nil {
		return IngestResult{}, fmt.Errorf("store %s: %w", source, err)
	}

	return IngestResult{Source: source, Chunks: len(chunks)}, nil
}

func (ix *Indexer) Prune(ctx context.Context, keep []string) ([]string, error) {
	rows, err := ix.pool.Query(ctx, `DELETE FROM rag.documents WHERE source <> ALL($1) RETURNING source`, keep)
	if err != nil {
		return nil, fmt.Errorf("prune documents: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
