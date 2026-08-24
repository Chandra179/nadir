package api

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/ingest"
)

// Stats renders the stat-card row: collection size from the store, plus
// last-run info from the ingest tracker's history.
func (d *dependencies) Stats(c *gin.Context) {
	ctx := c.Request.Context()

	data := struct {
		Documents       int
		Chunks          int
		LastRun         string
		LastTarget      string
		LastFailed      bool
		LastFailedCount int
	}{LastRun: "—", LastTarget: "no runs yet"}

	if d.store != nil {
		stats, err := d.store.Stats(ctx)
		if err != nil {
			d.log.Warn("dashboard stats: store stats failed", zap.Error(err))
		} else {
			data.Documents = stats.Documents
			data.Chunks = stats.Chunks
		}
	}

	if history := d.ingest.History(); len(history) > 0 {
		last := history[0]
		data.LastRun = relativeTime(last.FinishedAt)
		data.LastTarget = displayTarget(last.Target)
		data.LastFailedCount = last.Failed
		data.LastFailed = last.Failed > 0
	}

	d.renderHTML(c, http.StatusOK, "stats", data)
}

type statusEventView struct {
	Path        string
	Detail      string
	Icon        string
	IconClass   string
	StripeClass string
}

// IngestStatus renders the live-run log panel, polled by the dashboard
// while (and just after) a run is in progress.
func (d *dependencies) IngestStatus(c *gin.Context) {
	status := d.ingest.Status()

	events := make([]statusEventView, 0, len(status.Events))
	for _, e := range status.Events {
		v := statusEventView{Path: e.Path, Detail: e.Detail}
		switch e.Status {
		case ingest.EventRunning:
			v.Icon, v.IconClass, v.StripeClass = "◐", "spin", "active"
		case ingest.EventProcessed:
			v.Icon, v.IconClass, v.StripeClass = "✓", "ok", "ok"
		case ingest.EventSkipped:
			v.Icon, v.IconClass, v.StripeClass = "–", "skip", "skip"
			if v.Detail == "" {
				v.Detail = "skipped"
			}
		case ingest.EventFailed:
			v.Icon, v.IconClass, v.StripeClass = "✗", "err", "err"
		}
		events = append(events, v)
	}

	data := struct {
		Running bool
		HasRun  bool
		Target  string
		Events  []statusEventView
	}{
		Running: status.Running,
		HasRun:  !status.StartedAt.IsZero(),
		Target:  displayTarget(status.Target),
		Events:  events,
	}

	d.renderHTML(c, http.StatusOK, "run-status", data)
}

type historyRowView struct {
	Target    string
	Processed int
	Skipped   int
	Badge     template.HTML
	When      string
}

// IngestHistory renders the recent-runs table body.
func (d *dependencies) IngestHistory(c *gin.Context) {
	runs := d.ingest.History()

	rows := make([]historyRowView, 0, len(runs))
	for _, r := range runs {
		var badge template.HTML
		switch {
		case r.Err != "":
			badge = template.HTML(`<span class="status-badge err"><span class="dot"></span>error</span>`)
		case r.Failed > 0:
			badge = template.HTML(fmt.Sprintf(`<span class="status-badge err"><span class="dot"></span>%d failed</span>`, r.Failed))
		case r.Processed == 0 && r.Skipped == 0:
			badge = template.HTML(`<span class="status-badge warn"><span class="dot"></span>no files</span>`)
		default:
			badge = template.HTML(`<span class="status-badge ok"><span class="dot"></span>done</span>`)
		}
		rows = append(rows, historyRowView{
			Target:    displayTarget(r.Target),
			Processed: r.Processed,
			Skipped:   r.Skipped,
			Badge:     badge,
			When:      relativeTime(r.FinishedAt),
		})
	}

	d.renderHTML(c, http.StatusOK, "run-history", struct{ Runs []historyRowView }{rows})
}

func displayTarget(target string) string {
	if target == "" {
		return "(full source.paths sweep)"
	}
	return target
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
