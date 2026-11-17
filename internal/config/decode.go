package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// decodeConfig parses JSON or YAML into c with the SAME strictness for both: unknown keys are errors.
// YAML is converted to generic data, re-encoded as JSON and decoded through encoding/json with
// DisallowUnknownFields, so struct tags stay the single source of truth. (yaml.v3 itself rejects duplicate keys.)
func decodeConfig(data []byte, c *Config) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil // an empty file means "all defaults"
	}
	if trimmed[0] != '{' {
		var generic map[string]any
		if err := yaml.Unmarshal(data, &generic); err != nil {
			return fmt.Errorf("invalid YAML: %w", err)
		}
		b, err := json.Marshal(generic)
		if err != nil {
			return fmt.Errorf("unsupported YAML construct: %w", err)
		}
		trimmed = b
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	return dec.Decode(c)
}
