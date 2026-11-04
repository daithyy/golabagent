// Package mcpserver exposes the agent's tools over the Model Context Protocol,
// so any MCP client (Claude Code, Claude Desktop, IDEs, other agents) can
// search the reference documents and query the underwriting portfolio
package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/daithyy/golabagent/tools"
)

func New(tb *tools.Toolbox, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "golabagent", Version: version}, nil)

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(server, &mcp.Tool{
		Name:        tools.SearchDocsName,
		Description: tb.SearchDocsDescription(),
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.SearchDocsArgs) (*mcp.CallToolResult, any, error) {
		return toResult(tb.SearchDocs(ctx, args))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        tools.QueryPortfolioName,
		Description: tb.QueryPortfolioDescription(),
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.QueryPortfolioArgs) (*mcp.CallToolResult, any, error) {
		return toResult(tb.QueryPortfolio(ctx, args))
	})

	return server
}

// Run serves MCP over stdin/stdout until the client disconnects. Nothing else
// may write to stdout in this mode; logs go to stderr
func Run(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}

// toResult reports both tool argument errors and execution errors as tool
// results with IsError set, as the MCP spec recommends, so the calling model
// can see what went wrong and correct itself
func toResult(res tools.Result, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		if _, ok := errors.AsType[*tools.ArgError](err); !ok {
			err = errors.New("internal error: " + err.Error())
		}
		r := &mcp.CallToolResult{}
		r.SetError(err)
		return r, nil, nil
	}

	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: res.Text}}}, nil, nil
}
