package extraction

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExtractionConfig_UnmarshalYAML_ErrorSchema(t *testing.T) {
	var c ExtractionConfig
	if err := yaml.Unmarshal([]byte("errorSchema: legacy-error-response\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.ErrorSchema != "legacy-error-response" {
		t.Fatalf("ErrorSchema = %q", c.ErrorSchema)
	}
	if got := c.Options().ErrorSchema; got != "legacy-error-response" {
		t.Fatalf("options.ErrorSchema = %q", got)
	}
}

func TestExtractionConfig_UnmarshalYAML_LegacyAuthFieldsIgnored(t *testing.T) {
	// authScopeTranslation and rightsMappingPath are no longer recognised.
	// The auth pipeline always emits standard security.oauth2 requirements
	// from extracted strings; consumers translate downstream if needed.
	var c ExtractionConfig
	raw := "authScopeTranslation: true\nrightsMappingPath: /tmp/x.json\n"
	if err := yaml.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	// No assertions on individual fields: the struct intentionally has no
	// auth knobs; the unknown yaml keys decode into nothing.
}
