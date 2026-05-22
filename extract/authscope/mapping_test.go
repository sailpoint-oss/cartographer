package authscope

import (
	"path/filepath"
	"testing"
)

func TestLoadFictionalMapping(t *testing.T) {
	path := filepath.Join("testdata", "mapping.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	scopes, unmapped := m.MinimalScopes([]string{"api:orders:read", "api:orders:write"})
	if len(unmapped) != 0 {
		t.Fatalf("unmapped = %v", unmapped)
	}
	if len(scopes) != 1 || scopes[0] != "api:orders:manage" {
		t.Fatalf("minimal scopes = %v, want [api:orders:manage]", scopes)
	}
}

func TestMinimalScopesUnmapped(t *testing.T) {
	m, err := Load(filepath.Join("testdata", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, unmapped := m.MinimalScopes([]string{"unknown:right:read"})
	if len(unmapped) != 1 || unmapped[0] != "unknown:right:read" {
		t.Fatalf("unmapped = %v", unmapped)
	}
}

func TestClassify(t *testing.T) {
	m, err := Load(filepath.Join("testdata", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	rights, scopes := m.Classify([]string{"api:orders:read", "example:resource:manage"})
	if len(rights) != 0 {
		t.Fatalf("rights = %v", rights)
	}
	if len(scopes) != 2 {
		t.Fatalf("scopes = %v", scopes)
	}
	rights2, scopes2 := m.Classify([]string{"api:orders:write"})
	if len(scopes2) != 0 {
		t.Fatalf("scopes2 = %v", scopes2)
	}
	if len(rights2) != 1 || rights2[0] != "api:orders:write" {
		t.Fatalf("rights2 = %v", rights2)
	}
}

func TestApplyToOperation(t *testing.T) {
	m, err := Load(filepath.Join("testdata", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	op := map[string]any{"operationId": "listOrders"}
	ApplyToOperation(op, []string{"api:orders:read"}, ApplyOptions{Enabled: true, Mapping: m})
	if op[ExtRequiredRights] == nil {
		t.Fatal("missing required rights extension")
	}
	sec, ok := op["security"].([]any)
	if !ok || len(sec) == 0 {
		t.Fatal("missing security")
	}
}
