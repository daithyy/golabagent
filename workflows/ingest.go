package workflows

import (
	"path/filepath"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/daithyy/golabagent/rag"
)

type IngestInput struct {
	Documents []DocumentRef `json:"documents"`
	// prune removes previously indexed documents that are not in Documents,
	// making ingestion a sync rather than an append.
	Prune bool `json:"prune"`
}

type IngestResult struct {
	Documents []rag.IngestResult `json:"documents"`
	Chunks    int                `json:"chunks"`
	Skipped   int                `json:"skipped"`
	Pruned    []string           `json:"pruned"`
}

// IngestWorkflow ingests each document as its own activity, in parallel
// Each document is retried independently, and IngestFile is idempotent, so a
// retry or a re-run only re-embeds documents that changed
func IngestWorkflow(ctx workflow.Context, in IngestInput) (IngestResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second,
			MaximumAttempts: 5,
		},
	})

	var a *Activities
	res := IngestResult{Documents: []rag.IngestResult{}, Pruned: []string{}}

	futures := make([]workflow.Future, len(in.Documents))
	for i, doc := range in.Documents {
		futures[i] = workflow.ExecuteActivity(ctx, a.IngestDocument, doc)
	}

	keep := make([]string, 0, len(in.Documents))
	for i, f := range futures {
		var doc rag.IngestResult
		if err := f.Get(ctx, &doc); err != nil {
			return res, err
		}
		res.Documents = append(res.Documents, doc)
		res.Chunks += doc.Chunks
		if doc.Skipped {
			res.Skipped++
		}
		keep = append(keep, filepath.Base(in.Documents[i].Path))
	}

	if in.Prune {
		if err := workflow.ExecuteActivity(ctx, a.PruneDocuments, keep).Get(ctx, &res.Pruned); err != nil {
			return res, err
		}
	}

	return res, nil
}
