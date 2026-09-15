// Package contractcheck validates the dashboard's central TypeScript wire
// mirror against the repository-owned OpenAPI schemas. It intentionally keeps
// the check small and dependency-light instead of adding a code-generation
// toolchain for a handful of stable DTOs.
package contractcheck

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type document struct {
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

type schema struct {
	Ref        string            `yaml:"$ref"`
	Type       string            `yaml:"type"`
	Properties map[string]schema `yaml:"properties"`
	Required   []string          `yaml:"required"`
	Items      *schema           `yaml:"items"`
}

type field struct {
	optional bool
	typeName string
}

var typeStart = regexp.MustCompile(`^export type ([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\{\s*$`)
var fieldLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(\?)?\s*:\s*(.+);$`)

// Check validates all TypeScript exported object types against OpenAPI
// component schemas with the same name, including fields, optionality, and
// primitive/reference shapes.
func Check(openAPIPath, typescriptPath string) error {
	openAPIData, err := os.ReadFile(openAPIPath)
	if err != nil {
		return fmt.Errorf("read OpenAPI contract: %w", err)
	}
	var api document
	if err := yaml.Unmarshal(openAPIData, &api); err != nil {
		return fmt.Errorf("parse OpenAPI contract: %w", err)
	}
	typescriptData, err := os.ReadFile(typescriptPath)
	if err != nil {
		return fmt.Errorf("read TypeScript contract: %w", err)
	}
	types, err := parseTypeScript(string(typescriptData))
	if err != nil {
		return err
	}
	if len(types) != len(api.Components.Schemas) {
		return fmt.Errorf("contract type count differs: TypeScript=%d OpenAPI=%d", len(types), len(api.Components.Schemas))
	}
	for name := range api.Components.Schemas {
		if _, ok := types[name]; !ok {
			return fmt.Errorf("OpenAPI schema %q is missing from TypeScript mirror", name)
		}
	}
	for name := range types {
		if _, ok := api.Components.Schemas[name]; !ok {
			return fmt.Errorf("TypeScript type %q is missing from OpenAPI schemas", name)
		}
	}
	for name, fields := range types {
		if err := compareSchema(name, fields, api.Components.Schemas[name]); err != nil {
			return err
		}
	}
	return nil
}

func parseTypeScript(source string) (map[string]map[string]field, error) {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	result := make(map[string]map[string]field)
	for i := 0; i < len(lines); i++ {
		match := typeStart.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if match == nil {
			continue
		}
		name := match[1]
		fields := make(map[string]field)
		closed := false
		for i++; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if line == "};" {
				closed = true
				break
			}
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			match := fieldLine.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("cannot parse TypeScript field in %s: %q", name, line)
			}
			fields[match[1]] = field{optional: match[2] == "?", typeName: strings.TrimSpace(match[3])}
		}
		if !closed {
			return nil, fmt.Errorf("TypeScript type %q is not closed", name)
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("TypeScript type %q is declared more than once", name)
		}
		result[name] = fields
	}
	return result, nil
}

func compareSchema(name string, fields map[string]field, schema schema) error {
	if schema.Type != "object" {
		return fmt.Errorf("OpenAPI schema %q must be an object", name)
	}
	required := make(map[string]bool, len(schema.Required))
	for _, property := range schema.Required {
		required[property] = true
	}
	if len(fields) != len(schema.Properties) {
		return fmt.Errorf("schema %q field count differs: TypeScript=%d OpenAPI=%d", name, len(fields), len(schema.Properties))
	}
	for property, tsField := range fields {
		openAPIField, ok := schema.Properties[property]
		if !ok {
			return fmt.Errorf("schema %q property %q is missing from OpenAPI", name, property)
		}
		if tsField.optional == required[property] {
			return fmt.Errorf("schema %q property %q optionality differs", name, property)
		}
		wantType, err := schemaType(openAPIField)
		if err != nil {
			return fmt.Errorf("schema %q property %q: %w", name, property, err)
		}
		if tsField.typeName != wantType {
			return fmt.Errorf("schema %q property %q type differs: TypeScript=%q OpenAPI=%q", name, property, tsField.typeName, wantType)
		}
	}
	for property := range required {
		if _, ok := fields[property]; !ok {
			return fmt.Errorf("schema %q required property %q is missing from TypeScript", name, property)
		}
	}
	return nil
}

func schemaType(value schema) (string, error) {
	if value.Ref != "" {
		const prefix = "#/components/schemas/"
		if !strings.HasPrefix(value.Ref, prefix) {
			return "", fmt.Errorf("unsupported reference %q", value.Ref)
		}
		return strings.TrimPrefix(value.Ref, prefix), nil
	}
	if value.Type == "array" {
		if value.Items == nil {
			return "", fmt.Errorf("array has no item schema")
		}
		itemType, err := schemaType(*value.Items)
		if err != nil {
			return "", err
		}
		return itemType + "[]", nil
	}
	switch value.Type {
	case "string":
		return "string", nil
	case "integer", "number":
		return "number", nil
	case "boolean":
		return "boolean", nil
	default:
		return "", fmt.Errorf("unsupported OpenAPI type %q", value.Type)
	}
}
