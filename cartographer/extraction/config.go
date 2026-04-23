package extraction

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the .cartographer/cartographer.yaml schema.
type Config struct {
	Service ServiceConfig `yaml:"service"`
}

// ServiceConfig holds per-service extraction configuration fields.
type ServiceConfig struct {
	Name           string        `yaml:"name"`
	Description    string        `yaml:"description"`
	Version        string        `yaml:"version"`
	Team           string        `yaml:"team"`
	Slack          string        `yaml:"slack"`
	Language       string        `yaml:"language"`
	Template       string        `yaml:"template"`
	Contact        *ContactInfo  `yaml:"contact,omitempty"`
	License        *LicenseInfo  `yaml:"license,omitempty"`
	TermsOfService string        `yaml:"termsOfService,omitempty"`
	Servers        []Server      `yaml:"servers,omitempty"`
	PathRewrites   []PathRewrite `yaml:"pathRewrites,omitempty"`
	ExcludePaths   []string      `yaml:"excludePaths,omitempty"`
}

// ContactInfo mirrors the OpenAPI contact object.
type ContactInfo struct {
	Name  string `yaml:"name,omitempty"`
	URL   string `yaml:"url,omitempty"`
	Email string `yaml:"email,omitempty"`
}

// LicenseInfo mirrors the OpenAPI license object.
type LicenseInfo struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url,omitempty"`
}

// Server mirrors the OpenAPI server object.
type Server struct {
	URL         string                    `yaml:"url"`
	Description string                    `yaml:"description"`
	Variables   map[string]ServerVariable `yaml:"variables,omitempty"`
}

// ServerVariable mirrors the OpenAPI server variable object.
type ServerVariable struct {
	Description string `yaml:"description"`
	Default     string `yaml:"default"`
}

// PathRewrite defines a prefix replacement for extracted paths.
type PathRewrite struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// ReadConfig reads and parses a .cartographer/cartographer.yaml file.
func ReadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse yaml: %w", err)
	}
	return cfg, nil
}

// DetectLanguage examines project files to determine the service language.
// Signals are checked in a priority order that matches the most common
// service layouts at SailPoint: Go and Java at the root, then nested Go
// modules (e.g. pag/go/go.mod), then TypeScript (only when a NestJS
// dependency is present to avoid false positives on docs sites), then
// Python, then C#. C# is reported even though cartographer cannot extract
// from it yet so we can record an explicit language-unsupported
// status instead of silently producing zero operations.
func DetectLanguage(root string) (lang string, template string) {
	if fileExists(filepath.Join(root, "go.mod")) {
		return "go", "atlas-go"
	}
	if fileExists(filepath.Join(root, "build.gradle")) || fileExists(filepath.Join(root, "build.gradle.kts")) {
		return "java", "atlas-boot"
	}
	pkgJSON := filepath.Join(root, "package.json")
	if fileExists(pkgJSON) && hasNestJSDependency(pkgJSON) {
		return "typescript", "saas-atlasjs"
	}
	// Nested Go modules: some repos keep the Go sources under go/ (e.g.
	// pag/go/go.mod) with non-Go siblings at root. We only accept nested
	// modules when the root has no other strong language signal, which
	// the checks above already established.
	if findNestedGoModule(root) != "" {
		return "go", "atlas-go"
	}
	// Python is the final non-C# fallback: pyproject.toml is the strongest
	// signal, setup.py / Pipfile / requirements.txt cover legacy and
	// minimal projects.
	if fileExists(filepath.Join(root, "pyproject.toml")) ||
		fileExists(filepath.Join(root, "setup.py")) ||
		fileExists(filepath.Join(root, "setup.cfg")) ||
		fileExists(filepath.Join(root, "Pipfile")) ||
		fileExists(filepath.Join(root, "requirements.txt")) {
		return "python", "atlas-python"
	}
	if fileExists(filepath.Join(root, "Directory.Build.props")) || hasCSharpProject(root) {
		return "csharp", "atlas-csharp"
	}
	return "", ""
}

// FindGoModuleRoot returns the absolute directory containing the
// service's primary go.mod. It honours the "nested module under go/"
// layout used by polyglot repos like pag. Empty return means no go.mod
// is reachable from root.
func FindGoModuleRoot(root string) string {
	if fileExists(filepath.Join(root, "go.mod")) {
		return root
	}
	return findNestedGoModule(root)
}

// findNestedGoModule walks a small, well-known set of subdirectories
// looking for a go.mod. The list is intentionally narrow -- arbitrary
// recursion over the whole repo is too expensive in CI and would match
// vendored third-party modules. If we need to expand this later we can
// extend the candidates slice.
func findNestedGoModule(root string) string {
	candidates := []string{"go", "service", "server", "cmd", "src"}
	for _, c := range candidates {
		p := filepath.Join(root, c, "go.mod")
		if fileExists(p) {
			return filepath.Join(root, c)
		}
	}
	return ""
}

