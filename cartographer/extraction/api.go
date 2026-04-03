package extraction

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sailpoint-oss/cartographer/extract/goextract"
	"github.com/sailpoint-oss/cartographer/extract/javaextract"
	"github.com/sailpoint-oss/cartographer/extract/specgen"
	"github.com/sailpoint-oss/cartographer/extract/tsextract"
)

// Options holds all parameters needed for a single extraction run.
type Options struct {
	Lang        string
	Template    string
	RootDir     string
	Title       string
	Version     string
	Description string
	Verbose     bool
}

// Result holds the output of a single extraction run.
type Result struct {
	SpecMap    map[string]interface{}
	Operations int
	Types      int
}

// Extract performs source-code extraction for a single service.
func Extract(opts Options) (*Result, error) {
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}
	if opts.Template == "" {
		opts.Template = InferTemplate(opts.Lang)
	}

	switch opts.Lang {
	case "go":
		return doGoExtract(opts)
	case "java":
		return doJavaExtract(opts)
	case "typescript", "ts":
		return doTypeScriptExtract(opts)
	default:
		return nil, fmt.Errorf("unsupported language: %s (supported: go, java, typescript)", opts.Lang)
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
	default:
		return ""
	}
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

func doGoExtract(opts Options) (*Result, error) {
	pkg := opts.RootDir
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
	}, nil
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
	}, nil
}
