package com.example;

import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.util.List;
import java.util.Map;

@RestController
@RequestMapping("/api/v1/chronicle")
public class ChronicleController {

    @GetMapping("/services")
    public ResponseEntity<List<ServiceAPI>> listServices() {
        return null;
    }

    @GetMapping("/services/{id}")
    public ResponseEntity<ServiceAPI> getService(@PathVariable("id") String id) {
        return null;
    }

    @GetMapping("/configs")
    public Map<String, List<ConfigItem>> getConfigs() {
        return null;
    }

    @PostMapping("/services")
    public ResponseEntity<ServiceAPI> createService(@RequestBody ServiceAPI service) {
        return null;
    }
}
