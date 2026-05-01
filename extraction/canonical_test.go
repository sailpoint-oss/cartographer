package extraction

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverCanonicalSpec_NoCandidate(t *testing.T) {
	root := t.TempDir()
	spec, err := DiscoverCanonicalSpec(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec != nil {
		t.Fatalf("expected nil when no canonical spec exists, got %+v", spec)
	}
}

func TestDiscoverCanonicalSpec_FindsSailPointAPI(t *testing.T) {
	root := t.TempDir()
	subDir := filepath.Join(root, "my-svc-api")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `openapi: 3.0.1
info:
  title: My Svc
  version: 1.0.0
paths:
  /widgets:
    get:
      responses:
        "200":
          description: ok
    post:
      responses:
        "201":
          description: created
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
`
	if err := os.WriteFile(filepath.Join(subDir, "sailpoint-api.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec, err := DiscoverCanonicalSpec(root)
	if err != nil {
		t.Fatalf("DiscoverCanonicalSpec: %v", err)
	}
	if spec == nil {
		t.Fatal("expected to find sailpoint-api.yaml under my-svc-api/")
	}
	if spec.Kind != "openapi3" {
		t.Errorf("Kind = %q, want openapi3", spec.Kind)
	}
	if spec.Operations != 2 {
		t.Errorf("Operations = %d, want 2", spec.Operations)
	}
	if spec.Types != 1 {
		t.Errorf("Types = %d, want 1", spec.Types)
	}
	if !strings.HasSuffix(spec.RelPath, filepath.Join("my-svc-api", "sailpoint-api.yaml")) {
		t.Errorf("RelPath = %q, want suffix my-svc-api/sailpoint-api.yaml", spec.RelPath)
	}
}

func TestDiscoverCanonicalSpec_PriorityOrder(t *testing.T) {
	root := t.TempDir()
	// Place a lower-priority docs/swagger.yaml AND a higher-priority
	// sailpoint-api.yaml: the latter should win.
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docs, "swagger.yaml"), []byte("swagger: \"2.0\"\ninfo: {title: old, version: 1}\npaths: {}\n"), 0o644); err != nil {
		t.Fatalf("write swagger: %v", err)
	}
	top := "openapi: 3.0.3\ninfo: {title: top, version: 2}\npaths: {}\n"
	if err := os.WriteFile(filepath.Join(root, "sailpoint-api.yaml"), []byte(top), 0o644); err != nil {
		t.Fatalf("write sailpoint-api: %v", err)
	}

	spec, err := DiscoverCanonicalSpec(root)
	if err != nil {
		t.Fatalf("DiscoverCanonicalSpec: %v", err)
	}
	if spec == nil {
		t.Fatal("expected a spec")
	}
	if !strings.HasSuffix(spec.Path, "sailpoint-api.yaml") {
		t.Fatalf("priority order wrong: got %s", spec.Path)
	}
	if spec.Kind != "openapi3" {
		t.Fatalf("Kind = %q", spec.Kind)
	}
}

func TestDiscoverCanonicalSpec_Swagger2Conversion(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// swaggo-style Swagger 2.0 with basePath, definitions, paths.
	swag := `swagger: "2.0"
basePath: /widgets
info:
  title: Widgets
  version: "1.0"
paths:
  /widgets:
    get:
      summary: list widgets
      responses:
        "200":
          description: ok
          schema:
            type: array
            items:
              $ref: "#/definitions/Widget"
definitions:
  Widget:
    type: object
    properties:
      id:
        type: string
`
	if err := os.WriteFile(filepath.Join(docs, "swagger.yaml"), []byte(swag), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec, err := DiscoverCanonicalSpec(root)
	if err != nil {
		t.Fatalf("DiscoverCanonicalSpec: %v", err)
	}
	if spec == nil {
		t.Fatal("expected spec")
	}
	if spec.Kind != "swagger2" {
		t.Fatalf("Kind = %q, want swagger2", spec.Kind)
	}
	if _, ok := spec.SpecMap["openapi"].(string); !ok {
		t.Fatalf("expected openapi 3 output, got %v", spec.SpecMap["openapi"])
	}
	if spec.Operations != 1 {
		t.Errorf("Operations = %d, want 1", spec.Operations)
	}
	if spec.Types != 1 {
		t.Errorf("Types = %d, want 1 (converted from definitions)", spec.Types)
	}
	// Verify that $ref was converted from #/definitions/ to #/components/schemas/.
	paths, _ := spec.SpecMap["paths"].(map[string]interface{})
	widgets, _ := paths["/widgets"].(map[string]interface{})
	get, _ := widgets["get"].(map[string]interface{})
	responses, _ := get["responses"].(map[string]interface{})
	ok, _ := responses["200"].(map[string]interface{})
	content, _ := ok["content"].(map[string]interface{})
	if len(content) == 0 {
		t.Error("expected response to be migrated to content/media-type shape")
	}
}

func TestDiscoverCanonicalSpec_UnrecognisedFormat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "openapi.yaml"), []byte("# not a real spec\nsome: thing\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := DiscoverCanonicalSpec(root)
	if err == nil {
		t.Fatal("expected error for unrecognised format")
	}
}

func TestExtract_OverrideSpecPath(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: Override
  version: "1.0"
paths:
  /thing:
    get:
      responses:
        "200":
          description: ok
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := Extract(Options{
		Lang:             "go",
		RootDir:          root,
		Title:            "Override",
		OverrideSpecPath: specPath,
		Template:         "atlas-go",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.Source != SourceCanonicalOpenAPI3 {
		t.Fatalf("Source = %q, want canonical-openapi3", res.Source)
	}
	if res.Operations != 1 {
		t.Errorf("Operations = %d, want 1", res.Operations)
	}
	info := res.SpecMap["info"].(map[string]interface{})
	if info["x-spec-source"] != "canonical-openapi3" {
		t.Errorf("x-spec-source not set: %v", info["x-spec-source"])
	}
	if info["x-service-template"] != "atlas-go" {
		t.Errorf("x-service-template not set: %v", info["x-service-template"])
	}
}

// TestDiscoverCanonicalSpec_InlinesExternalRefs covers a multi-file
// OpenAPI 3 spec layout where paths and schemas live in sibling files
// referenced via $ref. The canonical loader's refInliner walks the
// decoded map, inlines path-item refs in place, and hoists schemas
// into components so the persisted output carries the inlined
// operations and schemas -- that is exactly what downstream meridian
// stages require.
func TestDiscoverCanonicalSpec_InlinesExternalRefs(t *testing.T) {
	root := t.TempDir()
	paths := filepath.Join(root, "paths")
	schemas := filepath.Join(root, "schemas")
	for _, d := range []string{paths, schemas} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	widgetPath := `get:
  summary: list widgets
  responses:
    "200":
      description: ok
      content:
        application/json:
          schema:
            $ref: "../schemas/widget.yaml"
post:
  summary: create widget
  responses:
    "201":
      description: created
`
	if err := os.WriteFile(filepath.Join(paths, "widgets.yaml"), []byte(widgetPath), 0o644); err != nil {
		t.Fatalf("write widgets: %v", err)
	}
	widgetSchema := `type: object
properties:
  id:
    type: string
  name:
    type: string
`
	if err := os.WriteFile(filepath.Join(schemas, "widget.yaml"), []byte(widgetSchema), 0o644); err != nil {
		t.Fatalf("write widget schema: %v", err)
	}
	top := `openapi: 3.0.3
info:
  title: Multi
  version: "1.0"
paths:
  /widgets:
    $ref: "./paths/widgets.yaml"
components:
  schemas:
    Widget:
      $ref: "./schemas/widget.yaml"
`
	if err := os.WriteFile(filepath.Join(root, "openapi.yaml"), []byte(top), 0o644); err != nil {
		t.Fatalf("write top: %v", err)
	}
	spec, err := DiscoverCanonicalSpec(root)
	if err != nil {
		t.Fatalf("DiscoverCanonicalSpec: %v", err)
	}
	if spec == nil {
		t.Fatal("expected spec, got nil")
	}
	if spec.Operations != 2 {
		t.Errorf("Operations = %d, want 2 after external-ref resolution", spec.Operations)
	}
	// Walk SpecMap to confirm no external $refs survived. Every ref
	// is either removed (inlined in place for path items) or
	// rewritten to an intra-document pointer (for schemas).
	assertNoExternalRefs(t, spec.SpecMap, "")
	// Path items are inlined in place, so /widgets now carries its
	// own get/post without a $ref wrapper.
	pathsMap, _ := spec.SpecMap["paths"].(map[string]interface{})
	widgets, _ := pathsMap["/widgets"].(map[string]interface{})
	if _, ok := widgets["get"]; !ok {
		t.Errorf("expected /widgets.get to be inlined: %#v", widgets)
	}
	if _, ok := widgets["post"]; !ok {
		t.Errorf("expected /widgets.post to be inlined: %#v", widgets)
	}
	// The schema file was lifted into components.schemas under a
	// derived name. Walk both the original Widget slot and the lifted
	// entry; we should find one that holds the actual schema body.
	components, _ := spec.SpecMap["components"].(map[string]interface{})
	schemasMap, _ := components["schemas"].(map[string]interface{})
	if spec.Types < 1 {
		t.Errorf("Types = %d, want >= 1", spec.Types)
	}
	if !findResolvedSchema(schemasMap, "id") {
		t.Errorf("expected at least one schema with an id property in components.schemas: %#v", schemasMap)
	}
}

// findResolvedSchema reports whether any entry under components.schemas
// is an object with a properties map containing the given key. This
// avoids coupling the test to the exact hoisted-component name.
func findResolvedSchema(schemas map[string]interface{}, propertyKey string) bool {
	for _, v := range schemas {
		m, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] != "object" {
			continue
		}
		props, ok := m["properties"].(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := props[propertyKey]; ok {
			return true
		}
	}
	return false
}

// TestDiscoverCanonicalSpec_CyclicSchemaDoesNotRecurseInfinitely
// guards against a crash observed on a production spec where a schema
// had a self-reference (e.g. a tree node that contains children of the
// same type). The external-ref clearing pass must terminate on cycles
// rather than blow the Go stack.
func TestDiscoverCanonicalSpec_CyclicSchemaDoesNotRecurseInfinitely(t *testing.T) {
	root := t.TempDir()
	spec := `openapi: 3.0.3
info:
  title: Cyclic
  version: "1.0"
paths:
  /nodes:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Node"
components:
  schemas:
    Node:
      type: object
      properties:
        id:
          type: string
        children:
          type: array
          items:
            $ref: "#/components/schemas/Node"
        parent:
          $ref: "#/components/schemas/Node"
`
	if err := os.WriteFile(filepath.Join(root, "openapi.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result, err := DiscoverCanonicalSpec(root)
	if err != nil {
		t.Fatalf("DiscoverCanonicalSpec returned error (likely stack overflow): %v", err)
	}
	if result == nil {
		t.Fatal("expected spec, got nil")
	}
	if result.Operations != 1 {
		t.Errorf("Operations = %d, want 1", result.Operations)
	}
	if result.Types != 1 {
		t.Errorf("Types = %d, want 1", result.Types)
	}
}

// assertNoExternalRefs walks node and fails the test if it finds any
// $ref string that is not an in-document pointer (starts with "#").
func assertNoExternalRefs(t *testing.T, node interface{}, path string) {
	t.Helper()
	switch n := node.(type) {
	case map[string]interface{}:
		if ref, ok := n["$ref"].(string); ok {
			if ref != "" && !strings.HasPrefix(ref, "#") {
				t.Errorf("external $ref survived at %s: %q", path, ref)
			}
		}
		for k, v := range n {
			assertNoExternalRefs(t, v, path+"/"+k)
		}
	case []interface{}:
		for i, v := range n {
			assertNoExternalRefs(t, v, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

func TestExtract_CSharpMinimalAPISucceeds(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "ItemsEndpoint.cs"), []byte(`
public static class ItemsEndpoint {
  public static void Map(WebApplication app) {
    var items = app.MapGroup("items");
    items.MapGet("", (int limit) => TypedResults.Ok(new ItemInfo()));
  }
}
public class ItemInfo { public string Id { get; set; } }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Extract(Options{
		Lang:    "csharp",
		RootDir: root,
		Title:   "Example API",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Operations != 1 {
		t.Fatalf("Operations = %d, want 1", result.Operations)
	}
}

func isUnsupportedLanguageError(err error) bool {
	for err != nil {
		if err == ErrLanguageUnsupported {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
