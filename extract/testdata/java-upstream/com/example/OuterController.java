package com.example;

import org.springframework.web.bind.annotation.*;
import java.util.List;

@RestController
@RequestMapping("/api/v1/outer")
public class OuterController {

    @RestController
    @RequestMapping("/inner")
    public static class InnerCatalogController {

        @GetMapping("/items")
        public List<String> listItems() {
            return null;
        }
    }
}
