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
    itemsGroup.MapGet("", async (int limit, HttpContext httpContext, IItemsHandler handler, CancellationToken ct) => {
      httpContext.Response.Headers["X-Total-Count"] = "0";
      return TypedResults.Ok(new SearchResponse<ItemInfo>());
    })
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
	if _, ok := got["GET /items"].ResponseHeaders["X-Total-Count"]; !ok {
		t.Fatalf("expected X-Total-Count response header, got %#v", got["GET /items"].ResponseHeaders)
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

func TestExtractMinimalAPIMethodGroupsAndExcludedRoutes(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "ApplicationsEndpoint.cs", `namespace Demo;

public static class ApplicationsEndpoint {
  public static void Map(WebApplication app) {
    var api = app.MapGroup("api");
    var apps = api.MapGroup("applications");
    apps.MapGet("", ListApplications)
      .WithName("ListApplications")
      .Produces<SearchResponse<ApplicationInfo>>(StatusCodes.Status200OK)
      .RequireAuthorization("example:application:read");
    apps.MapPost("/", CreateApplication)
      .Produces<ApplicationInfo>(StatusCodes.Status201Created)
      .RequireAuthorization("example:application:create");
    apps.MapGet("internal/{id:long}", GetInternalApplication)
      .ExcludeFromDescription();
  }

  private static Task<SearchResponse<ApplicationInfo>> ListApplications(int limit, IApplicationHandler handler, CancellationToken ct) {
    return null;
  }

  private static Task<ApplicationInfo> CreateApplication(CreateApplicationRequest request, IApplicationHandler handler, CancellationToken ct) {
    return null;
  }

  private static Task<ApplicationInfo> GetInternalApplication(long id) {
    return null;
  }
}

public class ApplicationInfo {
  public long Id { get; set; }
}

public class SearchResponse<T> {
  public T[] Items { get; set; }
}

public class CreateApplicationRequest {
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
	list := got["GET /api/applications"]
	if list == nil {
		t.Fatalf("missing method-group GET operation; got %#v", got)
	}
	if list.OperationID != "ListApplications" {
		t.Fatalf("operation ID = %q", list.OperationID)
	}
	if list.ResponseType != "SearchResponse<ApplicationInfo>" {
		t.Fatalf("response type = %q", list.ResponseType)
	}
	if list.ResponseStatus != 200 {
		t.Fatalf("response status = %d", list.ResponseStatus)
	}
	if len(list.Security) != 1 || list.Security[0] != "example:application:read" {
		t.Fatalf("security = %#v", list.Security)
	}
	if len(list.Parameters) == 0 || list.Parameters[0].Name != "limit" || list.Parameters[0].In != "query" {
		t.Fatalf("parameters = %#v", list.Parameters)
	}
	create := got["POST /api/applications"]
	if create == nil {
		t.Fatalf("missing method-group POST operation; got %#v", got)
	}
	if create.RequestBodyType != "CreateApplicationRequest" {
		t.Fatalf("request body = %q", create.RequestBodyType)
	}
	if got["GET /api/applications/internal/{id}"] != nil {
		t.Fatalf("expected ExcludeFromDescription route to be skipped")
	}
}

func TestExtractMinimalAPIChainMetadataConstantsAndReturnTypes(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "SearchEndpoints.cs", `namespace Demo;

public static class Policies {
  public const string Read = "example:search:read";
}

public static class SearchEndpoints {
  public static void Map(WebApplication app) {
    var api = app.MapGroup("/api")
      .RequireAuthorization(Policies.Read);

    api.MapPost("/search/{id:guid}", async (
      Guid id,
      SearchRequest request,
      ISearchHandler handler,
      CancellationToken cancellationToken) =>
    {
      var response = new SearchResponse<SearchItem>();
      return TypedResults.Ok(response);
    })
    .WithName("SearchItems");
  }
}

public class SearchRequest {
  public string Query { get; set; }
}

public class SearchResponse<T> {
  public T[] Items { get; set; }
}

public class SearchItem {
  public string Id { get; set; }
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
	op := got["POST /api/search/{id}"]
	if op == nil {
		t.Fatalf("missing operation; got %#v", got)
	}
	if op.RequestBodyType != "SearchRequest" {
		t.Fatalf("request body = %q", op.RequestBodyType)
	}
	if op.ResponseType != "SearchResponse<SearchItem>" {
		t.Fatalf("response type = %q", op.ResponseType)
	}
	if op.ResponseStatus != 200 {
		t.Fatalf("response status = %d", op.ResponseStatus)
	}
	if len(op.Security) != 1 || op.Security[0] != "example:search:read" {
		t.Fatalf("security = %#v", op.Security)
	}
	for _, p := range op.Parameters {
		if p.Name == "cancellationToken" {
			t.Fatalf("framework CancellationToken leaked as API parameter: %#v", op.Parameters)
		}
	}
}

func TestExtractMinimalAPIHandlerCallReturnInference(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "Endpoints.cs", `namespace Demo;

public static class SearchEndpoints {
  public static void Map(WebApplication app) {
    app.MapPost("/search", async (SearchRequest request, ISearchHandler handler, CancellationToken ct) =>
    {
      return await handler.SearchAsync(request, ct).ConfigureAwait(false);
    });
  }
}

public class SearchRequest {
  public string Query { get; set; }
}
`)
	writeCS(t, dir, "ISearchHandler.cs", `namespace Demo;

public interface ISearchHandler {
  Task<SearchResponse<SearchItem>> SearchAsync(SearchRequest request, CancellationToken ct);
}

public class SearchResponse<T> {
  public T[] Items { get; set; }
}

public class SearchItem {
  public string Id { get; set; }
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
	op := got["POST /search"]
	if op == nil {
		t.Fatalf("missing POST /search; got %#v", got)
	}
	if op.ResponseType != "SearchResponse<SearchItem>" {
		t.Fatalf("response type = %q", op.ResponseType)
	}
	if op.RequestBodyType != "SearchRequest" {
		t.Fatalf("request body = %q", op.RequestBodyType)
	}
}

// TestExtractMinimalAPIMultilineLambdaChainCapture verifies that fluent calls
// chained AFTER a multiline inline lambda body (whose statements contain ';')
// are still captured: WithName, Produces<T>, RequireAuthorization, and
// ExcludeFromDescription. Previously the chain was truncated at the first ';'
// inside the lambda body, dropping all of these.
func TestExtractMinimalAPIMultilineLambdaChainCapture(t *testing.T) {
	dir := t.TempDir()
	writeCS(t, dir, "WidgetEndpoints.cs", `namespace Demo;

public static class WidgetEndpoints {
  public static void Map(WebApplication app) {
    app.MapGet("/widgets", async (IWidgetHandler handler, CancellationToken ct) =>
    {
      var widgets = await handler.ListAsync(ct);
      return TypedResults.Ok(widgets);
    })
    .Produces<WidgetList>(StatusCodes.Status200OK)
    .WithName("ListWidgets")
    .RequireAuthorization("example:widget:read");

    app.MapGet("/internal/widgets", async (IWidgetHandler handler) =>
    {
      var w = await handler.ListAsync(default);
      return TypedResults.Ok(w);
    })
    .ExcludeFromDescription();
  }
}

public class WidgetList {
  public string[] Ids { get; set; }
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
	list := got["GET /widgets"]
	if list == nil {
		t.Fatalf("missing GET /widgets; got %#v", got)
	}
	if list.OperationID != "ListWidgets" {
		t.Errorf("operation ID = %q, want ListWidgets (from chained .WithName)", list.OperationID)
	}
	if list.ResponseType != "WidgetList" {
		t.Errorf("response type = %q, want WidgetList (from chained .Produces<T>)", list.ResponseType)
	}
	if len(list.Security) != 1 || list.Security[0] != "example:widget:read" {
		t.Errorf("security = %#v, want [example:widget:read] (from chained .RequireAuthorization)", list.Security)
	}
	if _, ok := got["GET /internal/widgets"]; ok {
		t.Errorf("expected ExcludeFromDescription route (chained after lambda) to be skipped")
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
	if _, ok := op["x-source"]; !ok {
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

func TestExtractCSharpJsonWireName(t *testing.T) {
	attrs := `[JsonPropertyName("user_id")] `
	if got := extractCSharpJsonWireName(attrs); got != "user_id" {
		t.Fatalf("got %q", got)
	}
	text := `[JsonPropertyName("user_id")] public string UserId { get; set; }
public string DisplayName { get; set; }`
	props := parseProperties(text)
	if len(props) != 2 {
		t.Fatalf("expected 2 properties, got %d: %#v", len(props), props)
	}
	if props[0].Name != "user_id" {
		t.Fatalf("first wire name = %q", props[0].Name)
	}
	if props[1].Name != "displayName" {
		t.Fatalf("second wire name = %q", props[1].Name)
	}
}
