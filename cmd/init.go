package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sailpoint-oss/cartographer/extraction"
	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	initRoot string
	initDir  string
	initName string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a .cartographer/cartographer.yaml config file",
	Long: `Initialize a new Cartographer configuration for a service repository.

Auto-detects the project language and template from project files (go.mod,
build.gradle, package.json) and generates a .cartographer/cartographer.yaml
with sensible defaults and documented optional fields.

Run this in the root of your service repository:

  cartographer init

Then edit .cartographer/cartographer.yaml to customize your service metadata,
path rewrites, server configuration, and other OpenAPI info fields.
`,
	Example: `  # Initialize in current directory
  cartographer init

  # Custom project root and name
  cartographer init --root /path/to/service --name "My Service"`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initRoot, "root", ".", "Project root directory")
	initCmd.Flags().StringVar(&initDir, "dir", ".cartographer", "Output directory for config file")
	initCmd.Flags().StringVar(&initName, "name", "", "Service name (auto-detected from directory name if omitted)")
}

func runInit(cmd *cobra.Command, args []string) error {
	absRoot, err := filepath.Abs(initRoot)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}

	outDir := initDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(absRoot, outDir)
	}
	configPath := filepath.Join(outDir, "cartographer.yaml")

	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("%s already exists; edit it directly or remove it first", configPath)
	}

	lang, template := extraction.DetectLanguage(absRoot)
	langComment := ""
	if lang != "" {
		langComment = fmt.Sprintf(" (auto-detected: %s)", lang)
	} else {
		langComment = " (could not auto-detect)"
		lang = "go"
		template = "atlas-go"
	}

	name := initName
	if name == "" {
		name = filepath.Base(absRoot)
		name = strings.ReplaceAll(name, "-", " ")
		name = cases.Title(language.English).String(name)
	}

	content := extraction.GenerateInitYAML(name, lang, template, langComment)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	logger.Info("Created config", "path", configPath, "service", name, "language", lang)
	logger.Info("Next steps:")
	logger.Info("  1. Edit the config to customize your service metadata")
	logger.Info("  2. Run: cartographer extract")
	logger.Info("  3. Review the generated spec at .cartographer/openapi.yaml")
	return nil
}
