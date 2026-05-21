package com.example;

import org.springframework.web.bind.annotation.*;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotNull;

@RestController
@RequestMapping("/api/v1/widgets")
public class WidgetResource {

    @GetMapping("/{id}")
    public WidgetDto get(@PathVariable("id") String id) {
        throw new NotFoundException("missing");
    }

    @PostMapping
    public WidgetDto create(@Valid @RequestBody CreateWidgetRequest body) {
        return null;
    }
}

class WidgetDto {
    private String id;
}

class CreateWidgetRequest {
    @NotNull
    private String name;
}
