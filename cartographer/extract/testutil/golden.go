// Package testutil provides golden-file (snapshot) testing utilities
// for OpenAPI spec generation tests.
package testutil

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "update golden files")

// Option configures golden file comparison.
type Option func(*config)

type config struct {
	section    []string
	normalizes []func(map[string]any)
}

// WithSection extracts a sub-path for partial comparison.
// e.g. WithSection("components", "schemas", "UserDTO") compares only that subtree.
func WithSection(keys ...string) Option {
	return func(c *config) {
		c.section = keys
	}
}

// NormalizeFunc modifies a spec map in-place for deterministic comparison.
type NormalizeFunc = func(map[string]any)

// WithNormalize adds normalization functions applied before comparison.
func WithNormalize(fns ...NormalizeFunc) Option {
	return func(c *config) {
		c.normalizes = append(c.normalizes, fns...)
	}
}

// StripSourceLocations removes x-source-file, x-source-line, x-source-column
// from all nested maps for deterministic comparison.
func StripSourceLocations(m map[string]any) {
	stripSourceLocsRecursive(m)
}

func stripSourceLocsRecursive(v any) {
	switch val := v.(type) {
	case map[string]any:
		delete(val, "x-source-file")
		delete(val, "x-source-line")
		delete(val, "x-source-column")
		for _, child := range val {
			stripSourceLocsRecursive(child)
		}
	case []any:
		for _, child := range val {
			stripSourceLocsRecursive(child)
		}
	}
}

// NormalizeSourcePaths strips temp directory prefixes from x-source-file values,
// keeping only the relative path from the last known directory segment.
func NormalizeSourcePaths(m map[string]any) {
	normalizeSourcePathsRecursive(m)
}

func normalizeSourcePathsRecursive(v any) {
	switch val := v.(type) {
	case map[string]any:
		if sf, ok := val["x-source-file"].(string); ok {
			// Keep only the path after the last "testdata/" segment
			if idx := strings.Index(sf, "testdata/"); idx >= 0 {
				val["x-source-file"] = sf[idx:]
			}
		}
		for _, child := range val {
			normalizeSourcePathsRecursive(child)
		}
	case []any:
		for _, child := range val {
			normalizeSourcePathsRecursive(child)
		}
	}
}

// AssertGolden compares actual spec output against a golden YAML file.
// If -update flag is set, writes the actual output as the new golden file.
// On mismatch, shows a unified diff.
func AssertGolden(t *testing.T, goldenPath string, actual map[string]any, opts ...Option) {
	t.Helper()

	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}

	// Apply normalizations
	for _, fn := range cfg.normalizes {
		fn(actual)
	}

	// Extract section if specified
	data := any(actual)
	for _, key := range cfg.section {
		m, ok := data.(map[string]any)
		if !ok {
			t.Fatalf("section key %q: parent is not a map", key)
		}
		data, ok = m[key]
		if !ok {
			t.Fatalf("section key %q not found in spec", key)
		}
	}

	actualYAML, err := yaml.Marshal(data)
	if err != nil {
		t.Fatalf("marshal actual: %v", err)
	}

	if *update {
		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, actualYAML, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Fatalf("golden file %s does not exist; run with -update to create it", goldenPath)
	}
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	if string(actualYAML) == string(expected) {
		return
	}

	diff := unifiedDiff(string(expected), string(actualYAML), goldenPath)
	t.Errorf("golden file mismatch: %s\n%s\nRun with -update to accept changes.", goldenPath, diff)
}

// unifiedDiff produces a simple line-by-line diff.
func unifiedDiff(expected, actual, name string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s (expected)\n", name)
	fmt.Fprintf(&b, "+++ %s (actual)\n", name)

	maxLen := len(expectedLines)
	if len(actualLines) > maxLen {
		maxLen = len(actualLines)
	}

	contextLines := 3
	type hunk struct {
		startE, startA int
		lines          []string
	}

	// Find differing line indices
	var diffIndices []int
	for i := 0; i < maxLen; i++ {
		e := ""
		a := ""
		if i < len(expectedLines) {
			e = expectedLines[i]
		}
		if i < len(actualLines) {
			a = actualLines[i]
		}
		if e != a {
			diffIndices = append(diffIndices, i)
		}
	}

	if len(diffIndices) == 0 {
		return ""
	}

	// Group into hunks with context
	printed := make(map[int]bool)
	for _, di := range diffIndices {
		start := di - contextLines
		if start < 0 {
			start = 0
		}
		end := di + contextLines + 1
		if end > maxLen {
			end = maxLen
		}
		for i := start; i < end; i++ {
			if printed[i] {
				continue
			}
			printed[i] = true
			e := ""
			a := ""
			if i < len(expectedLines) {
				e = expectedLines[i]
			}
			if i < len(actualLines) {
				a = actualLines[i]
			}
			if e != a {
				if i < len(expectedLines) {
					fmt.Fprintf(&b, "-%s\n", e)
				}
				if i < len(actualLines) {
					fmt.Fprintf(&b, "+%s\n", a)
				}
			} else {
				fmt.Fprintf(&b, " %s\n", e)
			}
		}
	}

	return b.String()
}
