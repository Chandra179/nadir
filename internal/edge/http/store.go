package api

import (
	"fmt"
	"nadir/internal/edge/http/respond"
	"net/http"

	"log/slog"
)

type deleteAllResponse struct {
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

// DeleteAllData permanently removes every indexed chunk by delegating to the
// store's DeleteAll, which publishes an empty collection generation and then
// retires the previous one. Cache invalidation on reset is enforced by the
// reset coordinator wired in internal/core/documents/indexing.
func (d *dependencies) DeleteAllData(w http.ResponseWriter, r *http.Request) {
	if d.reset == nil {
		d.log.Error("delete all data unavailable")
		respond.JSON(w, http.StatusServiceUnavailable, deleteAllResponse{Error: "delete is unavailable"})
		return
	}
	if err := d.reset(r.Context()); err != nil {
		d.log.Error("delete all data failed", slog.Any("error", err))
		msg := fmt.Sprintf("delete failed: %s", err.Error())
		respond.JSON(w, http.StatusInternalServerError, deleteAllResponse{Error: msg})
		return
	}
	d.importMu.Lock()
	d.lastImport = nil
	d.importMu.Unlock()

	respond.JSON(w, http.StatusOK, deleteAllResponse{Deleted: true})
}
