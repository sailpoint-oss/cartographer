package cmd

import (
	"fmt"

	"github.com/sailpoint-oss/cartographer/extraction"
	"github.com/spf13/cobra"
)

var (
	extractDir     string
	extractRoot    string
	extractOut     string
	extractLang    string
	extractTitle   string
	extractVersion string
	extractDesc    string
	extractTmpl    string
)

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract an OpenAPI spec from service source code",
	Long: `Extract an OpenAPI specification from a service's source code using
static analysis (Go: go/ast, Java/TypeScript: tree-sitter).

By default, reads .cartographer/cartographer.yaml for service configuration
including language, template, metadata, path rewrites, and servers. Use
--dir to specify a different config directory.

Explicit flags (--lang, --title, etc.) override config values. If no config
file exists, --lang is required.
`,
	Example: `  # Per-service extraction (reads .cartographer/cartographer.yaml)
  cartographer extract

  # Explicit config directory
  cartographer extract --dir .cartographer

  # Explicit language and output (no config file needed)
  cartographer extract --lang go --root ./my-service --output api.yaml

  # Override config values
  cartographer extract --title "My Service API" --version 2.0.0`,
	RunE: runExtract,
}

func init() {
	extractCmd.Flags().StringVar(&extractDir, "dir", ".cartographer", "Path to cartographer config directory")
	extractCmd.Flags().StringVarP(&extractRoot, "root", "r", ".", "Service project root")
	extractCmd.Flags().StringVarP(&extractOut, "output", "o", "", "Output spec path (default: <dir>/openapi.yaml)")
	extractCmd.Flags().StringVar(&extractLang, "lang", "", "Language override (go, java, typescript)")
	extractCmd.Flags().StringVar(&extractTitle, "title", "", "API title override")
	extractCmd.Flags().StringVar(&extractVersion, "version", "", "API version override")
	extractCmd.Flags().StringVar(&extractDesc, "description", "", "API description override")
	extractCmd.Flags().StringVar(&extractTmpl, "template", "", "Service template override (atlas-go, atlas-boot, saas-atlasjs)")
}

func runExtract(cmd *cobra.Command, args []string) error {
	result, err := extraction.ExtractProject(extraction.ProjectOptions{
		ConfigDir:   extractDir,
		RootDir:     extractRoot,
		OutputPath:  extractOut,
		Lang:        extractLang,
		Template:    extractTmpl,
		Title:       extractTitle,
		Version:     extractVersion,
		Description: extractDesc,
		Verbose:     verbose,
	})
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	logger.Info("Extracting service", "name", result.Effective.Title, "lang", result.Effective.Lang, "template", result.Effective.Template)
	logger.Info("Extraction complete", "operations", result.Operations, "types", result.Types, "config", result.HasConfig, "configApplied", result.ConfigApplied)

	if err := result.Write(); err != nil {
		return fmt.Errorf("write spec: %w", err)
	}

	logger.Info("OpenAPI spec written", "path", result.OutputPath)
	return nil
}
