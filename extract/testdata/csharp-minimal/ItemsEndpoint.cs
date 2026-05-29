namespace Demo;

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
