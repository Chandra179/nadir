package chat

import (
	"encoding/json"
	"nadir/internal/edge/http/respond"
	"net/http"

	"nadir/internal/core/conversation/chat"
	"nadir/internal/core/retrieval/search"
	"nadir/internal/edge/http/contract"
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
func (h *Handlers) StartTurn(w http.ResponseWriter, r *http.Request) {
	var body startTurnRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		respond.JSON(w, http.StatusBadRequest, map[string]any{"error": "invalid turn request"})
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

	turn := h.chat.StartTurn(r.Context(), req)
	respond.JSON(w, http.StatusOK, contract.FromChatTurn(req, turn))
}

func toSearchFilter(filter *filterRequest) *search.Filter {
	if filter == nil {
		return nil
	}
	return &search.Filter{FilePath: filter.FilePath, Header: filter.Header, SourceSHA: filter.SourceSHA}
}
