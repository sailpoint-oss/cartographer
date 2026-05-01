package extraction

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/csharpextract"
	"github.com/sailpoint-oss/cartographer/extract/goextract"
	"github.com/sailpoint-oss/cartographer/extract/javaextract"
	"github.com/sailpoint-oss/cartographer/extract/pythonextract"
	"github.com/sailpoint-oss/cartographer/extract/specgen"
	"github.com/sailpoint-oss/cartographer/extract/tsextract"
)

// ErrLanguageUnsupported is wrapped by Extract when Options.Lang points at
// a language cartographer knows about but cannot parse yet (currently:
// csharp). Downstream tooling uses errors.Is to surface a dedicated status
// instead of treating unsupported languages as generic extraction failures.
var ErrLanguageUnsupported = errors.New("language not supported by cartographer extractor")

// Options holds all parameters needed for a single extraction run.
type Options struct {
	Lang        string
	Template    string
	RootDir     string
	Title       string
	Version     string
	Description string
	Verbose     bool
	// OverrideSpecPath, when non-empty, causes Extract to skip source
	// extraction entirely and load the given file as the spec. The file
	// may be OpenAPI 3 (used as-is) or Swagger 2 (auto-converted via
	// the in-house structural converter in canonical.go). This is how
	// meridian forwards an in-repo sailpoint-api.yaml / docs/swagger.yaml
	// without re-parsing sources.
	OverrideSpecPath string
	// UseCanonicalSpec, when true, causes Extract to auto-discover an
	// in-repo canonical spec (sailpoint-api.yaml, docs/swagger.yaml, etc.)
	// before falling back to source extraction. When a canonical spec is
	// found the Result's Source field is populated accordingly.
	UseCanonicalSpec bool
}

// SpecSource records where the returned spec came from. Callers use this
// to decide how much to trust the sourcemap, whether to run post-extraction
// shaping, and which warnings to emit.
type SpecSource string

const (
	// SourceExtracted means the spec was produced by one of the
	// language-specific extractors (Go/Java/TS/Python).
	SourceExtracted SpecSource = "extracted"
	// SourceCanonicalOpenAPI3 means the spec was an in-repo OpenAPI 3.x
	// file loaded via the canonical-spec passthrough.
	SourceCanonicalOpenAPI3 SpecSource = "canonical-openapi3"
	// SourceCanonicalSwagger2 means the spec was an in-repo Swagger 2.0
	// file that was auto-converted to OpenAPI 3 via the in-house
	// structural converter in canonical.go.
	SourceCanonicalSwagger2 SpecSource = "canonical-swagger2"
)

// Result holds the output of a single extraction run.
type Result struct {
	SpecMap    map[string]interface{}
	Operations int
	Types      int
	// Source records which pipeline produced SpecMap.
	Source SpecSource
	// CanonicalPath is the absolute file path of the in-repo spec used,
	// or empty when Source == SourceExtracted.
	CanonicalPath string
}

// Extract performs extraction for a single service. When OverrideSpecPath
// or UseCanonicalSpec selects an in-repo canonical spec that file is used
// directly; otherwise source-code extraction runs based on opts.Lang.
func Extract(opts Options) (*Result, error) {
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}
	if opts.Template == "" {
		opts.Template = InferTemplate(opts.Lang)
	}

	// Canonical-spec passthrough wins over source extraction when the
	// caller explicitly points at a file or asks us to auto-discover
	// one. Non-REST services, openapi-generator split modules, and
	// services that hand-author their spec all rely on this branch.
	if opts.OverrideSpecPath != "" {
		return doCanonicalExtract(opts, opts.OverrideSpecPath)
	}
	if opts.UseCanonicalSpec {
		if candidate := firstCanonicalSpecPath(opts.RootDir); candidate != "" {
			return doCanonicalExtract(opts, candidate)
		}
	}

	switch opts.Lang {
	case "go":
		return doGoExtract(opts)
	case "java":
		return doJavaExtract(opts)
	case "typescript", "ts":
		return doTypeScriptExtract(opts)
	case "python", "py":
		return doPythonExtract(opts)
	case "csharp", "cs", "dotnet":
		return doCSharpExtract(opts)
	default:
		return nil, fmt.Errorf("unsupported language: %s (supported: go, java, typescript, python, csharp)", opts.Lang)
	}
}

// InferTemplate returns the default template for a given language.
func InferTemplate(lang string) string {
	switch lang {
	case "go":
		return "atlas-go"
	case "java":
		return "atlas-boot"
	case "typescript", "ts":
		return "saas-atlasjs"
	case "python", "py":
		return "atlas-python"
	case "csharp", "cs", "dotnet":
		return "atlas-csharp"
	default:
		return ""
	}
}

