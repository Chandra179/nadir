package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ReadinessCheck is the HTTP representation of one dependency's operational
// state. The platform readiness Module is mapped to this contract by the
// composition root, keeping HTTP independent of platform policy types.
type ReadinessCheck struct {
	Ready       bool   `json:"ready"`
	Model       string `json:"model,omitempty"`
	LoadedModel string `json:"loaded_model,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Device      string `json:"device,omitempty"`
	Error       string `json:"error,omitempty"`
	Details     string `json:"details,omitempty"`
}

// ReadinessReport is the HTTP representation of the complete dependency
// readiness result.
type ReadinessReport struct {
	Ready  bool                      `json:"ready"`
	Checks map[string]ReadinessCheck `json:"checks"`
}

// ReadinessFunc lets the HTTP transport consume a prebuilt platform readiness
// checker without depending on concrete infrastructure packages.
type ReadinessFunc func(context.Context) ReadinessReport

func (d *dependencies) readinessHandler(c *gin.Context) {
	report := ReadinessReport{Ready: true, Checks: map[string]ReadinessCheck{
		"application": {Ready: true},
	}}
	if d.readiness != nil {
		ctx := c.Request.Context()
		if d.readinessTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d.readinessTimeout)
			defer cancel()
		}
		report = d.readiness(ctx)
	}

	status := http.StatusOK
	if !report.Ready {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, report)
}

// readinessTimeoutDefault is only used by direct transport tests that do not
// construct the production configuration. Loaded configurations set the
// value explicitly through HTTPConfig.ReadinessTimeout.
const readinessTimeoutDefault = 15 * time.Second
