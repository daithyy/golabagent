package workflows

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/daithyy/golabagent/portfolio"
	"github.com/daithyy/golabagent/rag"
)

func TestImportWorkflow(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(IngestWorkflow)
	var a *Activities

	var m Manifest
	m.DownloadDir = "data/external"
	m.NotesDir = "data/docs"
	m.Portfolio.Policies = Source{File: "freq.arff", URL: "https://example.com/freq.arff"}
	m.Portfolio.Claims = Source{File: "sev.arff", URL: "https://example.com/sev.arff"}
	m.Documents = []Source{{File: "report.pdf", URL: "https://example.com/report.pdf", Title: "Market Report"}}

	env.OnActivity(a.ReadManifest, mock.Anything, "data/sources.json").Return(m, nil).Once()
	for _, s := range []Source{m.Portfolio.Policies, m.Portfolio.Claims, m.Documents[0]} {
		in := DownloadInput{URL: s.URL, Path: "data/external/" + s.File}
		env.OnActivity(a.DownloadFile, mock.Anything, in).Return(DownloadResult{Path: in.Path}, nil).Once()
	}
	env.OnActivity(a.LoadPortfolio, mock.Anything, LoadPortfolioInput{
		PoliciesPath: "data/external/freq.arff",
		ClaimsPath:   "data/external/sev.arff",
	}).Return(portfolio.LoadResult{Policies: 678013, Claims: 26639}, nil).Once()
	env.OnActivity(a.ListNotes, mock.Anything, "data/docs").Return([]string{"data/docs/notes.md"}, nil).Once()

	// The PDF is ingested with its title from the manifest; notes use their own heading.
	env.OnActivity(a.IngestDocument, mock.Anything, DocumentRef{Path: "data/external/report.pdf", Title: "Market Report"}).
		Return(rag.IngestResult{Source: "report.pdf", Chunks: 40}, nil).Once()
	env.OnActivity(a.IngestDocument, mock.Anything, DocumentRef{Path: "data/docs/notes.md"}).
		Return(rag.IngestResult{Source: "notes.md", Skipped: true}, nil).Once()
	env.OnActivity(a.PruneDocuments, mock.Anything, []string{"report.pdf", "notes.md"}).
		Return([]string{"old-lab-sop.md"}, nil).Once()

	env.ExecuteWorkflow(ImportWorkflow, ImportInput{Manifest: "data/sources.json"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var res ImportResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.Len(t, res.Downloads, 3)
	require.Equal(t, int64(678013), res.Portfolio.Policies)
	require.Equal(t, 40, res.Documents.Chunks)
	require.Equal(t, 1, res.Documents.Skipped)
	require.Equal(t, []string{"old-lab-sop.md"}, res.Documents.Pruned)
	env.AssertExpectations(t)
}
