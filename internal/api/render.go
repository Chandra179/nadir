package api

import (
	"html/template"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/dashboard"
)

// uiTemplates holds every UI template — the page shell and all htmx
// fragments under dashboard/partials/ — parsed exactly once at startup so
// template errors surface at boot instead of on first request.
var uiTemplates = template.Must(template.ParseFS(dashboard.Files,
	"search.html",
	"partials/*.html",
))

// renderHTML writes a named template as an HTML response, setting the
// content type and logging render failures.
func (d *dependencies) renderHTML(c *gin.Context, status int, name string, data any) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := uiTemplates.ExecuteTemplate(c.Writer, name, data); err != nil {
		d.log.Error("render template failed", zap.String("template", name), zap.Error(err))
	}
}
