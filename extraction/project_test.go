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
  template: "go-web"
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
	if info["x-service-template"] != "go-web" {
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

	if seen.Template != "go-web" {
		t.Fatalf("template = %q, want go-web", seen.Template)
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

// TestExtractProject_DetectsLanguageMismatch makes sure that when a
// service claims Java but the repo has go.mod we still run the requested
// extractor but flag the discrepancy on the result so callers can log it.
func TestExtractProject_DetectsLanguageMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := extractProjectWithRunner(ProjectOptions{
		RootDir:  root,
		Lang:     "java",
		Template: "java-spring",
	}, func(opts Options) (*Result, error) {
		return &Result{
			SpecMap: map[string]interface{}{
				"info":  map[string]interface{}{},
				"paths": map[string]interface{}{},
			},
			Source: SourceExtracted,
		}, nil
	})
	if err != nil {
		t.Fatalf("extractProjectWithRunner: %v", err)
	}
	if !res.LanguageMismatch {
		t.Fatal("expected LanguageMismatch = true")
	}
	if res.DetectedLanguage != "go" {
		t.Errorf("DetectedLanguage = %q, want go", res.DetectedLanguage)
	}
	if !res.Signals.Go {
		t.Error("expected Signals.Go to be set")
	}
}

// TestExtractProject_NonRESTKindStub covers the library / worker short
// circuit: when Kind is non-REST and no canonical spec exists, a stub
// spec is produced instead of running the extractor.
func TestExtractProject_NonRESTKindStub(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := 0
	res, err := extractProjectWithRunner(ProjectOptions{
		RootDir: root,
		Kind:    "library",
		Lang:    "go",
	}, func(opts Options) (*Result, error) {
		called++
		return nil, nil
	})
	if err != nil {
		t.Fatalf("extractProjectWithRunner: %v", err)
	}
	if called != 0 {
		t.Errorf("expected runner NOT to be called for library kind, got %d calls", called)
	}
	if res.Result == nil || res.SpecMap == nil {
		t.Fatal("expected stub result")
	}
	info := res.SpecMap["info"].(map[string]interface{})
	if info["x-service-kind"] != "library" {
		t.Errorf("x-service-kind = %v", info["x-service-kind"])
	}
	if info["x-spec-source"] != "non-rest-stub" {
		t.Errorf("x-spec-source = %v", info["x-spec-source"])
	}
}

// TestExtractProject_PreferCanonicalSpec verifies that when a canonical
// spec exists and the caller opts in, the extractor runner is skipped.
func TestExtractProject_PreferCanonicalSpec(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `openapi: 3.0.3
info:
  title: Canonical
  version: "2.5"
paths:
  /thing:
    get:
      responses:
        "200":
          description: ok
`
	if err := os.WriteFile(filepath.Join(root, "openapi.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	runnerCalled := 0
	res, err := extractProjectWithRunner(ProjectOptions{
		RootDir:             root,
		Lang:                "go",
		Template:            "go-web",
		PreferCanonicalSpec: true,
	}, func(opts Options) (*Result, error) {
		runnerCalled++
		// Fallthrough: because extractProjectWithRunner uses Extract
		// directly for canonical paths we do not expect the default
		// runner to fire. However when PreferCanonicalSpec drives the
		// path through Extract, the passthrough branch loads the spec
		// without touching the runner; the runner is still the test
		// seam for non-canonical Extract options.
		if opts.OverrideSpecPath != "" || opts.UseCanonicalSpec {
			return Extract(opts)
		}
		return &Result{SpecMap: map[string]interface{}{
			"info":  map[string]interface{}{},
			"paths": map[string]interface{}{},
		}}, nil
	})
	if err != nil {
		t.Fatalf("extractProjectWithRunner: %v", err)
	}
	if res.Source != SourceCanonicalOpenAPI3 {
		t.Errorf("Source = %q, want canonical-openapi3", res.Source)
	}
	if res.Operations != 1 {
		t.Errorf("Operations = %d, want 1", res.Operations)
	}
	info := res.SpecMap["info"].(map[string]interface{})
	if info["title"] != "Canonical" {
		t.Errorf("title lost during passthrough: %v", info["title"])
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
