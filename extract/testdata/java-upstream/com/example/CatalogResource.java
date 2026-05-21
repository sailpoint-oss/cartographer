package com.example;

import javax.ws.rs.*;
import javax.ws.rs.core.Response;
import java.util.List;

@Path("/api/v1/catalog")
public class CatalogResource extends ListBaseResource {

    @GET
    @Path("/items")
    public Response listItems() {
        return okWithCount(List.of("a", "b"));
    }
}