// FindCSharpSourceDirs finds conventional C# source directories.
func FindCSharpSourceDirs(root string) []string {
	candidates := []string{
		filepath.Join(root, "src"),
		filepath.Join(root, "Source"),
		filepath.Join(root, "source"),
	}
	var found []string
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found = append(found, dir)
			break
		}
	}
	if len(found) == 0 {
		found = append(found, root)
	}
	return found
}

// FindJavaSourceDirs finds conventional Java source directories in a project.
func FindJavaSourceDirs(root string) []string {
	candidates := []string{
		filepath.Join(root, "src", "main", "java"),
		filepath.Join(root, "src", "main"),
		filepath.Join(root, "app", "src", "main", "java"),
	}
	var found []string
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found = append(found, dir)
			break
		}
	}
	return found
}

// FindTypeScriptSourceDirs finds conventional TypeScript source directories.
func FindTypeScriptSourceDirs(root string) []string {
	candidates := []string{
		filepath.Join(root, "src"),
		filepath.Join(root, "lib"),
		filepath.Join(root, "app"),
	}
	var found []string
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found = append(found, dir)
			break
		}
	}
	return found
}

// FindPythonSourceDirs finds conventional Python source directories.
//
// Atlas-python services generally follow one of these layouts:
//
//   - src-layout              (pyproject.toml + src/<pkg>/)
//   - flat layout             (pyproject.toml + <pkg>/)
//   - app-style               (app.py in root, modules alongside)
//
// We pick the first that exists; if none match we fall back to the project
// root, which the walker will then recursively scan.
func FindPythonSourceDirs(root string) []string {
	candidates := []string{
		filepath.Join(root, "src"),
		filepath.Join(root, "app"),
		filepath.Join(root, "server"),
	}
	var found []string
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			found = append(found, dir)
			break
		}
	}
	if len(found) == 0 {
		// Look for a top-level package directory (one with __init__.py) inside
		// the root. This handles flat-layout services like
		//   <root>/<pkg>/__init__.py
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				if name == "tests" || name == "test" || name == "docs" || strings.HasPrefix(name, ".") {
					continue
				}
				init := filepath.Join(root, name, "__init__.py")
				if _, err := os.Stat(init); err == nil {
					found = append(found, filepath.Join(root, name))
					break
				}
			}
		}
	}
	return found
}

func doGoExtract(opts Options) (*Result, error) {
	// Resolve the actual Go module root. Repositories like pag keep
	// their Go sources under go/ with non-Go siblings at the repo
	// root, so a naive "<root>/..." pattern would miss them.
	modRoot := FindGoModuleRoot(opts.RootDir)
	if modRoot == "" {
		modRoot = opts.RootDir
	}
	pkg := modRoot
	if pkg != "." && pkg != "./..." {
		pkg = filepath.Join(pkg, "...")
	} else if pkg == "." {
		pkg = "./..."
	}

	cfg := goextract.Config{
		PackagePatterns: []string{pkg},
		Verbose:         opts.Verbose,
		IncludeTests:    false,
	}

	extractor := goextract.New(cfg)
	metadata, err := extractor.Extract(cfg)
	if err != nil {
		return nil, fmt.Errorf("go extraction: %w", err)
	}

	genCfg := specgen.Config{
		Title:           opts.Title,
		Version:         opts.Version,
		OpenAPIVersion:  "3.2",
		IncludeWebhooks: true,
		TreeShake:       true,
	}

	specMap := specgen.Generate(metadata, extractor, genCfg)

	if info, ok := specMap["info"].(map[string]interface{}); ok {
		info["x-service-template"] = opts.Template
	}

	return &Result{
		SpecMap:    specMap,
		Operations: len(metadata.Operations),
		Types:      len(metadata.Types),
		Source:     SourceExtracted,
	}, nil
}

// doCanonicalExtract loads an in-repo spec file, converts from Swagger 2
// when necessary, applies the service template metadata, and returns a
// Result as if an extractor had produced it. The resulting SpecMap is a
// plain OpenAPI 3 map that downstream shaping (ApplyConfig) can patch in
// place.
func doCanonicalExtract(opts Options, path string) (*Result, error) {
	spec, err := LoadCanonicalSpec(path, opts.RootDir)
	if err != nil {
		return nil, fmt.Errorf("canonical spec %s: %w", path, err)
	}
	// Seed info.title / info.version from options when the file omitted
	// them so downstream consumers see consistent values across services.
	info, _ := spec.SpecMap["info"].(map[string]interface{})
	if info == nil {
		info = map[string]interface{}{}
	}
	if _, ok := info["title"]; !ok && opts.Title != "" {
		info["title"] = opts.Title
	}
	if _, ok := info["version"]; !ok && opts.Version != "" {
		info["version"] = opts.Version
	}
	if opts.Description != "" {
		if _, ok := info["description"]; !ok {
			info["description"] = opts.Description
		}
	}
	if opts.Template != "" {
		info["x-service-template"] = opts.Template
	}
	info["x-spec-source"] = string(sourceFor(spec.Kind))
	if spec.RelPath != "" {
		info["x-spec-source-path"] = spec.RelPath
	} else {
		info["x-spec-source-path"] = path
	}
	spec.SpecMap["info"] = info

	return &Result{
		SpecMap:       spec.SpecMap,
		Operations:    spec.Operations,
		Types:         spec.Types,
		Source:        sourceFor(spec.Kind),
		CanonicalPath: spec.Path,
	}, nil
}

