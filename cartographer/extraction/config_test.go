package extraction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfig_FullSchema(t *testing.T) {
	content := `
service:
  name: "My Service"
  description: "A test service"
  version: "2.0.0"
  team: "Platform"
  slack: "#platform"
  language: "go"
  template: "atlas-go"
  contact:
    name: "Dev Team"
    url: "https://example.com"
    email: "dev@example.com"
  license:
    name: "MIT"
    url: "https://mit.com"
  termsOfService: "https://example.com/tos"
  servers:
    - url: "https://api.example.com"
      description: "Prod"
      variables:
        tenant:
          description: "Tenant"
          default: "demo"
  pathRewrites:
    - from: /old
      to: /new
  excludePaths:
    - /debug/**
`
	dir := t.TempDir()
	path := filepath.Join(dir, "cartographer.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	svc := cfg.Service
	if svc.Name != "My Service" {
		t.Errorf("name = %v", svc.Name)
	}
	if svc.Description != "A test service" {
		t.Errorf("description = %v", svc.Description)
	}
	if svc.Version != "2.0.0" {
		t.Errorf("version = %v", svc.Version)
	}
	if svc.Team != "Platform" {
		t.Errorf("team = %v", svc.Team)
	}
	if svc.Slack != "#platform" {
		t.Errorf("slack = %v", svc.Slack)
	}
	if svc.Language != "go" {
		t.Errorf("language = %v", svc.Language)
	}
	if svc.Template != "atlas-go" {
		t.Errorf("template = %v", svc.Template)
	}

	if svc.Contact == nil {
		t.Fatal("expected contact")
	}
	if svc.Contact.Name != "Dev Team" {
		t.Errorf("contact.name = %v", svc.Contact.Name)
	}
	if svc.Contact.URL != "https://example.com" {
		t.Errorf("contact.url = %v", svc.Contact.URL)
	}
	if svc.Contact.Email != "dev@example.com" {
		t.Errorf("contact.email = %v", svc.Contact.Email)
	}

	if svc.License == nil {
		t.Fatal("expected license")
	}
	if svc.License.Name != "MIT" {
		t.Errorf("license.name = %v", svc.License.Name)
	}
	if svc.License.URL != "https://mit.com" {
		t.Errorf("license.url = %v", svc.License.URL)
	}

	if svc.TermsOfService != "https://example.com/tos" {
		t.Errorf("termsOfService = %v", svc.TermsOfService)
	}

	if len(svc.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(svc.Servers))
	}
	if svc.Servers[0].URL != "https://api.example.com" {
		t.Errorf("server.url = %v", svc.Servers[0].URL)
	}
	if svc.Servers[0].Variables["tenant"].Default != "demo" {
		t.Errorf("server.variables.tenant.default = %v", svc.Servers[0].Variables["tenant"].Default)
	}

	if len(svc.PathRewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %d", len(svc.PathRewrites))
	}
	if svc.PathRewrites[0].From != "/old" || svc.PathRewrites[0].To != "/new" {
		t.Errorf("rewrite = %v", svc.PathRewrites[0])
	}

	if len(svc.ExcludePaths) != 1 || svc.ExcludePaths[0] != "/debug/**" {
		t.Errorf("excludePaths = %v", svc.ExcludePaths)
	}
}

