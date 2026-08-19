package middleware

import (
	"github.com/gin-gonic/gin"
)

// Metrics returns Gin middleware that records a request-duration histogram
// and a request counter, labeled by method, route (c.FullPath(), not the
// raw path — keeps cardinality bounded), and status.
func (d *dependencies) Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {

	}
}