// hasCSharpProject reports whether root contains any .csproj or .sln
// file at depth 1 or 2. We deliberately do not walk deeper: the SailPoint
// .NET services keep project files under src/ or at the repo root.
func hasCSharpProject(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	check := func(name string) bool {
		return strings.HasSuffix(strings.ToLower(name), ".csproj") ||
			strings.HasSuffix(strings.ToLower(name), ".sln")
	}
	for _, e := range entries {
		if !e.IsDir() {
			if check(e.Name()) {
				return true
			}
			continue
		}
		if e.Name() != "src" {
			continue
		}
		sub := filepath.Join(root, e.Name())
		subEntries, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				if check(se.Name()) {
					return true
				}
				continue
			}
			projDir := filepath.Join(sub, se.Name())
			projEntries, err := os.ReadDir(projDir)
			if err != nil {
				continue
			}
			for _, pe := range projEntries {
				if !pe.IsDir() && check(pe.Name()) {
					return true
				}
			}
		}
	}
	return false
}

// LanguageSignals enumerates every on-disk language indicator found at
// root. The zero value reports no signals. Order of fields matches the
// priority order DetectLanguage uses.
type LanguageSignals struct {
	Go        bool
	NestedGo  string // absolute path to the nested module dir, or ""
	Java      bool
	NestJS    bool
	Python    bool
	CSharp    bool
	Canonical string // path to any canonical spec discovered, or ""
}

// Primary returns the single highest-priority language for these
// signals, matching DetectLanguage's return value. "" when nothing
// was found.
func (s LanguageSignals) Primary() string {
	switch {
	case s.Go:
		return "go"
	case s.Java:
		return "java"
	case s.NestJS:
		return "typescript"
	case s.NestedGo != "":
		return "go"
	case s.Python:
		return "python"
	case s.CSharp:
		return "csharp"
	default:
		return ""
	}
}

// DetectLanguageSignals mirrors DetectLanguage but returns every signal
// it finds so callers can report mismatches ("repo claimed atlas-boot
// but we found go.mod"). The canonical-spec path is populated whenever
// DiscoverCanonicalSpec would succeed so meridian can decide to short-
// circuit source extraction even for a correctly-labelled service.
func DetectLanguageSignals(root string) LanguageSignals {
	var s LanguageSignals
	if fileExists(filepath.Join(root, "go.mod")) {
		s.Go = true
	} else {
		s.NestedGo = findNestedGoModule(root)
	}
	if fileExists(filepath.Join(root, "build.gradle")) || fileExists(filepath.Join(root, "build.gradle.kts")) || fileExists(filepath.Join(root, "pom.xml")) {
		s.Java = true
	}
	pkgJSON := filepath.Join(root, "package.json")
	if fileExists(pkgJSON) && hasNestJSDependency(pkgJSON) {
		s.NestJS = true
	}
	if fileExists(filepath.Join(root, "pyproject.toml")) ||
		fileExists(filepath.Join(root, "setup.py")) ||
		fileExists(filepath.Join(root, "setup.cfg")) ||
		fileExists(filepath.Join(root, "Pipfile")) ||
		fileExists(filepath.Join(root, "requirements.txt")) {
		s.Python = true
	}
	if fileExists(filepath.Join(root, "Directory.Build.props")) || hasCSharpProject(root) {
		s.CSharp = true
	}
	if candidate := firstCanonicalSpecPath(root); candidate != "" {
		s.Canonical = candidate
	}
	return s
}

// ApplyConfig applies service-local shaping and metadata to an extracted spec.
func ApplyConfig(specMap map[string]interface{}, cfg Config, template string) bool {
	changed := false
	if applyExcludePaths(specMap, cfg.Service.ExcludePaths) {
		changed = true
	}
	if applyPathRewrites(specMap, cfg.Service.PathRewrites) {
		changed = true
	}
	if injectMetadata(specMap, cfg, template) {
		changed = true
	}
	if applyServers(specMap, cfg.Service.Servers) {
		changed = true
	}
	return changed
}

// WriteFile writes a spec as YAML or JSON based on file extension.
func WriteFile(filePath string, spec map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(filePath), ".json") {
		data, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filePath, append(data, '\n'), 0o644)
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0o644)
}

