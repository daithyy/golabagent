package workflows

import (
	"path/filepath"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/daithyy/golabagent/portfolio"
)

type ImportInput struct {
	// manifest is the path to the data sources manifest, e.g. data/sources.json
	Manifest string `json:"manifest"`
}

type ImportResult struct {
	Downloads []DownloadResult     `json:"downloads"`
	Portfolio portfolio.LoadResult `json:"portfolio"`
	Documents IngestResult         `json:"documents"`
}

// ImportWorkflow brings the agent's data up to date from the manifest:
//
//  1. Download every external source (portfolio files and PDFs) in parallel,
//     skipping files already downloaded
//  2. Reload the portfolio tables from the downloaded files
//  3. Run IngestWorkflow as a child workflow over the PDFs and the notes in
//     notes_dir, pruning documents that are no longer listed
//
// Each step is retried on its own, so a flaky download does not redo the
// load, and a failed embedding does not redo the downloads
func ImportWorkflow(ctx workflow.Context, in ImportInput) (ImportResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: 2 * time.Second,
			MaximumAttempts: 5,
		},
	})

	var a *Activities
	res := ImportResult{Downloads: []DownloadResult{}}

	var m Manifest
	if err := workflow.ExecuteActivity(ctx, a.ReadManifest, in.Manifest).Get(ctx, &m); err != nil {
		return res, err
	}

	path := func(s Source) string { return filepath.Join(m.DownloadDir, s.File) }

	sources := append([]Source{m.Portfolio.Policies, m.Portfolio.Claims}, m.Documents...)
	downloads := make([]workflow.Future, len(sources))
	for i, s := range sources {
		downloads[i] = workflow.ExecuteActivity(ctx, a.DownloadFile, DownloadInput{URL: s.URL, Path: path(s)})
	}
	for _, f := range downloads {
		var d DownloadResult
		if err := f.Get(ctx, &d); err != nil {
			return res, err
		}
		res.Downloads = append(res.Downloads, d)
	}

	load := LoadPortfolioInput{PoliciesPath: path(m.Portfolio.Policies), ClaimsPath: path(m.Portfolio.Claims)}
	if err := workflow.ExecuteActivity(ctx, a.LoadPortfolio, load).Get(ctx, &res.Portfolio); err != nil {
		return res, err
	}

	var notes []string
	if err := workflow.ExecuteActivity(ctx, a.ListNotes, m.NotesDir).Get(ctx, &notes); err != nil {
		return res, err
	}

	docs := make([]DocumentRef, 0, len(m.Documents)+len(notes))
	for _, d := range m.Documents {
		docs = append(docs, DocumentRef{Path: path(d), Title: d.Title})
	}
	for _, n := range notes {
		docs = append(docs, DocumentRef{Path: n})
	}

	child := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID + "-ingest",
	})
	err := workflow.ExecuteChildWorkflow(child, IngestWorkflow, IngestInput{Documents: docs, Prune: true}).Get(ctx, &res.Documents)

	return res, err
}
