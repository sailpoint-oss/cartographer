package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripSourceLocations(t *testing.T) {
	m := map[string]any{
		"paths": map[string]any{
			"/users": map[string]any{
				"get": map[string]any{
					"operationId":    "listUsers",
					"x-source-file":  "/tmp/test/UserController.java",
					"x-source-line":  42,
					"x-source-column": 5,
				},
			},
		},
		"x-source-file": "/tmp/root",
	}

	StripSourceLocations(m)

	if _, ok := m["x-source-file"]; ok {
		t.Error("x-source-file should be removed from root")
	}
	paths := m["paths"].(map[string]any)
	users := paths["/users"].(map[string]any)
	get := users["get"].(map[string]any)
	if _, ok := get["x-source-file"]; ok {
		t.Error("x-source-file should be removed from nested map")
	}
	if get["operationId"] != "listUsers" {
		t.Error("non-source fields should be preserved")
	}
}

func TestNormalizeSourcePaths(t *testing.T) {
	m := map[string]any{
		"x-source-file": "/tmp/abc123/testdata/java-crud/UserController.java",
	}

	NormalizeSourcePaths(m)

	if m["x-source-file"] != "testdata/java-crud/UserController.java" {
		t.Errorf("got %v", m["x-source-file"])
	}
}

func TestAssertGoldenUpdate(t *testing.T) {
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "test.yaml")

	// Simulate -update by setting the flag
	old := *update
	*update = true
	defer func() { *update = old }()

	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "Test",
			"version": "1.0",
		},
	}

	AssertGolden(t, goldenPath, spec)

	content, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "openapi: 3.0.3") {
		t.Errorf("golden file should contain 'openapi: 3.0.3', got:\n%s", content)
	}
}

func TestAssertGoldenMatch(t *testing.T) {
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "test.yaml")

	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "Test",
			"version": "1.0",
		},
	}

	// First write golden
	old := *update
	*update = true
	AssertGolden(t, goldenPath, spec)
	*update = old

	// Then compare — should pass
	AssertGolden(t, goldenPath, spec)
}

func TestAssertGoldenWithSection(t *testing.T) {
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "section.yaml")

	spec := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"User": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	old := *update
	*update = true
	AssertGolden(t, goldenPath, spec, WithSection("components", "schemas", "User"))
	*update = old

	content, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	// Should contain User schema fields directly, not nested under components
	if !strings.Contains(string(content), "type: object") {
		t.Errorf("golden section should contain 'type: object', got:\n%s", content)
	}
}
