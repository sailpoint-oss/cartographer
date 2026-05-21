package com.example;

import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.bind.annotation.RequestMapping;
import java.util.List;

@RestController
@RequestMapping("/api/v1/scoped")
public class ScopedController {

    @org.springframework.web.bind.annotation.GetMapping("/entries")
    public List<String> listEntries() {
        return null;
    }
}
