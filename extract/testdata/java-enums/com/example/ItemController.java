package com.example;

import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;

@RestController
@RequestMapping("/api/v1/items")
public class ItemController {

    @GetMapping("/{id}")
    public ResponseEntity<ItemDTO> getItem(@PathVariable("id") String id) {
        return null;
    }
}
