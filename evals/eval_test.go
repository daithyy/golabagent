//go:build eval

// Package evals runs the golden question set against a running instance of
// the agent. It is excluded from `go test ./...` by the eval build tag
// because it needs the full stack (Postgres, Ollama, Temporal) and the API.
//
//	make eval                      # or:
//	go test -tags=eval -v -count=1 ./evals
//
// Environment:
//
//	GOLAB_URL       API base URL (default http://localhost:8000)
//	EVAL_MIN_PASS   minimum pass rate for the suite to succeed (default 0.7)
package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type goldenCase struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	// ExpectAll is a list of groups; the answer must match at least one
	// phrase from every group. Phrases are case-insensitive substrings, or
	// regular expressions when prefixed with "re:". Thousands separators are
	// removed from the answer first, so "384,156" matches "384156".
	ExpectAll [][]string `json:"expect_all"`
	// ExpectTools must all be called at least once.
	ExpectTools []string `json:"expect_tools"`
	// ExpectSources: at least one must be retrieved by the agent, and at
	// least one must appear in the top 5 of a direct /search for the question.
	ExpectSources []string `json:"expect_sources"`
}

type askResponse struct {
	WorkflowID string   `json:"workflow_id"`
	Answer     string   `json:"answer"`
	Sources    []string `json:"sources"`
	Steps      []struct {
		Tool    string `json:"tool"`
		IsError bool   `json:"is_error"`
	} `json:"steps"`
	PromptTokens      int      `json:"prompt_tokens"`
	CompletionTokens  int      `json:"completion_tokens"`
	HitStepLimit      bool     `json:"hit_step_limit"`
	GroundingRetries  int      `json:"grounding_retries"`
	UngroundedNumbers []string `json:"ungrounded_numbers"`
}

type searchResponse struct {
	Hits []struct {
		Source string `json:"source"`
	} `json:"hits"`
}

type outcome struct {
	id               string
	passed           bool
	retrievalHit     *bool
	latency          time.Duration
	tokens           int
	toolErrors       int
	groundingRetries int
	failures         []string
	workflowID       string
}

func TestGolden(t *testing.T) {
	baseURL := envOr("GOLAB_URL", "http://localhost:8000")
	minPass, err := strconv.ParseFloat(envOr("EVAL_MIN_PASS", "0.7"), 64)
	if err != nil {
		t.Fatalf("invalid EVAL_MIN_PASS: %v", err)
	}

	raw, err := os.ReadFile("golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Timeout: 15 * time.Minute}
	var results []outcome

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			o := run(t, client, baseURL, c)
			results = append(results, o)
			if !o.passed {
				t.Logf("FAIL %s: %s (workflow %s)", c.ID, strings.Join(o.failures, "; "), o.workflowID)
			}
		})
	}

	report(t, results, minPass)
}

func run(t *testing.T, client *http.Client, baseURL string, c goldenCase) outcome {
	o := outcome{id: c.ID}

	body, _ := json.Marshal(map[string]string{"question": c.Question})
	start := time.Now()
	res, err := client.Post(baseURL+"/ask", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /ask: %v", err)
	}
	defer res.Body.Close()
	o.latency = time.Since(start)

	if res.StatusCode != http.StatusOK {
		o.failures = append(o.failures, "HTTP "+res.Status)
		return o
	}

	var ask askResponse
	if err := json.NewDecoder(res.Body).Decode(&ask); err != nil {
		t.Fatalf("decode /ask: %v", err)
	}
	o.workflowID = ask.WorkflowID
	o.tokens = ask.PromptTokens + ask.CompletionTokens

	answer := normalise(ask.Answer)
	for _, group := range c.ExpectAll {
		if !slices.ContainsFunc(group, func(p string) bool { return matches(t, answer, p) }) {
			o.failures = append(o.failures, fmt.Sprintf("answer missing one of %q", group))
		}
	}

	var called []string
	for _, s := range ask.Steps {
		called = append(called, s.Tool)
		if s.IsError {
			o.toolErrors++
		}
	}
	for _, tool := range c.ExpectTools {
		if !slices.Contains(called, tool) {
			o.failures = append(o.failures, fmt.Sprintf("did not call %s (called %v)", tool, called))
		}
	}

	if len(c.ExpectSources) > 0 {
		if !slices.ContainsFunc(c.ExpectSources, func(s string) bool { return slices.Contains(ask.Sources, s) }) {
			o.failures = append(o.failures, fmt.Sprintf("agent did not retrieve any of %v (got %v)", c.ExpectSources, ask.Sources))
		}

		hit := retrievalHit(t, client, baseURL, c)
		o.retrievalHit = &hit
	}

	if ask.HitStepLimit {
		o.failures = append(o.failures, "hit step limit")
	}
	if len(ask.UngroundedNumbers) > 0 {
		o.failures = append(o.failures, fmt.Sprintf("numbers not found in any tool result: %v", ask.UngroundedNumbers))
	}
	o.groundingRetries = ask.GroundingRetries

	o.passed = len(o.failures) == 0
	return o
}

