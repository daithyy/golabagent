package workflows

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/daithyy/golabagent/llm"
)

func toolCall(name, args string) llm.ToolCall {
	return llm.ToolCall{Function: llm.ToolCallFunction{Name: name, Arguments: json.RawMessage(args)}}
}

func TestAskWorkflowRunsToolsThenAnswers(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	var a *Activities

	sqlCall := toolCall("query_portfolio_db", `{"sql":"SELECT area, SUM(claim_count) / SUM(exposure) FROM policies GROUP BY 1"}`)
	searchCall := toolCall("search_docs", `{"query":"bonus-malus maximum bonus"}`)

	// Turn 1: the model asks for both tools at once.
	env.OnActivity(a.Chat, mock.Anything, mock.MatchedBy(func(in ChatInput) bool { return len(in.Messages) == 2 })).
		Return(ChatResult{
			Message:      llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{sqlCall, searchCall}},
			PromptTokens: 100, CompletionTokens: 20,
		}, nil).Once()

	env.OnActivity(a.CallTool, mock.Anything, sqlCall).
		Return(ToolResult{Content: "area | frequency\nF | 0.14\n"}, nil).Once()
	env.OnActivity(a.CallTool, mock.Anything, searchCall).
		Return(ToolResult{Content: "[1] source: bonus-malus-rules.md\n...", Sources: []string{"bonus-malus-rules.md"}}, nil).Once()

	// Turn 2: system, user, assistant and two tool results are in context.
	env.OnActivity(a.Chat, mock.Anything, mock.MatchedBy(func(in ChatInput) bool {
		return len(in.Messages) == 5 && in.Messages[3].Role == "tool" && in.Messages[3].ToolName == "query_portfolio_db"
	})).Return(ChatResult{
		Message:      llm.Message{Role: "assistant", Content: "Area F has the highest frequency, 0.14 [bonus-malus-rules.md]."},
		PromptTokens: 300, CompletionTokens: 30,
	}, nil).Once()

	env.ExecuteWorkflow(AskWorkflow, AskInput{Question: "Which area has the highest claim frequency?"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var res AskResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.Equal(t, "Area F has the highest frequency, 0.14 [bonus-malus-rules.md].", res.Answer)
	require.Equal(t, []string{"bonus-malus-rules.md"}, res.Sources)
	require.Len(t, res.Steps, 2)
	require.Equal(t, 400, res.PromptTokens)
	require.Equal(t, 50, res.CompletionTokens)
	require.False(t, res.HitStepLimit)
	env.AssertExpectations(t)
}

func TestAskWorkflowForcesAnswerAtStepLimit(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	var a *Activities

	loop := toolCall("search_docs", `{"query":"again"}`)

	// The model keeps calling tools while they are allowed...
	env.OnActivity(a.Chat, mock.Anything, mock.MatchedBy(func(in ChatInput) bool { return in.AllowTools })).
		Return(ChatResult{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{loop}}}, nil).Times(MaxSteps)
	env.OnActivity(a.CallTool, mock.Anything, loop).
		Return(ToolResult{Content: "nothing new"}, nil).Times(MaxSteps)
	// ...then must answer when tools are withdrawn.
	env.OnActivity(a.Chat, mock.Anything, mock.MatchedBy(func(in ChatInput) bool { return !in.AllowTools })).
		Return(ChatResult{Message: llm.Message{Role: "assistant", Content: "I could not find an answer."}}, nil).Once()

	env.ExecuteWorkflow(AskWorkflow, AskInput{Question: "?"})

	require.NoError(t, env.GetWorkflowError())
	var res AskResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.True(t, res.HitStepLimit)
	require.Equal(t, "I could not find an answer.", res.Answer)
	require.Len(t, res.Steps, MaxSteps)
	env.AssertExpectations(t)
}

func TestAskWorkflowSendsBackUngroundedNumbers(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	var a *Activities

	query := toolCall("query_portfolio_db", `{"sql":"SELECT SUM(total_claim_amount) / SUM(exposure) FROM policies"}`)

	// Turn 1: the model answers with a figure it never queried.
	env.OnActivity(a.Chat, mock.Anything, mock.MatchedBy(func(in ChatInput) bool { return len(in.Messages) == 2 })).
		Return(ChatResult{Message: llm.Message{Role: "assistant", Content: "Pure premium is about €39.6."}}, nil).Once()

	// Turn 2: it is told which numbers are unsupported, and runs a query.
	env.OnActivity(a.Chat, mock.Anything, mock.MatchedBy(func(in ChatInput) bool {
		last := in.Messages[len(in.Messages)-1]
		return len(in.Messages) == 4 && last.Role == "user" && strings.Contains(last.Content, "39.6")
	})).Return(ChatResult{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{query}}}, nil).Once()
	env.OnActivity(a.CallTool, mock.Anything, query).
		Return(ToolResult{Content: "?column?\n167.1125\n"}, nil).Once()

	// Turn 3: a grounded answer.
	env.OnActivity(a.Chat, mock.Anything, mock.MatchedBy(func(in ChatInput) bool { return len(in.Messages) == 6 })).
		Return(ChatResult{Message: llm.Message{Role: "assistant", Content: "Pure premium is €167 per policy-year."}}, nil).Once()

	env.ExecuteWorkflow(AskWorkflow, AskInput{Question: "What is our pure premium?"})

	require.NoError(t, env.GetWorkflowError())
	var res AskResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.Equal(t, "Pure premium is €167 per policy-year.", res.Answer)
	require.Equal(t, 1, res.GroundingRetries)
	require.Empty(t, res.UngroundedNumbers)
	env.AssertExpectations(t)
}

func TestAskWorkflowReportsNumbersStillUngrounded(t *testing.T) {
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	var a *Activities

	// The model repeats the unsupported figure after being sent back once.
	env.OnActivity(a.Chat, mock.Anything, mock.Anything).
		Return(ChatResult{Message: llm.Message{Role: "assistant", Content: "Pure premium is about €39.6."}}, nil).Twice()

	env.ExecuteWorkflow(AskWorkflow, AskInput{Question: "What is our pure premium?"})

	require.NoError(t, env.GetWorkflowError())
	var res AskResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.Equal(t, 1, res.GroundingRetries)
	require.Equal(t, []string{"39.6"}, res.UngroundedNumbers)
	env.AssertExpectations(t)
}