func TestReadConfig_MinimalSchema(t *testing.T) {
	content := `
service:
  name: "Simple"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "cartographer.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Service.Name != "Simple" {
		t.Errorf("name = %v", cfg.Service.Name)
	}
	if cfg.Service.Contact != nil {
		t.Error("expected nil contact")
	}
	if cfg.Service.License != nil {
		t.Error("expected nil license")
	}
	if cfg.Service.Version != "" {
		t.Errorf("expected empty version, got %v", cfg.Service.Version)
	}
	if len(cfg.Service.Servers) != 0 {
		t.Error("expected no servers")
	}
	if len(cfg.Service.PathRewrites) != 0 {
		t.Error("expected no rewrites")
	}
}

func TestDetectLanguage_Go(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}

	lang, tmpl := DetectLanguage(dir)
	if lang != "go" || tmpl != "atlas-go" {
		t.Errorf("lang=%v, template=%v", lang, tmpl)
	}
}

func TestDetectLanguage_Java(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	lang, tmpl := DetectLanguage(dir)
	if lang != "java" || tmpl != "atlas-boot" {
		t.Errorf("lang=%v, template=%v", lang, tmpl)
	}
}

func TestDetectLanguage_TypeScript(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"dependencies": {"@nestjs/core": "^10.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}

	lang, tmpl := DetectLanguage(dir)
	if lang != "typescript" || tmpl != "saas-atlasjs" {
		t.Errorf("lang=%v, template=%v", lang, tmpl)
	}
}

func TestDetectLanguage_Unknown(t *testing.T) {
	dir := t.TempDir()
	lang, tmpl := DetectLanguage(dir)
	if lang != "" || tmpl != "" {
		t.Errorf("expected empty, got lang=%v, template=%v", lang, tmpl)
	}
}

func TestGenerateInitYAML(t *testing.T) {
	output := GenerateInitYAML("Test Service", "go", "atlas-go", " (auto-detected: go)")

	for _, needle := range []string{
		`name: "Test Service"`,
		`language: "go"`,
		`template: "atlas-go"`,
		"pathRewrites",
		"excludePaths",
		"contact",
		"servers",
	} {
		if !contains(output, needle) {
			t.Errorf("expected %q in output", needle)
		}
	}
}

func TestApplyConfig_InjectsMetadataAndShaping(t *testing.T) {
	specMap := map[string]interface{}{
		"info": map[string]interface{}{"title": "Test", "version": "1.0.0"},
		"paths": map[string]interface{}{
			"/old/widgets":   map[string]interface{}{},
			"/debug/metrics": map[string]interface{}{},
		},
	}

	cfg := Config{
		Service: ServiceConfig{
			Name:           "My API",
			Team:           "Team A",
			Slack:          "#team-a",
			TermsOfService: "https://example.com/tos",
			Contact:        &ContactInfo{Name: "Dev Team", Email: "dev@example.com"},
			License:        &LicenseInfo{Name: "MIT", URL: "https://opensource.org/licenses/MIT"},
			PathRewrites:   []PathRewrite{{From: "/old", To: "/v3"}},
			ExcludePaths:   []string{"/debug/**"},
			Servers: []Server{{
				URL:         "https://api.example.com",
				Description: "Prod",
			}},
		},
	}

	if !ApplyConfig(specMap, cfg, "atlas-go") {
		t.Fatal("expected config application to report changes")
	}

	info := specMap["info"].(map[string]interface{})
	if info["x-service-name"] != "My API" {
		t.Errorf("x-service-name = %v", info["x-service-name"])
	}
	if info["x-service-team"] != "Team A" {
		t.Errorf("x-service-team = %v", info["x-service-team"])
	}
	if info["x-service-slack"] != "#team-a" {
		t.Errorf("x-service-slack = %v", info["x-service-slack"])
	}
	if info["x-service-template"] != "atlas-go" {
		t.Errorf("x-service-template = %v", info["x-service-template"])
	}
	if info["termsOfService"] != "https://example.com/tos" {
		t.Errorf("termsOfService = %v", info["termsOfService"])
	}

	paths := specMap["paths"].(map[string]interface{})
	if _, ok := paths["/debug/metrics"]; ok {
		t.Fatal("expected debug path to be removed")
	}
	if _, ok := paths["/v3/widgets"]; !ok {
		t.Fatal("expected rewritten path to exist")
	}

	servers, ok := specMap["servers"].([]interface{})
	if !ok || len(servers) != 1 {
		t.Fatalf("servers = %v", specMap["servers"])
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
