package extraction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractProject_UsesConfigAndRootRelativePaths(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".cartographer", "cartographer.yaml")
	writeTestFile(t, configPath, `
service:
  name: "Configured API"
  description: "from config"
  version: "2.1.0"
  language: "go"
  template: "atlas-go"
  team: "Platform"
  pathRewrites:
    - from: /internal
      to: /v1
  excludePaths:
    - /debug/**
`)

	var seen Options
	result, err := extractProjectWithRunner(ProjectOptions{
		RootDir:   root,
		ConfigDir: ".cartographer",
		Verbose:   true,
	}, func(opts Options) (*Result, error) {
		seen = opts
		return &Result{
			SpecMap: map[string]interface{}{
				"info": map[string]interface{}{},
				"paths": map[string]interface{}{
					"/internal/widgets": map[string]interface{}{},
					"/debug/health":     map[string]interface{}{},
				},
			},
			Operations: 1,
			Types:      2,
		}, nil
	})
	if err != nil {
		t.Fatalf("ExtractProject: %v", err)
	}

	if seen.RootDir != root {
		t.Fatalf("rootDir = %q, want %q", seen.RootDir, root)
	}
	if seen.Title != "Configured API" {
		t.Fatalf("title = %q", seen.Title)
	}
	if seen.Version != "2.1.0" {
		t.Fatalf("version = %q", seen.Version)
	}
	if seen.Description != "from config" {
		t.Fatalf("description = %q", seen.Description)
	}
	if !seen.Verbose {
		t.Fatal("expected verbose to propagate to extraction options")
	}

	if !result.HasConfig {
		t.Fatal("expected config to be detected")
	}
	if !result.ConfigApplied {
		t.Fatal("expected config shaping to be applied")
	}
	if result.OutputPath != filepath.Join(root, ".cartographer", "openapi.yaml") {
		t.Fatalf("outputPath = %q", result.OutputPath)
	}
	if result.ConfigPath != configPath {
		t.Fatalf("configPath = %q, want %q", result.ConfigPath, configPath)
	}

	info := result.SpecMap["info"].(map[string]interface{})
	if info["x-service-name"] != "Configured API" {
		t.Fatalf("x-service-name = %v", info["x-service-name"])
	}
	if info["x-service-team"] != "Platform" {
		t.Fatalf("x-service-team = %v", info["x-service-team"])
	}
	if info["x-service-template"] != "atlas-go" {
		t.Fatalf("x-service-template = %v", info["x-service-template"])
	}

	paths := result.SpecMap["paths"].(map[string]interface{})
	if _, ok := paths["/v1/widgets"]; !ok {
		t.Fatal("expected rewritten path to exist")
	}
	if _, ok := paths["/debug/health"]; ok {
		t.Fatal("expected excluded path to be removed")
	}
}

func TestExtractProject_UsesOverridesAndNormalizesOutput(t *testing.T) {
	root := t.TempDir()

	var seen Options
	result, err := extractProjectWithRunner(ProjectOptions{
		RootDir:     root,
		OutputPath:  "build/spec",
		Lang:        "go",
		Title:       "Override API",
		Version:     "9.9.9",
		Description: "from flag",
	}, func(opts Options) (*Result, error) {
		seen = opts
		return &Result{
			SpecMap: map[string]interface{}{
				"info":  map[string]interface{}{"title": opts.Title},
				"paths": map[string]interface{}{},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("ExtractProject: %v", err)
	}

	if seen.Template != "atlas-go" {
		t.Fatalf("template = %q, want atlas-go", seen.Template)
	}
	if seen.Title != "Override API" {
		t.Fatalf("title = %q", seen.Title)
	}
	if seen.Version != "9.9.9" {
		t.Fatalf("version = %q", seen.Version)
	}
	if seen.Description != "from flag" {
		t.Fatalf("description = %q", seen.Description)
	}
	if result.HasConfig {
		t.Fatal("did not expect config")
	}
	if result.ConfigApplied {
		t.Fatal("did not expect config shaping without a config file")
	}
	if result.OutputPath != filepath.Join(root, "build", "spec.yaml") {
		t.Fatalf("outputPath = %q", result.OutputPath)
	}
}

func TestExtractProject_WriteUsesResolvedOutputPath(t *testing.T) {
	root := t.TempDir()

	result, err := extractProjectWithRunner(ProjectOptions{
		RootDir:    root,
		OutputPath: "dist/spec.json",
		Lang:       "go",
	}, func(opts Options) (*Result, error) {
		return &Result{
			SpecMap: map[string]interface{}{
				"openapi": "3.1.0",
				"info":    map[string]interface{}{"title": opts.Title, "version": opts.Version},
				"paths":   map[string]interface{}{},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("ExtractProject: %v", err)
	}

	if err := result.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "dist", "spec.json"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected JSON output with trailing newline, got %q", string(data))
	}
}

func TestExtractProject_ReturnsHelpfulLanguageError(t *testing.T) {
	root := t.TempDir()

	_, err := extractProjectWithRunner(ProjectOptions{
		RootDir: root,
	}, func(opts Options) (*Result, error) {
		t.Fatalf("runner should not be called when language cannot be resolved")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected language resolution error")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
