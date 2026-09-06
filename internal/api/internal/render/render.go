// Package render owns the embedded UI template set: the page shell and all
// htmx fragments under dashboard/. Templates are parsed once at startup so
// template errors surface at boot instead of on first request.
package render

import (
	"html/template"
	"io"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nadir/dashboard"
)

var uiTemplates = template.Must(template.ParseFS(dashboard.Files,
	"search.html",
	"partials/*.html",
))

// Engine renders the UI templates.
type Engine struct {
	log *zap.Logger
}

// New builds an Engine; a nil logger disables render-failure logging.
func New(log *zap.Logger) *Engine {
	if log == nil {
		log = zap.NewNop()
	}
	return &Engine{log: log}
}

// Execute writes a named template to w. Separate from HTML so tests can
// render fragments without a request.
func (e *Engine) Execute(w io.Writer, name string, data any) error {
	return uiTemplates.ExecuteTemplate(w, name, data)
}

// HTML writes a named template as an HTML response, setting the content
// type and logging render failures.
func (e *Engine) HTML(c *gin.Context, status int, name string, data any) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := e.Execute(c.Writer, name, data); err != nil {
		e.log.Error("render template failed", zap.String("template", name), zap.Error(err))
	}
}
