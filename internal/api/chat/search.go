package chat

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nadir/internal/chat"
	"nadir/internal/store"
)

// RetrievalSearch starts a chat turn (search + rewrite capture; generation
// runs under the chat service's supervisor) and renders one appended turn:
// the question, the retrieval tool call, the Think trace, and an SSE
// placeholder pointing at the turn's event stream.
func (h *Handlers) RetrievalSearch(c *gin.Context) {
	topK := h.topK
	if v, err := strconv.Atoi(c.PostForm("top_k")); err == nil && v > 0 {
		topK = v
	}

	var filter *store.SearchFilter
	if filePath, header, sha := c.PostForm("file_path"), c.PostForm("header"), c.PostForm("source_sha"); filePath != "" || header != "" || sha != "" {
		filter = &store.SearchFilter{FilePath: filePath, Header: header, SourceSHA: sha}
	}

	req := chat.Request{
		Query:         c.PostForm("query"),
		TopK:          topK,
		Filter:        filter,
		Generate:      c.PostForm("generate") == "on",
		SessionID:     c.PostForm("session_id"),
		AttachedFiles: attachedFileNames(c.PostForm("attached_files")),
	}

	turn := h.chat.StartTurn(c.Request.Context(), req)

	// A session id is minted by the chat service on the first turn of a
	// conversation (never client-generated, so a client can't spoof/collide
	// another session's id) and handed back via a response header for the
	// composer to carry on subsequent posts.
	if turn.SessionID != "" {
		c.Header("X-Nadir-Session-Id", turn.SessionID)
	}

	h.render.HTML(c, http.StatusOK, "turn", turnViewFromResult(req, turn))
}

// attachedFileNames splits the comma-joined "attached_files" field — names
// of recently imported files shown above the question. Display-only: the
// files were already ingested and don't scope this search.
func attachedFileNames(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			names = append(names, p)
		}
	}
	return names
}
