package com.example;

import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.util.List;

@RestController
@RequestMapping("/api/v1/spring")
public class SpringController {

    @GetMapping
    public ResponseEntity<List<SpringDTO>> list(
            @RequestParam(value = "offset", required = false, defaultValue = "0") int offset,
            @RequestParam(value = "limit", required = false, defaultValue = "50") int limit,
            @RequestParam(value = "active", required = false, defaultValue = "true") boolean active,
            @RequestParam(value = "name", required = false) String name) {
        return null;
    }
}
