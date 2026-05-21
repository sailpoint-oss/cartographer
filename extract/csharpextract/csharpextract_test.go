package csharpextract

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCS(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractMinimalAPIWithRouteGroupsAndSchemas(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "ItemsEndpoint.cs", `namespace Demo;

public static class ItemsEndpoint {
  public static void Map(WebApplication app) {
    var itemsGroup = app.MapGroup("items");
    itemsGroup.MapGet("", async (int limit, IItemsHandler handler, CancellationToken ct) => TypedResults.Ok(new SearchResponse<ItemInfo>()))
      .WithName("ListItems")
      .WithTags("Items")
      .RequireAuthorization("example:item:read");
    itemsGroup.MapPost("retry/{id:long}", async (long id, IItemsHandler handler, CancellationToken ct) => TypedResults.Ok(new ItemInfo()));
    itemsGroup.MapDelete("{id:long}", async (long id, IItemsHandler handler, CancellationToken ct) => TypedResults.NoContent());
  }
}

public class ItemInfo {
  public long Id { get; set; }
  public string Name { get; set; }
}

public class SearchResponse<T> {
  public T[] Items { get; set; }
}
`)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]*Operation{}
	for _, op := range result.Operations {
		got[op.Method+" "+op.Path] = op
	}
	for _, key := range []string{"GET /items", "POST /items/retry/{id}", "DELETE /items/{id}"} {
		if got[key] == nil {
			t.Fatalf("missing %s; got %#v", key, got)
		}
	}
	if got["GET /items"].Parameters[0].Name != "limit" {
		t.Fatalf("expected query parameter limit, got %#v", got["GET /items"].Parameters)
	}
	if got["POST /items/retry/{id}"].Parameters[0].In != "path" {
		t.Fatalf("expected path parameter for id")
	}
	if _, ok := result.Schemas["ItemInfo"]; !ok {
		t.Fatalf("expected ItemInfo schema")
	}
	if got["GET /items"].File == "" || got["GET /items"].Line == 0 {
		t.Fatalf("expected source location on operation")
	}
}

func TestExtractMVCControllerWithAttributes(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "WidgetsController.cs", `namespace Demo;

[ApiController]
[Route("widgets")]
[Authorize(Policy = "example:widget:read")]
public class WidgetsController : ControllerBase {
  /// <summary>Get widget by ID.</summary>
  [HttpGet("{id:long}")]
  [ProducesResponseType(typeof(WidgetDto), StatusCodes.Status200OK)]
  public ActionResult<WidgetDto> Get([FromRoute] long id) { return null; }

  [HttpPost("")]
  [ProducesResponseType(typeof(WidgetDto), StatusCodes.Status201Created)]
  public ActionResult<WidgetDto> Create([FromBody] CreateWidgetRequest request) { return null; }
}

public class WidgetDto {
  public long Id { get; set; }
  public string Name { get; set; }
}

public class CreateWidgetRequest {
  public string Name { get; set; }
}
`)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]*Operation{}
	for _, op := range result.Operations {
		got[op.Method+" "+op.Path] = op
	}
	if got["GET /widgets/{id}"] == nil {
		t.Fatalf("missing MVC GET operation; got %#v", got)
	}
	if got["POST /widgets"] == nil {
		t.Fatalf("missing MVC POST operation; got %#v", got)
	}
	if got["POST /widgets"].RequestBodyType != "CreateWidgetRequest" {
		t.Fatalf("request body = %q", got["POST /widgets"].RequestBodyType)
	}
	if got["GET /widgets/{id}"].Summary != "Get widget by ID." {
		t.Fatalf("summary = %q", got["GET /widgets/{id}"].Summary)
	}
	if _, ok := result.Schemas["WidgetDto"]; !ok {
		t.Fatalf("expected WidgetDto schema")
	}
}

func TestGenerateSpecIncludesCSharpSourceAndSchemas(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "WidgetsController.cs", `namespace Demo;
[ApiController]
[Route("widgets")]
public class WidgetsController {
  [HttpGet("{id}")]
  public WidgetDto Get(string id) { return null; }
}
public class WidgetDto { public string Id { get; set; } }
`)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	spec := GenerateSpec(result, SpecConfig{Title: "Example API", Version: "1.0.0", OpenAPIVersion: "3.2", ServiceTemplate: "csharp-web"})
	paths := spec["paths"].(map[string]any)
	op := paths["/widgets/{id}"].(map[string]any)["get"].(map[string]any)
	if _, ok := op["x-source-file"]; !ok {
		t.Fatalf("expected source map extension in operation: %#v", op)
	}
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	if _, ok := schemas["WidgetDto"]; !ok {
		t.Fatalf("expected WidgetDto component schema")
	}
}

func TestCSharpDataAnnotationsOnRequestBody(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "CreateWidgetRequest.cs", `namespace Demo;

public class CreateWidgetRequest {
  [Required]
  [StringLength(128)]
  public string Name { get; set; }
}
`)
	writeCS(t, dir, "WidgetsController.cs", `namespace Demo;

[ApiController]
[Route("widgets")]
public class WidgetsController {
  [HttpPost("")]
  public WidgetDto Create([FromBody] CreateWidgetRequest request) { return null; }
}

public class WidgetDto {
  public string Id { get; set; }
}
`)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	spec := GenerateSpec(result, SpecConfig{Title: "Example API", Version: "1.0.0"})
	paths := spec["paths"].(map[string]any)
	post := paths["/widgets"].(map[string]any)["post"].(map[string]any)
	reqBody := post["requestBody"].(map[string]any)
	content := reqBody["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	schema := jsonContent["schema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	name := props["name"].(map[string]any)
	if got, ok := name["maxLength"].(float64); !ok || int(got) != 128 {
		if gotInt, ok := name["maxLength"].(int); !ok || gotInt != 128 {
			t.Errorf("maxLength = %v, want 128", name["maxLength"])
		}
	}
	if ref, ok := schema["$ref"].(string); ok && ref != "" {
		components := spec["components"].(map[string]any)
		schemas := components["schemas"].(map[string]any)
		schema = schemas["CreateWidgetRequest"].(map[string]any)
	}
	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("required = %v, want [name]", required)
	}
}
