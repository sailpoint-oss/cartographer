package extraction

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	lang := firstNonEmpty(opts.Lang, cfg.Service.Language)
	if lang == "" {
		detected, _ := DetectLanguage(rootDir)
		if detected == "" {
			return nil, fmt.Errorf("could not determine language from project files in %s; run 'cartographer init' to scaffold a config or pass --lang explicitly", rootDir)
		}
		lang = detected
	}

	template := firstNonEmpty(opts.Template, cfg.Service.Template, InferTemplate(lang))
	title := firstNonEmpty(opts.Title, cfg.Service.Name, "API")
	version := firstNonEmpty(opts.Version, cfg.Service.Version, "1.0.0")
	description := firstNonEmpty(opts.Description, cfg.Service.Description)

	effective := Options{
		Lang:        lang,
		Template:    template,
		RootDir:     rootDir,
		Title:       title,
		Version:     version,
		Description: description,
		Verbose:     opts.Verbose,
	}

	result, err := runner(effective)
	if err != nil {
		return nil, err
	}

	configApplied := false
	if hasCfg {
		configApplied = ApplyConfig(result.SpecMap, cfg, template)
	}

	outputPath := opts.OutputPath
	if outputPath == "" {
		outputPath = filepath.Join(configDir, "openapi.yaml")
	} else {
		outputPath = resolveAgainstRoot(rootDir, outputPath)
	}
	outputPath = ensureSpecExtension(outputPath)

	return &ProjectResult{
		Result:        result,
		Effective:     effective,
		Config:        cfg,
		HasConfig:     hasCfg,
		ConfigApplied: configApplied,
		ConfigPath:    configPath,
		OutputPath:    outputPath,
	}, nil
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
