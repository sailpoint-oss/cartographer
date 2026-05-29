package testutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStripSourceLocations(t *testing.T) {
	m := map[string]any{
		"paths": map[string]any{
			"/users": map[string]any{
				"get": map[string]any{
					"operationId": "listUsers",
					"x-source": map[string]any{
						"file":   "/tmp/test/UserController.java",
						"line":   42,
						"column": 5,
					},
				},
			},
		},
		"x-source": map[string]any{"file": "/tmp/root"},
	}

	StripSourceLocations(m)

	if _, ok := m["x-source"]; ok {
		t.Error("x-source should be removed from root when it only contains locations")
	}
	paths := m["paths"].(map[string]any)
	users := paths["/users"].(map[string]any)
	get := users["get"].(map[string]any)
	if _, ok := get["x-source"]; ok {
		t.Error("x-source should be removed from nested map when it only contains locations")
	}
	if get["operationId"] != "listUsers" {
		t.Error("non-source fields should be preserved")
	}
}

func TestNormalizeSourcePaths(t *testing.T) {
	m := map[string]any{
		"x-source": map[string]any{"file": "/tmp/abc123/testdata/java-crud/UserController.java"},
	}

	NormalizeSourcePaths(m)

	source := m["x-source"].(map[string]any)
	if source["file"] != "testdata/java-crud/UserController.java" {
		t.Errorf("got %v", source["file"])
	}
}

// enableGoldenUpdate sets -update for unit tests that write goldens; CI sets CI=true
// globally, which must not block intentional update-mode tests.
func enableGoldenUpdate(t *testing.T) func() {
	t.Helper()
	oldUpdate := *update
	oldCI, hadCI := os.LookupEnv("CI")
	*update = true
	t.Setenv("CI", "")
	return func() {
		*update = oldUpdate
		if hadCI {
			t.Setenv("CI", oldCI)
		} else {
			t.Setenv("CI", "")
		}
	}
}

func TestAssertGoldenUpdate(t *testing.T) {
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "test.yaml")

	defer enableGoldenUpdate(t)()

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

	defer enableGoldenUpdate(t)()
	AssertGolden(t, goldenPath, spec)

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

	defer enableGoldenUpdate(t)()
	AssertGolden(t, goldenPath, spec, WithSection("components", "schemas", "User"))

	content, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	// Should contain User schema fields directly, not nested under components
	if !strings.Contains(string(content), "type: object") {
		t.Errorf("golden section should contain 'type: object', got:\n%s", content)
	}
}

func TestStripDiagnostics(t *testing.T) {
	m := map[string]any{
		"info": map[string]any{
			"title": "Test",
			"x-cartographer-diagnostics": map[string]any{
				"operations": 3,
			},
		},
	}
	StripDiagnostics(m)
	info := m["info"].(map[string]any)
	if _, ok := info["x-cartographer-diagnostics"]; ok {
		t.Error("x-cartographer-diagnostics should be removed")
	}
	if info["title"] != "Test" {
		t.Error("title should be preserved")
	}
}

func TestDefaultE2ENormalizers(t *testing.T) {
	norms := DefaultE2ENormalizers()
	if len(norms) != 2 {
		t.Fatalf("expected 2 default normalizers, got %d", len(norms))
	}
	m := map[string]any{
		"info": map[string]any{
			"x-cartographer-diagnostics": map[string]any{"operations": 1},
		},
		"x-source": map[string]any{"file": "/tmp/x"},
	}
	for _, n := range norms {
		n(m)
	}
	if _, ok := m["x-source"]; ok {
		t.Error("StripSourceLocations should remove x-source")
	}
	info := m["info"].(map[string]any)
	if _, ok := info["x-cartographer-diagnostics"]; ok {
		t.Error("StripDiagnostics should remove diagnostics")
	}
}

func TestRunGoldenCases(t *testing.T) {
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "smoke.yaml")
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "Smoke",
			"version": "1.0",
		},
	}

	defer enableGoldenUpdate(t)()

	RunGoldenCases(t, dir, nil, []GoldenCase{
		{Name: "smoke", GoldenPath: "smoke.yaml", Spec: func(t *testing.T) map[string]any {
			return spec
		}},
	})

	if _, err := os.Stat(goldenPath); err != nil {
		t.Fatalf("golden file not written: %v", err)
	}
}

func TestYAMLMarshalStability(t *testing.T) {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "Stability",
			"version": "1.0",
		},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "listItems",
					"responses": map[string]any{
						"200": map[string]any{"description": "OK"},
					},
				},
			},
		},
	}
	var first, second bytes.Buffer
	if err := yaml.NewEncoder(&first).Encode(spec); err != nil {
		t.Fatal(err)
	}
	if err := yaml.NewEncoder(&second).Encode(spec); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Errorf("yaml marshal not stable:\nfirst:\n%s\nsecond:\n%s", first.Bytes(), second.Bytes())
	}
}

func TestGoldenUpdateEnabled(t *testing.T) {
	old := *update
	defer func() { *update = old }()

	*update = true
	t.Setenv("CI", "")
	if !goldenUpdateEnabled() {
		t.Fatal("expected update enabled when CI is unset")
	}

	t.Setenv("CI", "true")
	if goldenUpdateEnabled() {
		t.Fatal("expected update disabled when CI is set")
	}

	*update = false
	t.Setenv("CI", "")
	if goldenUpdateEnabled() {
		t.Fatal("expected update disabled without -update flag")
	}
}
