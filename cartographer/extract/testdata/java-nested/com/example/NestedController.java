package com.example;

import javax.ws.rs.*;

@Path("/api/v1/nested")
public class NestedController {

    @GET
    @Path("/{id}")
    public OuterDTO get(@PathParam("id") String id) {
        return null;
    }
}
