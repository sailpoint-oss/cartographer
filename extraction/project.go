package extraction

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/extractionopts"
)

// ProjectOptions configures a service-local extraction run with optional
// .cartographer config loading and output-path planning.
type ProjectOptions struct {
	ConfigDir   string
	RootDir     string
	OutputPath  string
	Lang        string
	Template    string
	Title       string
	Version     string
	Description string
	Verbose     bool
	// Kind (optional) lets callers mark a service as non-REST (e.g.
	// "worker", "library"). When set to a non-rest kind the source
	// extraction is skipped and a minimal stub spec is produced; the
	// canonical-spec passthrough still runs when a spec file is present
	// so library modules that do publish a spec still show up.
	Kind string
	// PreferCanonicalSpec tells ExtractProject to auto-discover an
	// in-repo canonical spec (openapi.yaml, docs/swagger.yaml,
	// openapi.yaml, ...) and use it in place of source extraction when
	// one is found. When no canonical spec is found source extraction
	// runs as usual. Use this from batch pipelines that want the
	// passthrough behaviour without the caller having to probe for
	// files themselves.
	PreferCanonicalSpec bool
	// Extraction optionally overrides cartographer.yaml extraction settings.
	Extraction extractionopts.Options
}

func mergeExtractionOptions(cfg extractionopts.Options, override extractionopts.Options) extractionopts.Options {
	if override.ErrorSchema != "" {
		cfg.ErrorSchema = override.ErrorSchema
	}
	if len(override.SignaturePaginationTypes) > 0 {
		cfg.SignaturePaginationTypes = override.SignaturePaginationTypes
	}
	if override.MergeCoLocatedOpenAPI {
		cfg.MergeCoLocatedOpenAPI = true
	}
	return cfg
}

// ProjectResult captures the effective extraction settings after config
// resolution alongside the extracted spec.
type ProjectResult struct {
	*Result
	Effective     Options
	Config        Config
	HasConfig     bool
	ConfigApplied bool
	ConfigPath    string
	OutputPath    string
	// Signals captures every on-disk language indicator DetectLanguage
	// observed. Populated even when Lang was set explicitly so callers
	// can warn on label/content mismatches.
	Signals LanguageSignals
	// LanguageMismatch reports whether the effective language disagrees
	// with the strongest on-disk signal. Purely informational; the
	// extraction still runs with the effective language.
	LanguageMismatch bool
	// DetectedLanguage is the language DetectLanguage would have
	// chosen from on-disk signals alone. Empty when no signal exists.
	DetectedLanguage string
	// CanonicalSpecPath is the absolute path of the in-repo spec that
	// was used (passthrough). Empty when source extraction ran.
	CanonicalSpecPath string
}

// ExtractProject resolves config and overrides, performs extraction, applies
// service-local shaping, and returns the planned output path. Call Write to
// persist the generated spec.
func ExtractProject(opts ProjectOptions) (*ProjectResult, error) {
	return extractProjectWithRunner(opts, Extract)
}

// Write writes the generated spec to the resolved output path.
func (r *ProjectResult) Write() error {
	if r == nil || r.Result == nil {
		return fmt.Errorf("nil extraction result")
	}
	if r.OutputPath == "" {
		return fmt.Errorf("no output path resolved for extraction result")
	}
	return WriteFile(r.OutputPath, r.SpecMap)
}

