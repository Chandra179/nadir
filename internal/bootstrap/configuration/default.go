package config

import (
	"bytes"
	_ "embed"
)

//go:embed config.yaml
var defaultYAML []byte

// Default parses the shipped configuration without touching the filesystem,
// for runtimes such as the browser that have none. Environment overrides still
// apply where the platform provides them.
func Default() (*Config, error) {
	return Parse(bytes.NewReader(defaultYAML))
}
