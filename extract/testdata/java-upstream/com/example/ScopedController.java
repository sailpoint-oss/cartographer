package com.example;

import org.springframework.security.access.prepost.PreAuthorize;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.bind.annotation.RequestMapping;
import java.util.List;

@RestController
@RequestMapping("/api/v1/scoped")
public class ScopedController {

    @org.springframework.web.bind.annotation.GetMapping("/entries")
    @PreAuthorize("hasAuthority('example:resource:read')")
    public List<String> listEntries() {
        return null;
    }
}