func extractProjectWithRunner(opts ProjectOptions, runner func(Options) (*Result, error)) (*ProjectResult, error) {
	rootDir, err := resolveProjectRoot(opts.RootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	configDir := opts.ConfigDir
	if configDir == "" {
		configDir = ".cartographer"
	}
	configDir = resolveAgainstRoot(rootDir, configDir)
	configPath := filepath.Join(configDir, "cartographer.yaml")

	var (
		cfg    Config
		hasCfg bool
	)
	if info, err := os.Stat(configPath); err == nil && !info.IsDir() {
		cfg, err = ReadConfig(configPath)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", configPath, err)
		}
		hasCfg = true
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat config %s: %w", configPath, err)
	}

	// Collect every on-disk signal up front so we can both
	//   (1) fall back to auto-detection when no language was set, and
	//   (2) report label/content mismatches regardless of the path taken.
	signals := DetectLanguageSignals(rootDir)
	detected := signals.Primary()

	lang := firstNonEmpty(opts.Lang, cfg.Service.Language)
	if lang == "" {
		if detected == "" {
			return nil, fmt.Errorf("could not determine language from project files in %s; run 'cartographer init' to scaffold a config or pass --lang explicitly", rootDir)
		}
		lang = detected
	}

	// Normalise the provided language so downstream comparisons are
	// stable ("ts" and "typescript" both mean typescript).
	normalised := normaliseLang(lang)
	langMismatch := detected != "" && normalised != normaliseLang(detected)

	template := firstNonEmpty(opts.Template, cfg.Service.Template, InferTemplate(normalised))
	title := firstNonEmpty(opts.Title, cfg.Service.Name, "API")
	version := firstNonEmpty(opts.Version, cfg.Service.Version, "1.0.0")
	description := firstNonEmpty(opts.Description, cfg.Service.Description)

	effective := Options{
		Lang:             normalised,
		Template:         template,
		RootDir:          rootDir,
		Title:            title,
		Version:          version,
		Description:      description,
		Verbose:          opts.Verbose,
		UseCanonicalSpec: opts.PreferCanonicalSpec,
		Extraction:       mergeExtractionOptions(cfg.Service.Extraction.Options(), opts.Extraction),
	}

	// Non-REST services (workers, libraries, gateways) never have
	// meaningful routes in-source. Skip the extractor and either
	// use the in-repo canonical spec when one exists or return a
	// minimal stub so we can still produce an artifact.
	if isNonRESTKind(opts.Kind) {
		effective.UseCanonicalSpec = true
		if signals.Canonical == "" {
			stub := canonicalKindStub(effective, opts.Kind)
			return buildProjectResult(opts, stub, effective, cfg, hasCfg, configDir, configPath, signals, detected, langMismatch), nil
		}
	}

	result, err := runner(effective)
	if err != nil {
		return nil, err
	}

	configApplied := false
	if hasCfg {
		configApplied = ApplyConfig(result.SpecMap, cfg, template)
	}

	outputPath := resolveOutputPath(opts.OutputPath, rootDir, configDir)

	return &ProjectResult{
		Result:            result,
		Effective:         effective,
		Config:            cfg,
		HasConfig:         hasCfg,
		ConfigApplied:     configApplied,
		ConfigPath:        configPath,
		OutputPath:        outputPath,
		Signals:           signals,
		LanguageMismatch:  langMismatch,
		DetectedLanguage:  detected,
		CanonicalSpecPath: result.CanonicalPath,
	}, nil
}

// buildProjectResult assembles a ProjectResult for the non-REST stub path
// so the regular extraction flow and the stub flow share output-path and
// config-apply logic.
func buildProjectResult(
	opts ProjectOptions,
	result *Result,
	effective Options,
	cfg Config,
	hasCfg bool,
	configDir string,
	configPath string,
	signals LanguageSignals,
	detected string,
	langMismatch bool,
) *ProjectResult {
	configApplied := false
	if hasCfg {
		configApplied = ApplyConfig(result.SpecMap, cfg, effective.Template)
	}
	outputPath := resolveOutputPath(opts.OutputPath, effective.RootDir, configDir)
	return &ProjectResult{
		Result:            result,
		Effective:         effective,
		Config:            cfg,
		HasConfig:         hasCfg,
		ConfigApplied:     configApplied,
		ConfigPath:        configPath,
		OutputPath:        outputPath,
		Signals:           signals,
		LanguageMismatch:  langMismatch,
		DetectedLanguage:  detected,
		CanonicalSpecPath: result.CanonicalPath,
	}
}

func resolveOutputPath(override, rootDir, configDir string) string {
	outputPath := override
	if outputPath == "" {
		outputPath = filepath.Join(configDir, "openapi.yaml")
	} else {
		outputPath = resolveAgainstRoot(rootDir, outputPath)
	}
	return ensureSpecExtension(outputPath)
}

// canonicalKindStub returns a minimal OpenAPI 3 document for a service
// marked as non-REST. The x-service-kind extension makes the intent
// explicit so downstream reports can skip the service cleanly.
func canonicalKindStub(opts Options, kind string) *Result {
	info := map[string]interface{}{
		"title":          opts.Title,
		"version":        opts.Version,
		"x-service-kind": kind,
		"x-spec-source":  "non-rest-stub",
	}
	if opts.Description != "" {
		info["description"] = opts.Description
	}
	if opts.Template != "" {
		info["x-service-template"] = opts.Template
	}
	spec := map[string]interface{}{
		"openapi": "3.0.3",
		"info":    info,
		"paths":   map[string]interface{}{},
	}
	return &Result{
		SpecMap: spec,
		Source:  SpecSource("non-rest-stub"),
	}
}

func isNonRESTKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "worker", "library", "non-rest", "scheduler", "connector", "cli":
		return true
	default:
		return false
	}
}

func normaliseLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "ts", "typescript":
		return "typescript"
	case "py", "python":
		return "python"
	case "cs", "csharp", "dotnet":
		return "csharp"
	default:
		return strings.ToLower(strings.TrimSpace(lang))
	}
}

func resolveProjectRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}
	return filepath.Abs(root)
}

func resolveAgainstRoot(root, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func ensureSpecExtension(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") {
		return path
	}
	return path + ".yaml"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