func sourceFor(kind string) SpecSource {
	switch kind {
	case "swagger2":
		return SourceCanonicalSwagger2
	default:
		return SourceCanonicalOpenAPI3
	}
}

func doJavaExtract(opts Options) (*Result, error) {
	sourceDirs := FindJavaSourceDirs(opts.RootDir)
	if len(sourceDirs) == 0 {
		sourceDirs = []string{opts.RootDir}
	}

	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    opts.RootDir,
		SourceDirs: sourceDirs,
		Verbose:    opts.Verbose,
	})
	if err != nil {
		return nil, fmt.Errorf("java extraction: %w", err)
	}

	specMap := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:           opts.Title,
		Version:         opts.Version,
		Description:     opts.Description,
		OpenAPIVersion:  "3.2",
		ServiceTemplate: opts.Template,
		TreeShake:       true,
	})

	return &Result{
		SpecMap:    specMap,
		Operations: len(result.Operations),
		Types:      len(result.Types),
		Source:     SourceExtracted,
	}, nil
}

func doTypeScriptExtract(opts Options) (*Result, error) {
	sourceDirs := FindTypeScriptSourceDirs(opts.RootDir)
	if len(sourceDirs) == 0 {
		sourceDirs = []string{opts.RootDir}
	}

	result, err := tsextract.Extract(tsextract.Config{
		RootDir:    opts.RootDir,
		SourceDirs: sourceDirs,
		Verbose:    opts.Verbose,
	})
	if err != nil {
		return nil, fmt.Errorf("typescript extraction: %w", err)
	}

	specMap := tsextract.GenerateSpec(result, tsextract.SpecConfig{
		Title:           opts.Title,
		Version:         opts.Version,
		Description:     opts.Description,
		OpenAPIVersion:  "3.2",
		ServiceTemplate: opts.Template,
		TreeShake:       true,
	})

	return &Result{
		SpecMap:    specMap,
		Operations: len(result.Operations),
		Types:      len(result.Types),
		Source:     SourceExtracted,
	}, nil
}

func doPythonExtract(opts Options) (*Result, error) {
	sourceDirs := FindPythonSourceDirs(opts.RootDir)
	if len(sourceDirs) == 0 {
		sourceDirs = []string{opts.RootDir}
	}

	result, err := pythonextract.Extract(pythonextract.Config{
		RootDir:    opts.RootDir,
		SourceDirs: sourceDirs,
		Verbose:    opts.Verbose,
	})
	if err != nil {
		return nil, fmt.Errorf("python extraction: %w", err)
	}

	specMap := pythonextract.GenerateSpec(result, pythonextract.SpecConfig{
		Title:           opts.Title,
		Version:         opts.Version,
		Description:     opts.Description,
		OpenAPIVersion:  "3.2",
		ServiceTemplate: opts.Template,
		TreeShake:       true,
	})

	return &Result{
		SpecMap:    specMap,
		Operations: len(result.Operations),
		Types:      len(result.Types),
		Source:     SourceExtracted,
	}, nil
}

func doCSharpExtract(opts Options) (*Result, error) {
	sourceDirs := FindCSharpSourceDirs(opts.RootDir)
	result, err := csharpextract.Extract(csharpextract.Config{
		RootDir:    opts.RootDir,
		SourceDirs: sourceDirs,
		Verbose:    opts.Verbose,
	})
	if err != nil {
		return nil, fmt.Errorf("csharp extraction: %w", err)
	}
	specMap := csharpextract.GenerateSpec(result, csharpextract.SpecConfig{
		Title:           opts.Title,
		Version:         opts.Version,
		Description:     opts.Description,
		OpenAPIVersion:  "3.2",
		ServiceTemplate: opts.Template,
		TreeShake:       true,
	})
	return &Result{
		SpecMap:    specMap,
		Operations: len(result.Operations),
		Types:      len(result.Schemas),
		Source:     SourceExtracted,
	}, nil
}
