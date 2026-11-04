package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/daithyy/golabagent/llm"
	"github.com/daithyy/golabagent/portfolio"
	"github.com/daithyy/golabagent/rag"
	"github.com/daithyy/golabagent/tools"
)

type Activities struct {
	LLM     *llm.Client
	Tools   *tools.Toolbox
	Indexer *rag.Indexer
	Pool    *pgxpool.Pool
	HTTP    *http.Client
}

type ChatInput struct {
	Messages   []llm.Message
	AllowTools bool
}

type ChatResult struct {
	Message          llm.Message
	PromptTokens     int
	CompletionTokens int
}

func (a *Activities) Chat(ctx context.Context, in ChatInput) (ChatResult, error) {
	var defs []llm.Tool
	if in.AllowTools {
		var err error
		if defs, err = a.Tools.Definitions(); err != nil {
			return ChatResult{}, err
		}
	}

	resp, err := a.LLM.Chat(ctx, in.Messages, defs)
	if err != nil {
		return ChatResult{}, err
	}

	if resp.Thinking != "" {
		activity.GetLogger(ctx).Debug("model thinking", "thinking", resp.Thinking)
	}

	return ChatResult{
		Message:          resp.Message,
		PromptTokens:     resp.PromptTokens,
		CompletionTokens: resp.CompletionTokens,
	}, nil
}

type ToolResult struct {
	Content string
	Sources []string
	IsError bool
}

// CallTool executes one tool call. Mistakes the model can fix (bad SQL,
// unknown tool) come back as an error result for the model to read; only
// infrastructure failures fail the activity so Temporal retries them
func (a *Activities) CallTool(ctx context.Context, call llm.ToolCall) (ToolResult, error) {
	res, err := a.Tools.Call(ctx, call.Function.Name, call.Function.Arguments)

	var argErr *tools.ArgError
	if errors.As(err, &argErr) {
		return ToolResult{Content: "ERROR: " + argErr.Error(), IsError: true}, nil
	}
	if err != nil {
		return ToolResult{}, err
	}

	return ToolResult{Content: res.Text, Sources: res.Sources}, nil
}

// Manifest lists the external data sources (data/sources.json), so the
// provenance and licence of every input is recorded in one place
type Manifest struct {
	DownloadDir string `json:"download_dir"`
	NotesDir    string `json:"notes_dir"`
	Portfolio   struct {
		Policies Source `json:"policies"`
		Claims   Source `json:"claims"`
	} `json:"portfolio"`
	Documents []Source `json:"documents"`
}

type Source struct {
	File        string `json:"file"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Licence     string `json:"licence,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

func (a *Activities) ReadManifest(_ context.Context, path string) (Manifest, error) {
	var m Manifest

	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, temporal.NewNonRetryableApplicationError("invalid manifest "+path, "InvalidManifest", err)
	}

	return m, nil
}

type DownloadInput struct {
	URL  string
	Path string
}

type DownloadResult struct {
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	Skipped bool   `json:"skipped"`
}

// DownloadFile fetches a URL to a local path unless the file already exists.
// It writes to a temporary file and renames it into place, so an interrupted
// download never leaves a partial file behind for the next attempt to trust
func (a *Activities) DownloadFile(ctx context.Context, in DownloadInput) (DownloadResult, error) {
	if fi, err := os.Stat(in.Path); err == nil && fi.Size() > 0 {
		return DownloadResult{Path: in.Path, Bytes: fi.Size(), Skipped: true}, nil
	}

	if err := os.MkdirAll(filepath.Dir(in.Path), 0o755); err != nil {
		return DownloadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return DownloadResult{}, err
	}
	req.Header.Set("User-Agent", "golabagent/0.1 (+https://github.com/daithyy/golabagent)")

	res, err := a.HTTP.Do(req)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("download %s: %w", in.URL, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return DownloadResult{}, fmt.Errorf("download %s: %s", in.URL, res.Status)
	}

	tmp := in.Path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return DownloadResult{}, err
	}
	n, err := io.Copy(f, res.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("download %s: %w", in.URL, err)
	}

	// catch HTML error pages served with a 200 status
	if strings.EqualFold(filepath.Ext(in.Path), ".pdf") && !hasPrefix(tmp, "%PDF") {
		_ = os.Remove(tmp)
		return DownloadResult{}, fmt.Errorf("download %s: response is not a PDF", in.URL)
	}

	if err := os.Rename(tmp, in.Path); err != nil {
		return DownloadResult{}, err
	}

	return DownloadResult{Path: in.Path, Bytes: n}, nil
}

func hasPrefix(path, prefix string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, len(prefix))
	if _, err := io.ReadFull(f, buf); err != nil {
		return false
	}
	return bytes.Equal(buf, []byte(prefix))
}

type LoadPortfolioInput struct {
	PoliciesPath string
	ClaimsPath   string
}

func (a *Activities) LoadPortfolio(ctx context.Context, in LoadPortfolioInput) (portfolio.LoadResult, error) {
	return portfolio.Load(ctx, a.Pool, in.PoliciesPath, in.ClaimsPath)
}

// ListNotes returns the markdown and text files in dir, sorted
func (a *Activities) ListNotes(_ context.Context, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (ext == ".md" || ext == ".txt") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)

	return paths, nil
}

// DocumentRef identifies a document to ingest. Title is used when the
// document has no "# " heading of its own, e.g. for PDFs
type DocumentRef struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
}

func (a *Activities) IngestDocument(ctx context.Context, doc DocumentRef) (rag.IngestResult, error) {
	return a.Indexer.IngestFile(ctx, doc.Path, doc.Title)
}

// PruneDocuments removes indexed documents whose source is not in keep
func (a *Activities) PruneDocuments(ctx context.Context, keep []string) ([]string, error) {
	return a.Indexer.Prune(ctx, keep)
}