// retrievalHit checks retrieval on its own: does a direct hybrid search for
// the question return an expected source in the top 5? This separates
// "retrieval failed" from "the model ignored good context".
func retrievalHit(t *testing.T, client *http.Client, baseURL string, c goldenCase) bool {
	res, err := client.Get(baseURL + "/search?k=5&q=" + url.QueryEscape(c.Question))
	if err != nil {
		t.Fatalf("GET /search: %v", err)
	}
	defer res.Body.Close()

	var sr searchResponse
	if err := json.NewDecoder(res.Body).Decode(&sr); err != nil {
		t.Fatalf("decode /search: %v", err)
	}

	for _, h := range sr.Hits {
		if slices.Contains(c.ExpectSources, h.Source) {
			return true
		}
	}
	return false
}

func report(t *testing.T, results []outcome, minPass float64) {
	var passed, retrievalCases, retrievalHits, tokens, toolErrors, groundingRetries int
	var latency time.Duration

	t.Log("")
	t.Logf("%-26s %-6s %-10s %9s %8s", "case", "pass", "retrieval", "latency", "tokens")
	for _, r := range results {
		ret := "-"
		if r.retrievalHit != nil {
			retrievalCases++
			ret = "miss"
			if *r.retrievalHit {
				retrievalHits++
				ret = "hit"
			}
		}
		if r.passed {
			passed++
		}
		tokens += r.tokens
		toolErrors += r.toolErrors
		groundingRetries += r.groundingRetries
		latency += r.latency
		t.Logf("%-26s %-6v %-10s %8.1fs %8d", r.id, r.passed, ret, r.latency.Seconds(), r.tokens)
	}

	n := len(results)
	if n == 0 {
		t.Fatal("no eval cases ran")
	}

	passRate := float64(passed) / float64(n)
	t.Log("")
	t.Logf("pass rate:         %d/%d (%.0f%%)", passed, n, passRate*100)
	if retrievalCases > 0 {
		t.Logf("retrieval hit@5:   %d/%d (%.0f%%)", retrievalHits, retrievalCases, float64(retrievalHits)/float64(retrievalCases)*100)
	}
	t.Logf("mean latency:      %.1fs", latency.Seconds()/float64(n))
	t.Logf("mean tokens:       %d", tokens/n)
	t.Logf("tool errors:       %d (model self-corrections)", toolErrors)
	t.Logf("grounding retries: %d (answers sent back for unsupported numbers)", groundingRetries)

	if passRate < minPass {
		t.Fatalf("pass rate %.0f%% is below the %.0f%% threshold", passRate*100, minPass*100)
	}
}

// thousandsSep matches a separator between a digit and a group of three
// digits, e.g. the comma in "384,156" or the space in "384 156".
var thousandsSep = regexp.MustCompile(`(\d)[,\x{00a0}\x{202f} ](\d{3})\b`)

func normalise(answer string) string {
	s := strings.ToLower(answer)
	for {
		next := thousandsSep.ReplaceAllString(s, "$1$2")
		if next == s {
			return s
		}
		s = next
	}
}

func matches(t *testing.T, answer, phrase string) bool {
	if pattern, ok := strings.CutPrefix(phrase, "re:"); ok {
		re, err := regexp.Compile("(?is)" + pattern)
		if err != nil {
			t.Fatalf("invalid pattern %q: %v", phrase, err)
		}
		return re.MatchString(answer)
	}
	return strings.Contains(answer, strings.ToLower(phrase))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
