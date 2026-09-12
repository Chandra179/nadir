package chat

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"nadir/internal/conversation/chat"
	"nadir/internal/retrieval/search"
	"nadir/internal/transport/http/contract"
)

type startTurnRequest struct {
	Query         string         `json:"query"`
	TopK          int            `json:"top_k"`
	Filter        *filterRequest `json:"filter,omitempty"`
	Generate      bool           `json:"generate"`
	SessionID     string         `json:"session_id,omitempty"`
	AttachedFiles []string       `json:"attached_files,omitempty"`
	Edit          bool           `json:"edit,omitempty"`
	EditSequence  int            `json:"edit_sequence,omitempty"`
}

type filterRequest struct {
	FilePath  string `json:"file_path"`
	Header    string `json:"header"`
	SourceSHA string `json:"source_sha"`
}

// StartTurn starts retrieval and, when requested, generation. The response
// is a stable JSON snapshot; live answer text is delivered by SSE.
func (h *Handlers) StartTurn(c *gin.Context) {
	var body startTurnRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid turn request"})
		return
	}
	topK := h.topK
	if body.TopK > 0 {
		topK = body.TopK
	}
	if topK > h.maxTopK {
		topK = h.maxTopK
	}

	req := chat.Request{
		Query:         body.Query,
		TopK:          topK,
		Filter:        toSearchFilter(body.Filter),
		Generate:      body.Generate,
		SessionID:     body.SessionID,
		AttachedFiles: body.AttachedFiles,
		Edit:          body.Edit,
		EditSequence:  body.EditSequence,
	}

	turn := h.chat.StartTurn(c.Request.Context(), req)
	c.JSON(http.StatusOK, contract.FromChatTurn(req, turn))
}

func toSearchFilter(filter *filterRequest) *search.Filter {
	if filter == nil {
		return nil
	}
	return &search.Filter{FilePath: filter.FilePath, Header: filter.Header, SourceSHA: filter.SourceSHA}
}
