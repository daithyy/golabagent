package workflows

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/daithyy/golabagent/llm"
)

// MaxSteps bounds the agent loop so a confused model cannot loop forever
const MaxSteps = 6

const systemPrompt = `You are Underwriting Insight Agent, an assistant for an insurance data analytics team in Ireland that supports underwriters.

You have two tools:
- query_portfolio_db: read-only SQL over the underwriting portfolio, a book of motor third-party liability policies (rating factors, exposure, claim counts) and their claim amounts.
- search_docs: search reference documents: Central Bank of Ireland reports on the Irish private motor market and on differential pricing, EIOPA's opinion on AI governance in insurance, the French bonus-malus rules, and the portfolio data dictionary.

Rules:
1. Use query_portfolio_db for anything quantitative about the portfolio. Never guess numbers. Weight rates by exposure. Do any calculation, including ratios and percentage differences against figures from documents, with query_portfolio_db (e.g. SELECT 1 - 167.11 / 424), never in your head.
2. Use search_docs for Irish market benchmarks, regulation, definitions and rating rules.
3. Questions that compare the portfolio with the market, or ask what a result means, need both tools.
4. Cite documents inline using the source file name in square brackets, e.g. [bonus-malus-rules.md]. Only cite sources returned by search_docs; never cite tool names. Present database results as plain numbers.
5. If the tools do not contain the answer, say so. Do not invent data, and only describe searches or queries you actually ran.
6. You inform underwriting decisions; you do not make them. Do not recommend declining or pricing individual customers.
7. Be concise: give the direct answer first, then the supporting numbers and citations.

Today's date is %s.`

type AskInput struct {
	Question string `json:"question"`
}

// step records one tool call, for the API response and debugging
type Step struct {
	Tool    string `json:"tool"`
	Args    string `json:"args"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

type AskResult struct {
	Answer           string   `json:"answer"`
	Sources          []string `json:"sources"`
	Steps            []Step   `json:"steps"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	// HitStepLimit is true when the model ran out of tool-calling steps and
	// was forced to answer.
	HitStepLimit bool `json:"hit_step_limit"`
	// GroundingRetries counts how often the model was sent back because its
	// answer contained numbers not found in any tool result.
	GroundingRetries int `json:"grounding_retries"`
	// UngroundedNumbers lists numbers in the final answer that still could
	// not be traced to a tool result. Non-empty means: do not trust the figures.
	UngroundedNumbers []string `json:"ungrounded_numbers"`
}

// MaxGroundingRetries bounds how often an answer is sent back for containing
// unsupported numbers
const MaxGroundingRetries = 1

const groundingFeedback = `Your answer contains numbers that do not appear in any tool result: %s.
Every number in your answer must come from a tool result. Run a query or search to get these figures, calculate derived figures such as ratios with query_portfolio_db, or remove them. Then answer again.`

func AskWorkflow(ctx workflow.Context, in AskInput) (AskResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// Generous: a 9B model on modest hardware can take a while per turn.
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: 2 * time.Second,
			MaximumAttempts: 3,
		},
	})

	var a *Activities
	// non-nil slices so the API returns [] rather than null
	res := AskResult{Sources: []string{}, Steps: []Step{}, UngroundedNumbers: []string{}}

	// workflow.Now is deterministic: it is the same on replay
	today := workflow.Now(ctx).UTC().Format("Monday 2 January 2006")
	messages := []llm.Message{
		{Role: "system", Content: fmt.Sprintf(systemPrompt, today)},
		{Role: "user", Content: in.Question},
	}

	// everything the answer's numbers may legitimately come from
	evidence := []string{in.Question}

	for step := 0; ; step++ {
		allowTools := step < MaxSteps
		res.HitStepLimit = !allowTools

		var chat ChatResult
		if err := workflow.ExecuteActivity(ctx, a.Chat, ChatInput{Messages: messages, AllowTools: allowTools}).Get(ctx, &chat); err != nil {
			return res, err
		}
		res.PromptTokens += chat.PromptTokens
		res.CompletionTokens += chat.CompletionTokens
		messages = append(messages, chat.Message)

		if len(chat.Message.ToolCalls) == 0 || !allowTools {
			res.Answer = chat.Message.Content

			// deterministic, so it is safe to run in workflow code
			missing := ungroundedNumbers(res.Answer, evidence)
			if len(missing) > 0 && res.GroundingRetries < MaxGroundingRetries && allowTools {
				res.GroundingRetries++
				messages = append(messages, llm.Message{
					Role:    "user",
					Content: fmt.Sprintf(groundingFeedback, strings.Join(missing, ", ")),
				})
				continue
			}

			res.UngroundedNumbers = append(res.UngroundedNumbers, missing...)
			return res, nil
		}

		// run this turn's tool calls in parallel, then append the results
		// in the order the model requested them
		futures := make([]workflow.Future, len(chat.Message.ToolCalls))
		for i, call := range chat.Message.ToolCalls {
			futures[i] = workflow.ExecuteActivity(ctx, a.CallTool, call)
		}

		for i, call := range chat.Message.ToolCalls {
			var tr ToolResult
			if err := futures[i].Get(ctx, &tr); err != nil {
				return res, err
			}

			messages = append(messages, llm.Message{Role: "tool", ToolName: call.Function.Name, Content: tr.Content})
			evidence = append(evidence, tr.Content)
			res.Steps = append(res.Steps, Step{
				Tool:    call.Function.Name,
				Args:    string(call.Function.Arguments),
				IsError: tr.IsError,
				Result:  truncate(tr.Content, 500),
			})
			for _, s := range tr.Sources {
				if !slices.Contains(res.Sources, s) {
					res.Sources = append(res.Sources, s)
				}
			}
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