// GenerateInitYAML returns the default scaffold for .cartographer/cartographer.yaml.
func GenerateInitYAML(name, lang, template, langComment string) string {
	return fmt.Sprintf(`# Cartographer configuration for %s
# See: https://github.com/sailpoint-oss/cartographer

service:
  # Required: display name for the API spec
  name: "%s"

  # Optional: description appears in the OpenAPI info section
  # description: ""

  # Optional: API version (defaults to 1.0.0)
  # version: "1.0.0"

  # Language and template%s
  language: "%s"
  template: "%s"

  # Optional: team ownership metadata (injected as x-service-team, x-service-slack)
  # team: ""
  # slack: ""

  # Optional: OpenAPI contact info
  # contact:
  #   name: "Team Name"
  #   url: "https://example.com"
  #   email: "team@example.com"

  # Optional: license info
  # license:
  #   name: "MIT"
  #   url: "https://opensource.org/licenses/MIT"

  # Optional: terms of service URL
  # termsOfService: "https://example.com/terms"

  # Optional: server definitions (overrides default localhost)
  # servers:
  #   - url: "https://{tenant}.api.example.com"
  #     description: "Production API"
  #     variables:
  #       tenant:
  #         description: "Your tenant ID"
  #         default: "example"

  # Optional: rewrite extracted paths (e.g. to match API gateway routes)
  # pathRewrites:
  #   - from: /internal/prefix
  #     to: /v3/public-path

  # Optional: exclude paths from the generated spec
  # excludePaths:
  #   - /debug/**
  #   - /internal/**
`, name, name, langComment, lang, template)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasNestJSDependency(pkgJSONPath string) bool {
	data, err := os.ReadFile(pkgJSONPath)
	if err != nil {
		return false
	}
	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	for _, depsKey := range []string{"dependencies", "devDependencies", "peerDependencies"} {
		if deps, ok := pkg[depsKey].(map[string]interface{}); ok {
			if _, ok := deps["@nestjs/core"]; ok {
				return true
			}
		}
	}
	return false
}

func injectMetadata(specMap map[string]interface{}, cfg Config, template string) bool {
	info := getObject(specMap["info"])
	changed := false

	if cfg.Service.Name != "" && info["x-service-name"] != cfg.Service.Name {
		info["x-service-name"] = cfg.Service.Name
		changed = true
	}
	if cfg.Service.Team != "" && info["x-service-team"] != cfg.Service.Team {
		info["x-service-team"] = cfg.Service.Team
		changed = true
	}
	if cfg.Service.Slack != "" && info["x-service-slack"] != cfg.Service.Slack {
		info["x-service-slack"] = cfg.Service.Slack
		changed = true
	}
	if template != "" && info["x-service-template"] != template {
		info["x-service-template"] = template
		changed = true
	}

	if contact := contactToMap(cfg.Service.Contact); contact != nil {
		info["contact"] = contact
		changed = true
	}
	if license := licenseToMap(cfg.Service.License); license != nil {
		info["license"] = license
		changed = true
	}
	if cfg.Service.TermsOfService != "" && info["termsOfService"] != cfg.Service.TermsOfService {
		info["termsOfService"] = cfg.Service.TermsOfService
		changed = true
	}

	if changed {
		specMap["info"] = info
	}
	return changed
}

func applyExcludePaths(specMap map[string]interface{}, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	paths := getObject(specMap["paths"])
	if len(paths) == 0 {
		return false
	}

	changed := false
	for path := range paths {
		for _, pattern := range patterns {
			if matchGlob(pattern, path) {
				delete(paths, path)
				changed = true
				break
			}
		}
	}
	if changed {
		specMap["paths"] = paths
	}
	return changed
}

func applyPathRewrites(specMap map[string]interface{}, rewrites []PathRewrite) bool {
	if len(rewrites) == 0 {
		return false
	}

	sorted := make([]PathRewrite, len(rewrites))
	copy(sorted, rewrites)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].From) > len(sorted[j].From)
	})

	paths := getObject(specMap["paths"])
	if len(paths) == 0 {
		return false
	}

	newPaths := make(map[string]interface{}, len(paths))
	changed := false
	for path, value := range paths {
		rewritten := false
		for _, rewrite := range sorted {
			if path == rewrite.From || strings.HasPrefix(path, rewrite.From+"/") {
				newPath := rewrite.To + path[len(rewrite.From):]
				newPaths[newPath] = value
				if newPath != path {
					changed = true
				}
				rewritten = true
				break
			}
		}
		if !rewritten {
			newPaths[path] = value
		}
	}

	if changed {
		specMap["paths"] = newPaths
	}
	return changed
}

func applyServers(specMap map[string]interface{}, servers []Server) bool {
	if len(servers) == 0 {
		return false
	}

	serverList := make([]interface{}, 0, len(servers))
	for _, server := range servers {
		item := map[string]interface{}{
			"url":         server.URL,
			"description": server.Description,
		}
		if len(server.Variables) > 0 {
			vars := make(map[string]interface{}, len(server.Variables))
			for key, variable := range server.Variables {
				value := map[string]interface{}{
					"default": variable.Default,
				}
				if variable.Description != "" {
					value["description"] = variable.Description
				}
				vars[key] = value
			}
			item["variables"] = vars
		}
		serverList = append(serverList, item)
	}

	specMap["servers"] = serverList
	return true
}

func contactToMap(contact *ContactInfo) map[string]interface{} {
	if contact == nil {
		return nil
	}
	value := make(map[string]interface{})
	if contact.Name != "" {
		value["name"] = contact.Name
	}
	if contact.URL != "" {
		value["url"] = contact.URL
	}
	if contact.Email != "" {
		value["email"] = contact.Email
	}
	if len(value) == 0 {
		return nil
	}
	return value
}

func licenseToMap(license *LicenseInfo) map[string]interface{} {
	if license == nil || license.Name == "" {
		return nil
	}
	value := map[string]interface{}{"name": license.Name}
	if license.URL != "" {
		value["url"] = license.URL
	}
	return value
}

func getObject(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return object
}

func matchGlob(pattern, path string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	if strings.Contains(pattern, "*") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}
	return path == pattern
}
