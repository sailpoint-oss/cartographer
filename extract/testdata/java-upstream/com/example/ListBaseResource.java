package com.example;

import javax.ws.rs.core.Response;
import java.util.List;

public class ListBaseResource {
    protected static final String TOTAL_COUNT_HEADER = "X-Total-Count";

    protected Response okWithCount(List<?> items) {
        return Response.ok(items)
            .header(TOTAL_COUNT_HEADER, String.valueOf(items.size()))
            .build();
    }
}
