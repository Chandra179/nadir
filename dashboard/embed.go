// Package dashboard holds the UI assets for the chat/dashboard interface:
// the page shell (search.html) and the htmx fragments under partials/.
// The embedded FS is parsed once by internal/api at startup.
package dashboard

import "embed"

//go:embed search.html partials/*.html
var Files embed.FS
