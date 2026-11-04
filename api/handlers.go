package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"github.com/daithyy/golabagent/workflows"
)

func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "golabagent-app",
	})
}

type askRequest struct {
	Question string `json:"question" binding:"required"`
}

type askResponse struct {
	WorkflowID string `json:"workflow_id"`
	workflows.AskResult
}

// Ask starts an AskWorkflow and waits for the answer. The workflow ID is
// returned so the full trace can be inspected in the Temporal UI
func (h *Handlers) Ask(c *gin.Context) {
	var req askRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	id := "ask-" + uuid.NewString()
	run, err := h.Temporal.ExecuteWorkflow(c.Request.Context(),
		client.StartWorkflowOptions{ID: id, TaskQueue: h.TaskQueue},
		workflows.AskWorkflow, workflows.AskInput{Question: req.Question})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var res workflows.AskResult
	if err := run.Get(c.Request.Context(), &res); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "workflow_id": id})
		return
	}

	c.JSON(http.StatusOK, askResponse{WorkflowID: id, AskResult: res})
}

type importRequest struct {
	Manifest string `json:"manifest"`
}

// Import runs an ImportWorkflow from the data sources manifest (default
// data/sources.json) and waits for it to finish: downloads, portfolio load
// and document ingestion
func (h *Handlers) Import(c *gin.Context) {
	req := importRequest{Manifest: "data/sources.json"}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	id := "import-" + uuid.NewString()
	run, err := h.Temporal.ExecuteWorkflow(c.Request.Context(),
		client.StartWorkflowOptions{ID: id, TaskQueue: h.TaskQueue},
		workflows.ImportWorkflow, workflows.ImportInput{Manifest: req.Manifest})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var res workflows.ImportResult
	if err := run.Get(c.Request.Context(), &res); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "workflow_id": id})
		return
	}

	c.JSON(http.StatusOK, gin.H{"workflow_id": id, "result": res})
}

// Search exposes hybrid retrieval directly, without the LLM, so retrieval
// quality can be inspected and evaluated on its own
func (h *Handlers) Search(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing q parameter"})
		return
	}

	k, err := strconv.Atoi(c.DefaultQuery("k", "5"))
	if err != nil || k < 1 || k > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "k must be between 1 and 50"})
		return
	}

	hits, err := h.Indexer.Search(c.Request.Context(), q, k)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"query": q, "hits": hits})
}
