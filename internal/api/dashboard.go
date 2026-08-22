package api

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/internal/ingest"
)

type dashboardRoot struct {
	// Label is the root as configured (source.paths entry), shown on the
	// chip.
	Label string
	// Fill is the absolute path used as the input's value, so the chip
	// always resolves regardless of how many roots are configured.
	Fill string
}

// Dashboard renders the dashboard page (Tailwind + htmx via CDN, templated
// at request time so its "recent roots" chips reflect the live
// source.paths config); the interactive pieces poll the fragment endpoints
// below.
func (d *dependencies) Dashboard(c *gin.Context) {
	tmpl, err := template.ParseFiles("dashboard/index.html")
	if err != nil {
		d.log.Error("parse dashboard template failed", zap.Error(err))
		c.String(http.StatusInternalServerError, "dashboard unavailable")
		return
	}

	roots := make([]dashboardRoot, 0, len(d.sourceRoots))
	for _, r := range d.sourceRoots {
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = r
		}
		roots = append(roots, dashboardRoot{Label: r, Fill: abs})
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(c.Writer, struct{ Roots []dashboardRoot }{roots}); err != nil {
		d.log.Error("render dashboard template failed", zap.Error(err))
	}
}

var statsTmpl = template.Must(template.New("stats").Parse(`
<div class="card"><div class="label">Documents indexed</div><div class="value">{{.Documents}}</div></div>
<div class="card"><div class="label">Chunks in collection</div><div class="value">{{.Chunks}}</div></div>
<div class="card"><div class="label">Last run</div><div class="value accent">{{.LastRun}}</div><div class="delta">{{.LastTarget}}</div></div>
<div class="card"><div class="label">Failed last run</div><div class="value{{if .LastFailed}} danger{{end}}">{{.LastFailedCount}}</div></div>
`))

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

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := statsTmpl.Execute(c.Writer, data); err != nil {
		d.log.Error("render stats fragment failed", zap.Error(err))
	}
}

var statusTmpl = template.Must(template.New("status").Parse(`
{{if .HasRun}}
<div class="log-status-line">
<span class="log-status-target">{{if .Running}}Running · {{.Target}}{{else}}Last run · {{.Target}}{{end}}</span>
{{if .Running}}<span class="live-badge"><span class="ring"></span>live</span>{{else}}<span class="live-badge idle">idle</span>{{end}}
</div>
{{end}}
{{if .Events}}
<div class="log">
{{range .Events}}<div class="log-row s-{{.StripeClass}}"><span class="icon {{.IconClass}}">{{.Icon}}</span><span class="path"><b>{{.Path}}</b>{{if .Detail}} <span class="dim">{{.Detail}}</span>{{end}}</span></div>
{{end}}
</div>
{{else}}
<p class="empty">No ingest has run yet. Submit a path to get started.</p>
{{end}}
`))

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

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := statusTmpl.Execute(c.Writer, data); err != nil {
		d.log.Error("render status fragment failed", zap.Error(err))
	}
}

var historyTmpl = template.Must(template.New("history").Parse(`
{{if .Runs}}
{{range .Runs}}<tr>
<td class="target">{{.Target}}</td>
<td>{{.Processed}}</td>
<td>{{.Skipped}}</td>
<td>{{.Badge}}</td>
<td>{{.When}}</td>
</tr>
{{end}}
{{else}}
<tr><td colspan="5" class="empty-row">No runs yet.</td></tr>
{{end}}
`))

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

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := historyTmpl.Execute(c.Writer, struct{ Runs []historyRowView }{rows}); err != nil {
		d.log.Error("render history fragment failed", zap.Error(err))
	}
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
