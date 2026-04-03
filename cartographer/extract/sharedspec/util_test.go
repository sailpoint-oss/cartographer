package sharedspec

import (
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

type testAdapter struct{}

func (testAdapter) ParamTypeToSchema(string) map[string]any { return map[string]any{"type": "string"} }
func (testAdapter) IsSimpleType(string) bool                { return true }
func (testAdapter) BuildSecuritySchemes(*specmodel.Result) map[string]any {
	return nil
}
func (testAdapter) FindTypeBySimpleName(map[string]*index.TypeDecl, string) *index.TypeDecl {
	return nil
}
func (testAdapter) IsFileType(string) bool                   { return false }
func (testAdapter) FormParamSchema(string) map[string]any    { return map[string]any{"type": "string"} }

func TestEnsureSchemaRefsExist_AddsMissingSchemaStubs(t *testing.T) {
	spec := map[string]any{
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"responses": map[string]any{
						"200": map[string]any{
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"$ref": "#/components/schemas/MissingType",
									},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"ExistingType": map[string]any{"type": "object"},
			},
		},
	}
	added := EnsureSchemaRefsExist(spec)
	if added != 1 {
		t.Fatalf("expected 1 added stub, got %d", added)
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["MissingType"]; !ok {
		t.Fatal("expected MissingType stub to be created")
	}
}

func TestNormalizeOpenAPIPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "already normalized", in: "/v3/workgroups", want: "/v3/workgroups"},
		{name: "missing slash", in: "v3/workgroups", want: "/v3/workgroups"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOpenAPIPath(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeOpenAPIPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAutoSummary(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"GET", "/v3/entitlements", "List Entitlements"},
		{"GET", "/v3/entitlements/{id}", "Get Entitlement"},
		{"POST", "/v3/entitlements", "Create Entitlement"},
		{"PUT", "/v3/entitlements/{id}", "Update Entitlement"},
		{"PATCH", "/v3/entitlements/{id}", "Patch Entitlement"},
		{"DELETE", "/v3/entitlements/{id}", "Delete Entitlement"},
		{"GET", "/api/v1/roles/{id}/access-profiles", "List Accessprofiles"},
		{"GET", "/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := AutoSummary(tt.method, tt.path)
			if got != tt.want {
				t.Errorf("AutoSummary(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
			}
		})
	}
}

func TestAutoDescription(t *testing.T) {
	tests := []struct {
		method, path string
		wantEmpty    bool
	}{
		{"GET", "/v3/entitlements", false},
		{"GET", "/v3/entitlements/{id}", false},
		{"POST", "/v3/entitlements", false},
		{"DELETE", "/v3/entitlements/{id}", false},
		{"GET", "/", true},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := AutoDescription(tt.method, tt.path, "")
			if tt.wantEmpty && got != "" {
				t.Errorf("AutoDescription(%q, %q) = %q, want empty", tt.method, tt.path, got)
			}
			if !tt.wantEmpty && got == "" {
				t.Errorf("AutoDescription(%q, %q) = empty, want non-empty", tt.method, tt.path)
			}
		})
	}
}

func TestAutoResponseDesc(t *testing.T) {
	tests := []struct {
		method, path, respType, want string
	}{
		{"GET", "/v3/entitlements/{id}", "Entitlement", "Entitlement"},
		{"GET", "/v3/entitlements", "[]Entitlement", "List of Entitlements"},
		{"POST", "/v3/entitlements", "Entitlement", "Entitlement created"},
		{"DELETE", "/v3/entitlements/{id}", "", "Entitlement deleted"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := AutoResponseDesc(tt.method, tt.path, tt.respType)
			if got != tt.want {
				t.Errorf("AutoResponseDesc(%q, %q, %q) = %q, want %q", tt.method, tt.path, tt.respType, got, tt.want)
			}
		})
	}
}

func TestSingularize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Entitlements", "Entitlement"},
		{"Categories", "Category"},
		{"Processes", "Process"},
		{"Boxes", "Box"},
		{"Class", "Class"},
		{"Role", "Role"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := singularize(tt.in)
			if got != tt.want {
				t.Errorf("singularize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildPaths_NormalizesPathKeys(t *testing.T) {
	result := &specmodel.Result{
		Operations: []*specmodel.Operation{
			{
				Path:        "workgroups",
				Method:      "GET",
				OperationID: "listWorkgroups",
				ResponseStatus: 200,
			},
		},
		Schemas: map[string]any{},
		Types:   map[string]*index.TypeDecl{},
	}

	paths := buildPaths(result, testAdapter{})
	if _, ok := paths["/workgroups"]; !ok {
		t.Fatalf("expected normalized /workgroups path key, got %#v", paths)
	}
	if _, ok := paths["workgroups"]; ok {
		t.Fatalf("unexpected non-normalized path key present: %#v", paths)
	}
}
