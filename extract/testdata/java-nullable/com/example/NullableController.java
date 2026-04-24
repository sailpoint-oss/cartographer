package com.example;

import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;

@RestController
@RequestMapping("/api/v1/nullable")
public class NullableController {

    @GetMapping("/{id}")
    public ResponseEntity<NullableDTO> get(@PathVariable("id") String id) {
        return null;
    }
}
