package api

import (
	"go.temporal.io/sdk/client"

	"github.com/gin-gonic/gin"

	"github.com/daithyy/golabagent/rag"
)

type Handlers struct {
	Temporal  client.Client
	TaskQueue string
	Indexer   *rag.Indexer
}

func Setup(h *Handlers) *gin.Engine {
	r := gin.Default()

	r.GET("/health", HealthHandler)
	r.POST("/ask", h.Ask)
	r.POST("/import", h.Import)
	r.GET("/search", h.Search)

	return r
}
