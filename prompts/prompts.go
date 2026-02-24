// Package prompts provides embedded default prompt templates for MADFLOW agents.
// These templates are used when no custom prompts directory is found (e.g. on
// a fresh `madflow init`). The embedded files are also written out to the
// project's prompts/ directory so that users can inspect and customise them.
package prompts

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed superintendent.md
var superintendentMD []byte

//go:embed engineer.md
var engineerMD []byte

// defaultFiles maps filename -> content for all embedded prompt templates.
var defaultFiles = map[string][]byte{
	"superintendent.md": superintendentMD,
	"engineer.md":       engineerMD,
}

// ReadDefault returns the embedded content of the named prompt file
// (e.g. "superintendent.md").  It returns an error if the name is unknown.
func ReadDefault(name string) ([]byte, error) {
	data, ok := defaultFiles[name]
	if !ok {
		return nil, fmt.Errorf("no embedded default prompt for %q", name)
	}
	return data, nil
}

// WriteDefaults writes all embedded prompt files into dir, creating the
// directory if necessary.  Existing files are silently overwritten only when
// they do not yet exist so that user customisations are preserved.
func WriteDefaults(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create prompts dir: %w", err)
	}

	for name, data := range defaultFiles {
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			// File already exists – respect user customisation
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("write default prompt %s: %w", name, err)
		}
	}
	return nil
}
